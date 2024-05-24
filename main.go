/*

Copyright 2020 SAP SE

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.uber.org/zap/zapcore"
	v1 "k8s.io/api/core/v1"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/go-logr/logr"
	"github.com/sapcc/maintenance-controller/api"
	"github.com/sapcc/maintenance-controller/cache"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/controllers"
	"github.com/sapcc/maintenance-controller/esx"
	"github.com/sapcc/maintenance-controller/event"
	"github.com/sapcc/maintenance-controller/kubernikus"
	"github.com/sapcc/maintenance-controller/metrics"
	"github.com/sapcc/maintenance-controller/otelr"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

type reconcilerConfig struct {
	metricsAddr                 string
	metricsTimeout              time.Duration
	enableESXMaintenance        bool
	enableKubernikusMaintenance bool
}

func main() {
	var reconcilerCfg reconcilerConfig
	var kubecontext, probeAddr string
	var enableLeaderElection bool
	flag.StringVar(&reconcilerCfg.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&kubecontext, "kubecontext", "", "The context to use from the kubeconfig (defaults to current-context)")
	flag.StringVar(&probeAddr, "health-addr", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&reconcilerCfg.enableESXMaintenance, "enable-esx-maintenance", false,
		"Enables an additional controller, which will indicate ESX host maintenance using labels.")
	flag.BoolVar(&reconcilerCfg.enableKubernikusMaintenance, "enable-kubernikus-maintenance", false,
		"Enables an additional controller, which will indicate outdated kubelets and enable VM deletions.")
	flag.DurationVar(&reconcilerCfg.metricsTimeout, "metrics-timeout", 65*time.Second, //nolint:gomnd
		"Maximum delay between SIGTERM and actual shutdown to scrape metrics one last time.")
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctx := ctrl.SetupSignalHandler()
	zaplog := zap.New(zap.UseFlagOptions(&opts))
	otelShutdown, err := setupOtel(ctx, zaplog.GetSink(), zaplog.WithName("otel"))
	if err != nil {
		setupLog.Error(err, "unable to setup open telemetry metric export")
		os.Exit(1)
	}

	restConfig := getKubeconfigOrDie(kubecontext)
	setupLog.Info("Loaded kubeconfig", "context", kubecontext, "host", restConfig.Host)

	leaderElectionRetry := 5 * time.Second //nolint:gomnd
	shutdownTimeout := 70 * time.Second    //nolint:gomnd
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                     scheme,
		Metrics:                    server.Options{BindAddress: "0"},               // disable inbuilt metrics server
		WebhookServer:              webhook.NewServer(webhook.Options{Port: 9443}), //nolint:gomnd
		HealthProbeBindAddress:     probeAddr,
		EventBroadcaster:           event.NewNodeBroadcaster(),
		LeaderElectionResourceLock: "leases",
		LeaderElection:             enableLeaderElection,
		LeaderElectionID:           constants.LeaderElectionID,
		RetryPeriod:                &leaderElectionRetry,
		GracefulShutdownTimeout:    &shutdownTimeout,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = metrics.RegisterMaintenanceMetrics(); err != nil {
		setupLog.Error(err, "unable to setup metrics")
		os.Exit(1)
	}
	setupChecks(mgr)
	err = setupReconcilers(mgr, &reconcilerCfg)
	if err != nil {
		setupLog.Error(err, "problem setting up reconcilers")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	otelShutdown()
	setupLog.Info("Received SIGTERM or SIGINT. See you later.")
}

func getKubeconfigOrDie(kubecontext string) *rest.Config {
	if kubecontext == "" {
		kubecontext = os.Getenv("KUBECONTEXT")
	}
	restConfig, err := config.GetConfigWithContext(kubecontext)
	if err != nil {
		setupLog.Error(err, "Failed to load kubeconfig")
		os.Exit(1)
	}
	return restConfig
}

func setupChecks(mgr manager.Manager) {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}
}

func setupReconcilers(mgr manager.Manager, cfg *reconcilerConfig) error {
	nodeInfoCache := cache.NewNodeInfoCache()
	if err := (&controllers.NodeReconciler{
		Client:        mgr.GetClient(),
		Log:           ctrl.Log.WithName("controllers").WithName("maintenance"),
		Scheme:        mgr.GetScheme(),
		Recorder:      mgr.GetEventRecorderFor("maintenance"),
		NodeInfoCache: nodeInfoCache,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup maintenance controller node reconciler: %w", err)
	}

	// Required for affinity check plugin as well as kubernikus and ESX integration
	err := mgr.GetFieldIndexer().IndexField(context.Background(),
		&v1.Pod{},
		"spec.nodeName",
		func(o client.Object) []string {
			pod, ok := o.(*v1.Pod) //nolint:forcetypeassert
			if !ok {
				return []string{}
			}
			return []string{pod.Spec.NodeName}
		})
	if err != nil {
		return fmt.Errorf("unable to create index spec.nodeName on pod resource: %w", err)
	}

	apiServer := api.Server{
		Address:       cfg.metricsAddr,
		Log:           ctrl.Log.WithName("metrics"),
		WaitTimeout:   cfg.metricsTimeout,
		NodeInfoCache: nodeInfoCache,
		Elected:       mgr.Elected(),
		Client:        mgr.GetClient(),
	}
	if err := mgr.Add(&apiServer); err != nil {
		return fmt.Errorf("failed to attach prometheus metrics server: %w", err)
	}

	if cfg.enableKubernikusMaintenance {
		setupLog.Info("Kubernikus integration is enabled")
		if err := (&kubernikus.NodeReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("kubernikus"),
			Scheme: mgr.GetScheme(),
			Conf:   mgr.GetConfig(),
		}).SetupWithManager(mgr); err != nil {
			return fmt.Errorf("failed to setup kubernikus node reconciler: %w", err)
		}
	}

	if cfg.enableESXMaintenance {
		setupLog.Info("ESX integration is enabled")
		controller := esx.Runnable{
			Client: mgr.GetClient(),
			Conf:   mgr.GetConfig(),
			Log:    ctrl.Log.WithName("controllers").WithName("esx"),
		}
		if err := mgr.Add(&controller); err != nil {
			return fmt.Errorf("failed to create ESX reconciler: %w", err)
		}
	}
	return nil
}

func setupOtel(ctx context.Context, baseSink logr.LogSink, logger logr.Logger) (func(), error) {
	resource, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName("maintenance-controller"),
			semconv.ServiceVersion("0.1.0"),
		))
	if err != nil {
		return nil, err
	}

	traceExporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}

	tracerProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(resource),
	)

	otel.SetTextMapPropagator(propagation.TraceContext{})

	metricExporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithResource(resource),
		metric.WithReader(metric.NewPeriodicReader(
			metricExporter,
			metric.WithInterval(time.Minute),
		)),
	)
	otel.SetMeterProvider(meterProvider)

	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, err
	}
	logProvider := log.NewLoggerProvider(
		log.WithProcessor(log.NewSimpleProcessor(logExporter)),
		log.WithResource(resource),
	)
	global.SetLoggerProvider(logProvider)

	otelSink := otelr.NewOtelSink(global.Logger("maintenance-controller"))
	ctrl.SetLogger(logr.New(otelr.NewMultiSink([]logr.LogSink{baseSink, otelSink})))

	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(cause error) {
		logger.Error(cause, "encountered otel error")
	}))
	shutdown := func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			logger.Error(err, "failed to shutdown open telemetry trace exporter")
		}
		if err := meterProvider.Shutdown(context.Background()); err != nil {
			logger.Error(err, "failed to shutdown open telemetry metrics exporter")
		}
		if err := logProvider.Shutdown(context.Background()); err != nil {
			logger.Error(err, "failed to shutdown open telemetry log exporter")
		}
	}
	return shutdown, nil
}

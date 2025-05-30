// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/zap/zapcore"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
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

	"github.com/sapcc/maintenance-controller/api"
	"github.com/sapcc/maintenance-controller/cache"
	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/controllers"
	"github.com/sapcc/maintenance-controller/esx"
	"github.com/sapcc/maintenance-controller/event"
	"github.com/sapcc/maintenance-controller/kubernikus"
	"github.com/sapcc/maintenance-controller/metrics"
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
	var kubecontext, probeAddr, pprofAddr string
	var enableLeaderElection bool
	flag.StringVar(&reconcilerCfg.metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&kubecontext, "kubecontext", "", "The context to use from the kubeconfig (defaults to current-context)")
	flag.StringVar(&probeAddr, "health-addr", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&pprofAddr, "pprof-addr", "", "The address the pprof endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&reconcilerCfg.enableESXMaintenance, "enable-esx-maintenance", false,
		"Enables an additional controller, which will indicate ESX host maintenance using labels.")
	flag.BoolVar(&reconcilerCfg.enableKubernikusMaintenance, "enable-kubernikus-maintenance", false,
		"Enables an additional controller, which will indicate outdated kubelets and enable VM deletions.")
	flag.DurationVar(&reconcilerCfg.metricsTimeout, "metrics-timeout", 65*time.Second,
		"Maximum delay between SIGTERM and actual shutdown to scrape metrics one last time.")
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	restConfig := getKubeconfigOrDie(kubecontext)
	setupLog.Info("Loaded kubeconfig", "context", kubecontext, "host", restConfig.Host)

	leaderElectionRetry := 5 * time.Second
	shutdownTimeout := 70 * time.Second
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                     scheme,
		Metrics:                    server.Options{BindAddress: "0"}, // disable inbuilt metrics server
		WebhookServer:              webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress:     probeAddr,
		EventBroadcaster:           event.NewNodeBroadcaster(),
		LeaderElectionResourceLock: "leases",
		LeaderElection:             enableLeaderElection,
		LeaderElectionID:           constants.LeaderElectionID,
		RetryPeriod:                &leaderElectionRetry,
		GracefulShutdownTimeout:    &shutdownTimeout,
		PprofBindAddress:           pprofAddr,
		Cache:                      common.DefaultKubernetesCacheOpts(),
		Client: client.Options{
			Cache: &client.CacheOptions{
				// The only secret lookup is the optional lookup in the
				// Kubernikus controller. To allow scoping RBAC to secrets
				// with a resourceName, the cache needs to be disabled.
				DisableFor: []client.Object{&v1.Secret{}},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	metrics.RegisterMaintenanceMetrics()
	setupChecks(mgr)
	err = setupReconcilers(mgr, &reconcilerCfg)
	if err != nil {
		setupLog.Error(err, "problem setting up reconcilers")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
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
		Clientset:     kubernetes.NewForConfigOrDie(mgr.GetConfig()),
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

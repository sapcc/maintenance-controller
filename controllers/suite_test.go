// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/sapcc/maintenance-controller/cache"
	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/event"
	"github.com/sapcc/maintenance-controller/metrics"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const config = `
intervals:
  requeue: 200ms
  notify: 500ms
instances:
  notify: null
  check:
  - type: hasLabel
    name: transition
    config:
      key: transition
      value: "true"
  - type: prometheusInstant
    name: fail
    config:
      url: bananabread
  trigger:
  - type: alterLabel
    name: alter
    config:
      key: alter
      value: "true"
      remove: false
  - type: alterLabel
    name: entered
    config:
      key: entered
      value: "true"
      remove: false
profiles:
- name: count
  operational:
    transitions:
    - check: transition
      trigger: alter
      next: maintenance-required
  maintenance-required:
    transitions:
    - check: transition && transition
      next: in-maintenance
  in-maintenance:
    enter: entered
    transitions:
    - check: transition && transition && transition
      next: operational
- name: test
  operational:
    transitions:
    - check: transition
      trigger: alter
      next: maintenance-required
- name: multi
  operational:
    transitions:
    - check: transition
      trigger: alter
      next: maintenance-required
  maintenance-required:
    transitions:
    - check: transition
      next: in-maintenance
  in-maintenance:
    transitions:
    - check: "!transition"
      next: operational
- name: block
  operational:
    transitions:
    - check: transition
      next: maintenance-required
  maintenance-required:
    transitions:
    - check: "!transition"
      next: in-maintenance
- name: to-maintenance
  operational:
    transitions:
    - check: transition
      next: in-maintenance
- name: broken
  operational:
    transitions:
    - check: fail
      next: maintenance-required
`

var (
	cfg            *rest.Config
	k8sClient      client.Client
	k8sClientset   kubernetes.Interface
	k8sManager     ctrl.Manager
	testEnv        *envtest.Environment
	stopController context.CancelFunc
	nodeInfoCache  cache.NodeInfoCache
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(3 * time.Second)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sManager, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
		EventBroadcaster: event.NewNodeBroadcaster(),
		Cache:            common.DefaultKubernetesCacheOpts(),
	})
	Expect(err).ToNot(HaveOccurred())

	err = k8sManager.GetFieldIndexer().IndexField(context.Background(),
		&corev1.Pod{},
		"spec.nodeName",
		func(o client.Object) []string {
			pod, ok := o.(*corev1.Pod)
			if !ok {
				return []string{}
			}
			return []string{pod.Spec.NodeName}
		})
	Expect(err).To(Succeed())

	metrics.RegisterMaintenanceMetrics()

	nodeInfoCache = cache.NewNodeInfoCache()
	err = (&NodeReconciler{
		Client:        k8sManager.GetClient(),
		Log:           ctrl.Log.WithName("controllers").WithName("maintenance"),
		Scheme:        k8sManager.GetScheme(),
		Recorder:      k8sManager.GetEventRecorder("controller"),
		NodeInfoCache: nodeInfoCache,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		stopCtx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
		stopController = cancel
		err = k8sManager.Start(stopCtx)
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClientset, err = kubernetes.NewForConfig(k8sManager.GetConfig())
	Expect(err).To(Succeed())
	Expect(k8sClientset).ToNot(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).To(Succeed())
	Expect(k8sClient).ToNot(BeNil())

	err = os.MkdirAll("./config", 0700)
	Expect(err).To(Succeed())
	err = os.WriteFile(constants.MaintenanceConfigFilePath, []byte(config), 0600)
	Expect(err).To(Succeed())
})

var _ = AfterSuite(func() {
	stopController()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())

	err = os.Remove(constants.MaintenanceConfigFilePath)
	Expect(err).To(Succeed())
	err = os.Remove("./config")
	Expect(err).To(Succeed())
})

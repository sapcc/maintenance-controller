/*******************************************************************************
*
* Copyright 2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package controllers

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/cache"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/event"
	"github.com/sapcc/maintenance-controller/metrics"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
  trigger:
  - type: alterLabel
    name: alter
    config:
      key: alter
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
`

var cfg *rest.Config
var k8sClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var stopController context.CancelFunc
var nodeInfoCache cache.NodeInfoCache

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
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
		EventBroadcaster:   event.NewNodeBroadcaster(),
	})
	Expect(err).ToNot(HaveOccurred())

	err = k8sManager.GetFieldIndexer().IndexField(context.Background(),
		&corev1.Pod{},
		"spec.nodeName",
		func(o client.Object) []string {
			pod := o.(*corev1.Pod)
			return []string{pod.Spec.NodeName}
		})
	Expect(err).To(Succeed())

	metrics.RegisterMaintenanceMetrics()

	nodeInfoCache = cache.NewNodeInfoCache()
	err = (&NodeReconciler{
		Client:        k8sManager.GetClient(),
		Log:           ctrl.Log.WithName("controllers").WithName("maintenance"),
		Scheme:        k8sManager.GetScheme(),
		Recorder:      k8sManager.GetEventRecorderFor("controller"),
		NodeInfoCache: nodeInfoCache,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		stopCtx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
		stopController = cancel
		err = k8sManager.Start(stopCtx)
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
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

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

package kubernikus

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/event"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const cloudprovider string = `
[Global]
auth-url="https://identity-3.qa-de-1.cloud.sap/v3/"
domain-name="kubernikus"
tenant-id="id"
username="user"
password="pw"
region="qa-de-1"
`

const config string = `
intervals:
  requeue: 250ms
  postDrain: 0s
  podDeletion:
    period: 2s
    timeout: 30s
`

var cfg *rest.Config
var k8sClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment

func TestKubernikus(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
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

	err = (&NodeReconciler{
		Client: k8sManager.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("maintenance"),
		Scheme: k8sManager.GetScheme(),
		Conf:   cfg,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		Expect(err).ToNot(HaveOccurred())
	}()

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	err = os.MkdirAll("./config", 0700)
	Expect(err).To(Succeed())
	err = os.MkdirAll("./provider", 0700)
	Expect(err).To(Succeed())
	err = ioutil.WriteFile(constants.CloudProviderConfigFilePath, []byte(cloudprovider), 0600)
	Expect(err).To(Succeed())
	err = ioutil.WriteFile(constants.KubernikusConfigFilePath, []byte(config), 0600)
	Expect(err).To(Succeed())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())

	err = os.Remove(constants.KubernikusConfigFilePath)
	Expect(err).To(Succeed())
	err = os.Remove(constants.CloudProviderConfigFilePath)
	Expect(err).To(Succeed())
	err = os.Remove("./config")
	Expect(err).To(Succeed())
	err = os.Remove("./provider")
	Expect(err).To(Succeed())
})

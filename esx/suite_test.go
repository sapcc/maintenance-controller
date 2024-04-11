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

package esx

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"text/template"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/sapcc/maintenance-controller/constants"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

const ESXName string = "DC0_H0"
const TemplateURL = "http://" + AvailabilityZoneReplacer

// The availability "parser" only considers the last character of the region string.
// To get the required credentials the one char AZ needs to be part of the hostname.
// Additionally the simulated vCenter binds to a random port.
const configTemplate = `
intervals:
  check:
    jitter: 0.1
    period: 400ms
  podDeletion:
    period: 1s
    timeout: 4s
  podEviction:
    period: 1s
    timeout: 4s
  vmShutdown:
    period: 1s
    timeout: 4s
vCenters:
  templateUrl: http://loc$AZlhost:{{ .Port }}
  credentials:
    a:
      username: user
      password: pass
`

var cfg *rest.Config
var k8sClient client.Client
var k8sManager ctrl.Manager
var testEnv *envtest.Environment
var vCenter *simulator.Model
var vcServer *simulator.Server
var stopController context.CancelFunc

func TestESX(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "ESX Suite")
}

// This is like sigs.k8s.io/controller-runtime SetupSignalHandler()
// but without the no double signal handler check.
func SetupSignalHandler() context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		cancel()
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	return ctx
}

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(3 * time.Second)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	var err error

	By("setup simulated vCenter")
	vCenter = simulator.VPX()
	vCenter.Host = 2
	err = vCenter.Create()
	Expect(err).To(Succeed())
	vcServer = vCenter.Service.NewServer()

	vcClient, err := govmomi.NewClient(context.Background(), vcServer.URL, true)
	Expect(err).To(Succeed())

	renameVM := func(view *view.ContainerView, oldName, newName string) {
		var vms []mo.VirtualMachine
		err := view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"}, []string{"summary.runtime"},
			&vms, property.Match{"name": oldName})
		Expect(err).To(Succeed())
		vm := object.NewVirtualMachine(vcClient.Client, vms[0].Self)
		task, err := vm.Rename(context.Background(), newName)
		Expect(err).To(Succeed())
		Expect(task.WaitEx(context.Background())).To(Succeed())
	}
	mgr := view.NewManager(vcClient.Client)
	view, err := mgr.CreateContainerView(context.Background(),
		vcClient.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	Expect(err).To(Succeed())
	renameVM(view, "DC0_H0_VM0", "firstvm")
	renameVM(view, "DC0_H0_VM1", "secondvm")
	renameVM(view, "DC0_H1_VM0", "thirdvm")
	renameVM(view, "DC0_H1_VM1", "fourthvm")

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
	}

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

	controller := Runnable{
		Client: k8sManager.GetClient(),
		Conf:   k8sManager.GetConfig(),
		Log:    ctrl.Log.WithName("controllers").WithName("esx"),
	}
	err = k8sManager.Add(&controller)
	Expect(err).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	err = os.MkdirAll("config", 0700)
	Expect(err).To(Succeed())
	file, err := os.Create(constants.EsxConfigFilePath)
	Expect(err).To(Succeed())
	defer file.Close()

	tpl := template.New("config")
	_, err = tpl.Parse(configTemplate)
	Expect(err).To(Succeed())
	data := struct {
		Port string
	}{Port: vcServer.URL.Port()}
	err = tpl.Execute(file, data)
	Expect(err).To(Succeed())
	err = file.Sync()
	Expect(err).To(Succeed())

	go func() {
		defer GinkgoRecover()
		stopCtx, cancel := context.WithCancel(SetupSignalHandler())
		stopController = cancel
		err = k8sManager.Start(stopCtx)
		Expect(err).ToNot(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	stopController()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())

	err = os.Remove(constants.EsxConfigFilePath)
	Expect(err).To(Succeed())
	err = os.Remove("config")
	Expect(err).To(Succeed())

	By("tearing down simulated vCenter")
	vcServer.Close()
	vCenter.Remove()
})

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
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	vctypes "github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ShouldReboot", func() {
	It("should pass if all nodes require ESX maintenance and are allowed to reboot", func() {
		nodes := make([]corev1.Node, 2)
		nodes[0].Labels = make(map[string]string)
		nodes[0].Labels[MaintenanceLabelKey] = string(InMaintenance)
		nodes[0].Labels[RebootOkLabelKey] = TrueString
		nodes[1].Labels = make(map[string]string)
		nodes[1].Labels[MaintenanceLabelKey] = string(InMaintenance)
		nodes[1].Labels[RebootOkLabelKey] = TrueString
		esx := Host{
			Nodes: nodes,
		}
		result := ShouldReboot(&esx)
		Expect(result).To(BeTrue())
	})

	It("should not pass if at least one node does not require maintenance", func() {
		nodes := make([]corev1.Node, 2)
		nodes[0].Labels = make(map[string]string)
		nodes[0].Labels[MaintenanceLabelKey] = string(InMaintenance)
		nodes[0].Labels[RebootOkLabelKey] = TrueString
		nodes[1].Labels = make(map[string]string)
		nodes[1].Labels[MaintenanceLabelKey] = string(NoMaintenance)
		nodes[1].Labels[RebootOkLabelKey] = TrueString
		esx := Host{
			Nodes: nodes,
		}
		result := ShouldReboot(&esx)
		Expect(result).To(BeFalse())
	})

	It("should not pass if at least one node does not have approval", func() {
		nodes := make([]corev1.Node, 2)
		nodes[0].Labels = make(map[string]string)
		nodes[0].Labels[MaintenanceLabelKey] = string(InMaintenance)
		nodes[0].Labels[RebootOkLabelKey] = TrueString
		nodes[1].Labels = make(map[string]string)
		nodes[1].Labels[MaintenanceLabelKey] = string(InMaintenance)
		nodes[1].Labels[RebootOkLabelKey] = "thisisfine"
		esx := Host{
			Nodes: nodes,
		}
		result := ShouldReboot(&esx)
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("ShouldCordon", func() {
	It("should pass if the controller initiated a reboot and node is schedulable", func() {
		var node corev1.Node
		node.Annotations = map[string]string{RebootInitiatedAnnotationKey: TrueString}
		node.Spec.Unschedulable = false
		result := ShouldCordon(&node)
		Expect(result).To(BeTrue())
	})

	It("should not pass if the controller did not initiate a reboot and node is schedulable", func() {
		var node corev1.Node
		node.Annotations = map[string]string{RebootInitiatedAnnotationKey: "garbage"}
		node.Spec.Unschedulable = false
		result := ShouldCordon(&node)
		Expect(result).To(BeFalse())
	})

	It("should not pass if the controller initiated a reboot and node is unschedulable", func() {
		var node corev1.Node
		node.Annotations = map[string]string{RebootInitiatedAnnotationKey: TrueString}
		node.Spec.Unschedulable = true
		result := ShouldCordon(&node)
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("ShouldDrain", func() {
	It("should pass if the controller initiated a reboot and node is unschedulable", func() {
		var node corev1.Node
		node.Annotations = map[string]string{RebootInitiatedAnnotationKey: TrueString}
		node.Spec.Unschedulable = true
		result := ShouldDrain(&node)
		Expect(result).To(BeTrue())
	})

	It("should not pass if the controller initiated a reboot and node is schedulable", func() {
		var node corev1.Node
		node.Annotations = map[string]string{RebootInitiatedAnnotationKey: TrueString}
		node.Spec.Unschedulable = false
		result := ShouldDrain(&node)
		Expect(result).To(BeFalse())
	})

	It("should not pass if the controller did not initiate a reboot and node is unschedulable", func() {
		var node corev1.Node
		node.Annotations = map[string]string{RebootInitiatedAnnotationKey: "garbage"}
		node.Spec.Unschedulable = true
		result := ShouldDrain(&node)
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("GetPodsForDeletion", func() {

	makePod := func(podName, nodeName string, custom ...func(*corev1.Pod)) error {
		var pod corev1.Pod
		pod.Namespace = "default"
		pod.Name = podName
		pod.Spec.NodeName = nodeName
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		for _, cust := range custom {
			cust(&pod)
		}
		return k8sClient.Create(context.Background(), &pod)
	}

	AfterEach(func() {
		var podList corev1.PodList
		Expect(k8sClient.List(context.Background(), &podList)).To(Succeed())
		var gracePeriod int64
		for i := range podList.Items {
			Expect(k8sClient.Delete(context.Background(), &podList.Items[i],
				&client.DeleteOptions{GracePeriodSeconds: &gracePeriod})).To(Succeed())
		}
		err := WaitForPodDeletions(context.Background(), podList.Items, WaitParameters{
			Client:  k8sClient,
			Period:  1 * time.Second,
			Timeout: 4 * time.Second,
		})
		Expect(err).To(Succeed())
	})

	It("filters for the correct node", func() {
		Expect(makePod("firstpod", "firstnode")).To(Succeed())
		Expect(makePod("secondpod", "secondnode")).To(Succeed())
		deletable, err := GetPodsForDeletion(context.Background(), k8sClient, "firstnode")
		Expect(err).To(Succeed())
		Expect(deletable).To(HaveLen(1))
	})

	It("filters DaemonSets", func() {
		Expect(makePod("firstpod", "node")).To(Succeed())
		Expect(makePod("secondpod", "node")).To(Succeed())
		Expect(makePod("ds", "node", func(p *corev1.Pod) {
			p.OwnerReferences = []v1.OwnerReference{{Kind: "DaemonSet", APIVersion: "apps/v1", Name: "ds", UID: types.UID("ds")}}
		})).To(Succeed())
		deletable, err := GetPodsForDeletion(context.Background(), k8sClient, "node")
		Expect(err).To(Succeed())
		Expect(deletable).To(HaveLen(2))
	})

	It("filters MirrorPods", func() {
		Expect(makePod("firstpod", "node")).To(Succeed())
		Expect(makePod("secondpod", "node")).To(Succeed())
		Expect(makePod("mirror", "node", func(p *corev1.Pod) {
			p.Annotations = map[string]string{corev1.MirrorPodAnnotationKey: TrueString}
		})).To(Succeed())
		deletable, err := GetPodsForDeletion(context.Background(), k8sClient, "node")
		Expect(err).To(Succeed())
		Expect(deletable).To(HaveLen(2))
	})
})

var _ = Describe("ShutdownVM", func() {
	var vCenters *VCenters

	BeforeEach(func() {
		vCenters = &VCenters{
			Template: "http://" + AvailabilityZoneReplacer,
			Credentials: map[string]Credential{
				vcServer.URL.Host: {
					Username: "user",
					Password: "pass",
				},
			},
		}
	})

	AfterEach(func() {
		// power on VM
		client, err := vCenters.Client(context.Background(), vcServer.URL.Host)
		Expect(err).To(Succeed())
		mgr := view.NewManager(client.Client)
		Expect(err).To(Succeed())
		view, err := mgr.CreateContainerView(context.Background(),
			client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		Expect(err).To(Succeed())
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Filter{"name": "DC0_H0_VM0"})
		Expect(err).To(Succeed())
		vm := object.NewVirtualMachine(client.Client, vms[0].Self)
		task, err := vm.PowerOn(context.Background())
		Expect(err).To(Succeed())
		err = task.Wait(context.Background())
		Expect(err).To(Succeed())
	})

	It("should shutdown a VM", func() {
		err := ShutdownVM(context.Background(), vCenters, HostInfo{
			AvailabilityZone: vcServer.URL.Host,
			Name:             HostSystemName,
		}, "DC0_H0_VM0")
		Expect(err).To(Succeed())

		client, err := vCenters.Client(context.Background(), vcServer.URL.Host)
		Expect(err).To(Succeed())
		mgr := view.NewManager(client.Client)
		Expect(err).To(Succeed())
		view, err := mgr.CreateContainerView(context.Background(),
			client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		Expect(err).To(Succeed())
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Filter{"name": "DC0_H0_VM0"})
		Expect(err).To(Succeed())
		result := vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOff
		Expect(result).To(BeTrue())
	})
})

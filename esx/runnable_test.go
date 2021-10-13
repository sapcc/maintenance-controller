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
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	vctypes "github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DefaultNamespace string = "default"

var _ = Describe("The ESX controller", func() {

	var firstNode *corev1.Node
	var secondNode *corev1.Node
	var thirdNode *corev1.Node
	var fourthNode *corev1.Node

	makeNode := func(name, esx string, schedulable, withPods bool) (*corev1.Node, error) {
		node := &corev1.Node{}
		node.Name = name
		node.Namespace = DefaultNamespace
		node.Spec.Unschedulable = !schedulable
		node.Labels = make(map[string]string)
		node.Labels[constants.HostLabelKey] = esx
		node.Labels[constants.FailureDomainLabelKey] = "eu-nl-2a"
		err := k8sClient.Create(context.Background(), node)
		if err != nil {
			return nil, err
		}

		if !withPods {
			return node, nil
		}

		pod := &corev1.Pod{}
		pod.Namespace = DefaultNamespace
		pod.Name = name + "-container"
		pod.Spec.NodeName = name
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		var gracePeriod int64
		pod.Spec.TerminationGracePeriodSeconds = &gracePeriod
		err = k8sClient.Create(context.Background(), pod)
		if err != nil {
			return nil, err
		}
		return node, nil
	}

	BeforeEach(func() {
		var err error
		firstNode, err = makeNode("firstvm", ESXName, true, true)
		Expect(err).To(Succeed())
		secondNode, err = makeNode("secondvm", ESXName, true, true)
		Expect(err).To(Succeed())
		thirdNode, err = makeNode("thirdvm", "DC0_H1", true, false)
		Expect(err).To(Succeed())
		fourthNode, err = makeNode("fourthvm", "DC0_H1", false, false)
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), firstNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), secondNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), thirdNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), fourthNode)
		Expect(err).To(Succeed())

		var podList corev1.PodList
		err = k8sClient.List(context.Background(), &podList)
		Expect(err).To(Succeed())
		var gracePeriod int64
		for i := range podList.Items {
			err = k8sClient.Delete(context.Background(), &podList.Items[i],
				&client.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			Expect(err).To(Succeed())
		}

		vcClient, err := govmomi.NewClient(context.Background(), vcServer.URL, true)
		Expect(err).To(Succeed())
		// set host out of maintenance
		host := object.NewHostSystem(vcClient.Client, vctypes.ManagedObjectReference{
			Type:  "HostSystem",
			Value: "host-21",
		})
		task, err := host.ExitMaintenanceMode(context.Background(), 1000)
		Expect(err).To(Succeed())
		err = task.Wait(context.Background())
		Expect(err).To(Succeed())
	})

	It("labels previously unlabeled nodes", func() {
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "firstvm"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(NoMaintenance)))
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "secondvm"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(NoMaintenance)))
	})

	It("labels all nodes on a single EXS host in case of changes to the maintenance state", func() {
		vcClient, err := govmomi.NewClient(context.Background(), vcServer.URL, true)
		Expect(err).To(Succeed())

		// set host in maintenance
		host := object.NewHostSystem(vcClient.Client, vctypes.ManagedObjectReference{
			Type:  "HostSystem",
			Value: "host-21",
		})
		task, err := host.EnterMaintenanceMode(context.Background(), 1000, false, &vctypes.HostMaintenanceSpec{})
		Expect(err).To(Succeed())
		err = task.Wait(context.Background())
		Expect(err).To(Succeed())

		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "firstvm"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(InMaintenance)))
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "secondvm"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(InMaintenance)))
	})

	It("shuts down nodes on an ESX host if it is in-maintenance and reboots are allowed", func() {
		vcClient, err := govmomi.NewClient(context.Background(), vcServer.URL, true)
		Expect(err).To(Succeed())

		// set host in maintenance
		host := object.NewHostSystem(vcClient.Client, vctypes.ManagedObjectReference{
			Type:  "HostSystem",
			Value: "host-21",
		})
		task, err := host.EnterMaintenanceMode(context.Background(), 1000, false, &vctypes.HostMaintenanceSpec{})
		Expect(err).To(Succeed())
		err = task.Wait(context.Background())
		Expect(err).To(Succeed())

		allowMaintenance := func(node *corev1.Node) error {
			cloned := node.DeepCopy()
			node.Labels[constants.EsxRebootOkLabelKey] = constants.TrueStr
			return k8sClient.Patch(context.Background(), node, client.MergeFrom(cloned))
		}
		Expect(allowMaintenance(firstNode)).To(Succeed())
		Expect(allowMaintenance(secondNode)).To(Succeed())

		Eventually(func() map[string]string {
			node := &corev1.Node{}
			err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			Expect(err).To(Succeed())
			return node.Annotations
		}).Should(HaveKey(constants.EsxRebootInitiatedAnnotationKey))
		Eventually(func() bool {
			node := &corev1.Node{}
			err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeTrue())
		Eventually(func() []corev1.Pod {
			var podList corev1.PodList
			err = k8sClient.List(context.Background(), &podList)
			Expect(err).To(Succeed())
			return podList.Items
		}, 10*time.Second).Should(HaveLen(0))
		Eventually(func() bool {
			mgr := view.NewManager(vcClient.Client)
			Expect(err).To(Succeed())
			view, err := mgr.CreateContainerView(context.Background(),
				vcClient.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
			Expect(err).To(Succeed())
			var vms []mo.VirtualMachine
			err = view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"},
				[]string{"summary.runtime"}, &vms, property.Filter{"name": "firstvm"})
			Expect(err).To(Succeed())
			return vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOff
		}).Should(BeTrue())

		// ensure VM's on different host are not affected
		mgr := view.NewManager(vcClient.Client)
		Expect(err).To(Succeed())
		view, err := mgr.CreateContainerView(context.Background(),
			vcClient.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		Expect(err).To(Succeed())
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Filter{"name": "thirdvm"})
		Expect(err).To(Succeed())
		result := vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn
		Expect(result).To(BeTrue())
		err = view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Filter{"name": "fourthvm"})
		Expect(err).To(Succeed())
		result = vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn
		Expect(result).To(BeTrue())
	})

	It("starts nodes on an ESX host if it is out of maintenance and the controller initiated the shutdown", func() {
		vcClient, err := govmomi.NewClient(context.Background(), vcServer.URL, true)
		Expect(err).To(Succeed())

		markInitiated := func(node *corev1.Node) error {
			cloned := node.DeepCopy()
			node.Spec.Unschedulable = true
			node.Annotations = map[string]string{constants.EsxRebootInitiatedAnnotationKey: constants.TrueStr}
			return k8sClient.Patch(context.Background(), node, client.MergeFrom(cloned))
		}
		Expect(markInitiated(firstNode)).To(Succeed())
		Expect(markInitiated(secondNode)).To(Succeed())

		Eventually(func() bool {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}, 10*time.Second).Should(BeFalse())
		Eventually(func() map[string]string {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			Expect(err).To(Succeed())
			return node.Annotations
		}).ShouldNot(HaveKey(constants.EsxRebootInitiatedAnnotationKey))
		Eventually(func() bool {
			mgr := view.NewManager(vcClient.Client)
			Expect(err).To(Succeed())
			view, err := mgr.CreateContainerView(context.Background(),
				vcClient.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
			Expect(err).To(Succeed())
			var vms []mo.VirtualMachine
			err = view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"},
				[]string{"summary.runtime"}, &vms, property.Filter{"name": "firstvm"})
			Expect(err).To(Succeed())
			return vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn
		}).Should(BeTrue())
	})

})

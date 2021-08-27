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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	vctypes "github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const DefaultNamespace string = "default"

var _ = Describe("The ESX controller", func() {

	var firstNode *corev1.Node
	var secondNode *corev1.Node

	makeNode := func(name string) (*corev1.Node, error) {
		node := &corev1.Node{}
		node.Name = name
		node.Namespace = DefaultNamespace
		node.Labels = make(map[string]string)
		node.Labels[HostLabelKey] = ESXName
		node.Labels[FailureDomainLabelKey] = "eu-nl-2a"
		err := k8sClient.Create(context.Background(), node)
		if err != nil {
			return nil, err
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
		firstNode, err = makeNode("first")
		Expect(err).To(Succeed())
		secondNode, err = makeNode("second")
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), firstNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), secondNode)
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
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "first"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
			return val
		}).Should(Equal(string(NoMaintenance)))
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "second"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
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
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "first"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
			return val
		}).Should(Equal(string(InMaintenance)))
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "second"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
			return val
		}).Should(Equal(string(InMaintenance)))
	})

	// We cant check for actual shutdown of the VMs
	// as their name given by the simulator (DC0_H0_VM0, ...)
	// are not valid Kubernetes node names
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
			node.Labels[RebootOkLabelKey] = TrueString
			return k8sClient.Patch(context.Background(), node, client.MergeFrom(cloned))
		}
		Expect(allowMaintenance(firstNode)).To(Succeed())
		Expect(allowMaintenance(secondNode)).To(Succeed())

		Eventually(func() bool {
			node := &corev1.Node{}
			err = k8sClient.Get(context.Background(), types.NamespacedName{Namespace: DefaultNamespace, Name: "first"}, node)
			Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeTrue())
		Eventually(func() []corev1.Pod {
			var podList corev1.PodList
			err = k8sClient.List(context.Background(), &podList)
			Expect(err).To(Succeed())
			return podList.Items
		}).Should(HaveLen(0))
	})

})

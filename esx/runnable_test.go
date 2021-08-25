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
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("The ESX controller", func() {

	var firstNode *corev1.Node
	var secondNode *corev1.Node

	BeforeEach(func() {
		firstNode = &corev1.Node{}
		firstNode.Name = "first"
		firstNode.Labels = make(map[string]string)
		firstNode.Labels[HostLabelKey] = ESXName
		firstNode.Labels[FailureDomainLabelKey] = "eu-nl-2a"
		err := k8sClient.Create(context.Background(), firstNode)
		Expect(err).To(Succeed())

		secondNode = &corev1.Node{}
		secondNode.Name = "second"
		secondNode.Labels = make(map[string]string)
		secondNode.Labels[HostLabelKey] = ESXName
		secondNode.Labels[FailureDomainLabelKey] = "eu-nl-2a"
		err = k8sClient.Create(context.Background(), secondNode)
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), firstNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), secondNode)
		Expect(err).To(Succeed())
	})

	It("labels previously unlabeled nodes", func() {
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "first"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
			return val
		}, 2*time.Second).Should(Equal(string(NoMaintenance)))
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "second"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
			return val
		}, 2*time.Second).Should(Equal(string(NoMaintenance)))
	})

	It("labels all nodes on a single EXS host in case of changes to the maintenance state", func() {
		vcClient, err := govmomi.NewClient(context.Background(), vcServer.URL, true)
		Expect(err).To(Succeed())

		// set host in maintenance
		host := object.NewHostSystem(vcClient.Client, types.ManagedObjectReference{
			Type:  "HostSystem",
			Value: "host-21",
		})
		task, err := host.EnterMaintenanceMode(context.Background(), 1000, false, &types.HostMaintenanceSpec{})
		Expect(err).To(Succeed())
		err = task.Wait(context.Background())
		Expect(err).To(Succeed())

		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "first"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
			return val
		}, 2*time.Second).Should(Equal(string(InMaintenance)))
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "second"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[MaintenanceLabelKey]
			return val
		}, 2*time.Second).Should(Equal(string(InMaintenance)))
	})

})

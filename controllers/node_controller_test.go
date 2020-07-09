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
	"encoding/json"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/plugin/impl"
	"github.com/sapcc/maintenance-controller/state"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("The controller", func() {

	var targetNode *corev1.Node

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = "targetnode"
		err := k8sClient.Create(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	It("should label a previously unmanaged node", func() {
		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels["state"]
			return val
		}).Should(Equal(string(state.Operational)))
	})

	It("should add the data annotation", func() {
		Eventually(func() bool {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			Expect(err).To(Succeed())

			val := node.Annotations["chain-data"]
			return json.Valid([]byte(val))
		}).Should(BeTrue())
	})

	It("should use the chains described in the annotations", func() {
		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		unmodifiedNode := node.DeepCopy()
		Expect(err).To(Succeed())

		node.Annotations = make(map[string]string)
		node.Labels = make(map[string]string)
		node.Annotations["chain-operational-check"] = "transition"
		node.Annotations["chain-operational-trigger"] = "alter"
		node.Labels["transition"] = "true"
		err = k8sClient.Patch(context.Background(), &node, client.MergeFrom(unmodifiedNode))
		Expect(err).To(Succeed())

		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels["state"]
			return val
		}).Should(Equal(string(state.InMaintenance)))

		err = k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels["alter"]).To(Equal("true"))
	})

})

var _ = Describe("The MaxMaintenance plugin", func() {

	var targetNode *corev1.Node

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = "targetnode"
		targetNode.Labels = make(map[string]string)
		targetNode.Labels["state"] = string(state.InMaintenance)
		err := k8sClient.Create(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	// The test below requires a connection to an api server,
	// which is not simulated within the plugin/impl package
	It("should fetch data from the api server", func() {
		max := impl.MaxMaintenance{MaxNodes: 1}
		result, err := max.Check(plugin.Parameters{Client: k8sClient, StateKey: "state", Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

})

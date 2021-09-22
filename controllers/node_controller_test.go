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
	"fmt"
	"time"

	"github.com/elastic/go-ucfg/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/plugin/impl"
	"github.com/sapcc/maintenance-controller/state"
	"github.com/slack-go/slack/slacktest"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const trueStr = "true"

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

			val := node.Labels[StateLabelKey]
			return val
		}).Should(Equal(string(state.Operational)))
	})

	It("should add the data annotation", func() {
		Eventually(func() bool {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			Expect(err).To(Succeed())

			val := node.Annotations[DataAnnotationKey]
			return json.Valid([]byte(val))
		}).Should(BeTrue())
	})

	It("should use the profile described in the annotation", func() {
		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		unmodifiedNode := node.DeepCopy()

		node.Annotations = make(map[string]string)
		node.Labels = make(map[string]string)
		node.Labels[ProfileLabelKey] = "test"
		node.Labels["transition"] = trueStr
		err = k8sClient.Patch(context.Background(), &node, client.MergeFrom(unmodifiedNode))
		Expect(err).To(Succeed())

		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[StateLabelKey]
			return val
		}).Should(Equal(string(state.InMaintenance)))

		err = k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels["alter"]).To(Equal(trueStr))
		events := &corev1.EventList{}
		err = k8sClient.List(context.Background(), events)
		Expect(err).To(Succeed())
		Expect(events.Items).ToNot(HaveLen(0))
		Expect(events.Items[0].InvolvedObject.UID).To(BeEquivalentTo("targetnode"))
	})

	It("should annotate the last used profile", func() {
		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		unmodifiedNode := node.DeepCopy()

		node.Annotations = make(map[string]string)
		node.Labels = make(map[string]string)
		node.Labels[ProfileLabelKey] = "test"
		node.Labels["transition"] = trueStr
		err = k8sClient.Patch(context.Background(), &node, client.MergeFrom(unmodifiedNode))
		Expect(err).To(Succeed())

		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			Expect(err).To(Succeed())

			dataStr := node.Annotations[DataAnnotationKey]
			fmt.Printf("Data Annotation: %v\n", dataStr)
			var data state.Data
			err = json.Unmarshal([]byte(dataStr), &data)
			Expect(err).To(Succeed())
			return data.LastProfile
		}).Should(Equal("test"))
	})

	It("should follow one profile after leaving the operational state", func() {
		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		unmodifiedNode := node.DeepCopy()

		node.Annotations = make(map[string]string)
		node.Labels = make(map[string]string)
		node.Labels[ProfileLabelKey] = "block--multi"
		node.Labels["transition"] = trueStr
		err = k8sClient.Patch(context.Background(), &node, client.MergeFrom(unmodifiedNode))
		Expect(err).To(Succeed())

		Eventually(func() string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			Expect(err).To(Succeed())

			val := node.Labels[StateLabelKey]
			return val
		}).Should(Equal(string(state.Required)))

		err = k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels).ToNot(HaveKey("alter"))
	})

	It("should parse the count profile", func() {
		config, err := yaml.NewConfig([]byte(config))
		Expect(err).To(Succeed())
		conf, err := LoadConfig(config)
		Expect(err).To(Succeed())
		Expect(conf.Profiles).To(HaveKey("count"))
		profile := conf.Profiles["count"]
		Expect(profile.Name).To(Equal("count"))
		operational := profile.Chains[state.Operational]
		Expect(operational.Check.Plugins).To(HaveLen(1))
		Expect(operational.Notification.Plugins).To(HaveLen(0))
		Expect(operational.Trigger.Plugins).To(HaveLen(1))
		required := profile.Chains[state.Required]
		Expect(required.Check.Plugins).To(HaveLen(2))
		Expect(required.Notification.Plugins).To(HaveLen(0))
		Expect(required.Trigger.Plugins).To(HaveLen(0))
		maintenance := profile.Chains[state.InMaintenance]
		Expect(maintenance.Check.Plugins).To(HaveLen(3))
		Expect(maintenance.Notification.Plugins).To(HaveLen(0))
		Expect(maintenance.Trigger.Plugins).To(HaveLen(0))
	})

})

var _ = Describe("The MaxMaintenance plugin", func() {

	var targetNode *corev1.Node

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = "targetnode"
		targetNode.Labels = make(map[string]string)
		targetNode.Labels[StateLabelKey] = string(state.InMaintenance)
		err := k8sClient.Create(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	// The test below requires a connection to an api server,
	// which is not simulated within the plugin/impl package
	It("should should fail if a node is in maintenance", func() {
		max := impl.MaxMaintenance{MaxNodes: 1}
		result, err := max.Check(plugin.Parameters{Client: k8sClient, StateKey: StateLabelKey, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

	It("should pass if no node is in maintenance", func() {
		patched := targetNode.DeepCopy()
		patched.Labels[StateLabelKey] = string(state.Operational)
		err := k8sClient.Patch(context.Background(), patched, client.MergeFrom(targetNode))
		Expect(err).To(Succeed())
		max := impl.MaxMaintenance{MaxNodes: 1}
		result, err := max.Check(plugin.Parameters{Client: k8sClient, StateKey: StateLabelKey, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

})

var _ = Describe("The stagger plugin", func() {

	var firstNode *corev1.Node
	var secondNode *corev1.Node
	var leaseName types.NamespacedName

	BeforeEach(func() {
		leaseName = types.NamespacedName{
			Namespace: "default",
			Name:      "mc-lease",
		}

		firstNode = &corev1.Node{}
		firstNode.Name = "firstnode"
		err := k8sClient.Create(context.Background(), firstNode)
		Expect(err).To(Succeed())

		secondNode = &corev1.Node{}
		secondNode.Name = "secondnode"
		err = k8sClient.Create(context.Background(), secondNode)
		Expect(err).To(Succeed())
	})

	AfterEach(func() {
		var lease coordinationv1.Lease
		lease.Name = leaseName.Name
		lease.Namespace = leaseName.Namespace
		err := k8sClient.Delete(context.Background(), &lease)
		Expect(err).To(Succeed())

		err = k8sClient.Delete(context.Background(), firstNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), secondNode)
		Expect(err).To(Succeed())
	})

	It("creates the lease object", func() {
		stagger := impl.Stagger{Duration: 3 * time.Second, LeaseName: leaseName}
		result, err := stagger.Check(plugin.Parameters{Client: k8sClient, Node: firstNode, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

	It("blocks within the lease duration", func() {
		stagger := impl.Stagger{Duration: 3 * time.Second, LeaseName: leaseName}
		_, err := stagger.Check(plugin.Parameters{Client: k8sClient, Node: firstNode, Ctx: context.Background()})
		Expect(err).To(Succeed())
		result, err := stagger.Check(plugin.Parameters{Client: k8sClient, Node: secondNode, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

	It("grabs the lease after it timed out", func() {
		stagger := impl.Stagger{Duration: 3 * time.Second, LeaseName: leaseName}
		_, err := stagger.Check(plugin.Parameters{Client: k8sClient, Node: firstNode, Ctx: context.Background()})
		Expect(err).To(Succeed())
		time.Sleep(4 * time.Second)
		result, err := stagger.Check(plugin.Parameters{Client: k8sClient, Node: secondNode, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
		lease := &coordinationv1.Lease{}
		err = k8sClient.Get(context.Background(), leaseName, lease)
		Expect(err).To(Succeed())
		Expect(*lease.Spec.HolderIdentity).To(Equal("secondnode"))
	})

})

var _ = Describe("The slack thread plugin", func() {
	var server *slacktest.Server
	var url string

	BeforeEach(func() {
		server = slacktest.NewTestServer()
		server.Start()
		url = server.GetAPIURL()
	})

	AfterEach(func() {
		server.Stop()
	})

	It("should send a message and create its lease", func() {
		slack := impl.SlackThread{
			Token:   "",
			Title:   "title",
			Channel: "#thechannel",
			Message: "msg",
			LeaseName: types.NamespacedName{
				Name:      "slack-lease",
				Namespace: "default",
			},
			Period: 1 * time.Second,
		}
		slack.SetTestURL(url)
		err := slack.Notify(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Eventually(func() []string {
			return server.GetSeenOutboundMessages()
		}).Should(HaveLen(2))
		Eventually(func() error {
			var lease coordinationv1.Lease
			err := k8sClient.Get(context.Background(), slack.LeaseName, &lease)
			return err
		}).Should(Succeed())
	})
})

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
	"time"

	"github.com/elastic/go-ucfg/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/plugin/impl"
	"github.com/sapcc/maintenance-controller/state"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slacktest"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("The controller", func() {

	var targetNode *corev1.Node

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = "targetnode"
		Expect(k8sClient.Create(context.Background(), targetNode)).To(Succeed())

		events := &corev1.EventList{}
		Expect(k8sClient.List(context.Background(), events)).To(Succeed())
		for _, event := range events.Items {
			Expect(k8sClient.Delete(context.Background(), &event)).To(Succeed())
		}
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	It("should label a previously unmanaged node", func() {
		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.StateLabelKey]
			return val
		}).Should(Equal(string(state.Operational)))
	})

	It("should add the data annotation", func() {
		Eventually(func(g Gomega) bool {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Annotations[constants.DataAnnotationKey]
			return json.Valid([]byte(val))
		}).Should(BeTrue())
	})

	createNodeWithProfile := func(profile string) {
		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		unmodifiedNode := node.DeepCopy()

		node.Annotations = make(map[string]string)
		node.Labels = make(map[string]string)
		node.Labels[constants.ProfileLabelKey] = profile
		node.Labels["transition"] = constants.TrueStr
		err = k8sClient.Patch(context.Background(), &node, client.MergeFrom(unmodifiedNode))
		Expect(err).To(Succeed())
	}

	It("should use the profile described in the label", func() {
		createNodeWithProfile("test")

		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.StateLabelKey]
			return val
		}).Should(Equal(string(state.Required)))

		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels["alter"]).To(Equal(constants.TrueStr))
		data, err := state.ParseData(&node)
		Expect(err).To(Succeed())
		Expect(data.ProfileStates["test"]).To(Equal(state.Required))
		events := &corev1.EventList{}
		err = k8sClient.List(context.Background(), events)
		Expect(err).To(Succeed())
		Expect(events.Items).ToNot(HaveLen(0))
		Expect(events.Items[0].InvolvedObject.UID).To(BeEquivalentTo("targetnode"))
	})

	It("should follow profiles concurrently", func() {
		createNodeWithProfile("block--multi")

		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.StateLabelKey]
			return val
		}).Should(Equal(string(state.InMaintenance)))

		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels).To(HaveKey("alter"))
		data, err := state.ParseData(&node)
		Expect(err).To(Succeed())
		Expect(data.ProfileStates["block"]).To(Equal(state.Required))
		Expect(data.ProfileStates["multi"]).To(Equal(state.InMaintenance))
	})

	It("should use a profile even if other specified profiles have not been configured", func() {
		createNodeWithProfile("does-not-exist--test")

		Eventually(func(g Gomega) map[string]state.NodeStateLabel {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			g.Expect(err).To(Succeed())

			data, err := state.ParseData(&node)
			g.Expect(err).To(Succeed())
			return data.ProfileStates
		}).Should(SatisfyAll(
			Not(HaveKey("does-not-exist")),
			Satisfy(func(ps map[string]state.NodeStateLabel) bool {
				return ps["test"] == state.Required
			}),
		))
	})

	It("should only allow one profile to be in-maintenance concurrently", func() {
		createNodeWithProfile("multi--to-maintenance")

		Eventually(func(g Gomega) string {
			var node corev1.Node
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)).To(Succeed())
			return node.Labels[constants.StateLabelKey]
		}).Should(Equal("in-maintenance"))

		Consistently(func(g Gomega) int {
			var node corev1.Node
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)).To(Succeed())
			data, err := state.ParseData(&node)
			g.Expect(err).To(Succeed())
			var maintenanceCounter int
			for _, val := range data.ProfileStates {
				if val == state.InMaintenance {
					maintenanceCounter++
				}
			}
			return maintenanceCounter
		}).Should(Equal(1))
	})

	It("should cleanup the profile-state map in the data annotation", func() {
		createNodeWithProfile("multi--otherprofile1--otherprofile2")

		Eventually(func(g Gomega) map[string]state.NodeStateLabel {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: "targetnode"}, &node)
			g.Expect(err).To(Succeed())

			data, err := state.ParseData(&node)
			g.Expect(err).To(Succeed())
			return data.ProfileStates
		}).Should(HaveLen(1))
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
		Expect(operational.Transitions[0].Check.Plugins).To(HaveLen(1))
		Expect(operational.Notification.Plugins).To(HaveLen(0))
		Expect(operational.Transitions[0].Trigger.Plugins).To(HaveLen(1))
		required := profile.Chains[state.Required]
		Expect(required.Transitions[0].Check.Plugins).To(HaveLen(2))
		Expect(required.Notification.Plugins).To(HaveLen(0))
		Expect(required.Transitions[0].Trigger.Plugins).To(HaveLen(0))
		maintenance := profile.Chains[state.InMaintenance]
		Expect(maintenance.Transitions[0].Check.Plugins).To(HaveLen(3))
		Expect(maintenance.Notification.Plugins).To(HaveLen(0))
		Expect(maintenance.Transitions[0].Trigger.Plugins).To(HaveLen(0))
	})

})

var _ = Describe("The MaxMaintenance plugin", func() {

	var targetNode *corev1.Node

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = "targetnode"
		targetNode.Labels = make(map[string]string)
		targetNode.Labels[constants.StateLabelKey] = string(state.InMaintenance)
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
		result, err := max.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

	It("should pass if no node is in maintenance", func() {
		patched := targetNode.DeepCopy()
		patched.Labels[constants.StateLabelKey] = string(state.Operational)
		err := k8sClient.Patch(context.Background(), patched, client.MergeFrom(targetNode))
		Expect(err).To(Succeed())
		max := impl.MaxMaintenance{MaxNodes: 1}
		result, err := max.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
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
		var leaseList coordinationv1.LeaseList
		Expect(k8sClient.List(context.Background(), &leaseList)).To(Succeed())
		for i := range leaseList.Items {
			err := k8sClient.Delete(context.Background(), &leaseList.Items[i])
			Expect(err).To(Succeed())
		}
		Expect(k8sClient.Delete(context.Background(), firstNode)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), secondNode)).To(Succeed())
	})

	checkNode := func(stagger *impl.Stagger, node *corev1.Node) bool {
		result, err := stagger.Check(plugin.Parameters{Client: k8sClient, Node: node, Ctx: context.Background()})
		Expect(err).To(Succeed())
		err = stagger.AfterEval(result, plugin.Parameters{Client: k8sClient, Node: node, Ctx: context.Background()})
		Expect(err).To(Succeed())
		return result
	}

	It("blocks within the lease duration", func() {
		stagger := impl.Stagger{
			Duration:       3 * time.Second,
			LeaseName:      leaseName.Name,
			LeaseNamespace: leaseName.Namespace,
			Parallel:       1,
		}
		result := checkNode(&stagger, firstNode)
		Expect(result).To(BeTrue())
		result = checkNode(&stagger, firstNode)
		Expect(result).To(BeFalse())
		result = checkNode(&stagger, secondNode)
		Expect(result).To(BeFalse())
	})

	It("grabs the lease after it timed out", func() {
		stagger := impl.Stagger{
			Duration:       3 * time.Second,
			LeaseName:      leaseName.Name,
			LeaseNamespace: leaseName.Namespace,
			Parallel:       1,
		}
		checkNode(&stagger, firstNode)
		time.Sleep(4 * time.Second)
		result := checkNode(&stagger, secondNode)
		Expect(result).To(BeTrue())
		lease := &coordinationv1.Lease{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Namespace: "default",
			Name:      stagger.LeaseName + "-0",
		}, lease)
		Expect(err).To(Succeed())
		Expect(*lease.Spec.HolderIdentity).To(Equal("secondnode"))
	})

	It("passes for two nodes if parallel is 2", func() {
		stagger := impl.Stagger{
			Duration:       3 * time.Second,
			LeaseName:      leaseName.Name,
			LeaseNamespace: leaseName.Namespace,
			Parallel:       2,
		}
		Expect(checkNode(&stagger, firstNode)).To(BeTrue())
		Expect(checkNode(&stagger, secondNode)).To(BeTrue())
	})

})

var _ = Describe("The slack thread plugin", func() {
	var server *slacktest.Server
	var url string
	var leaseName types.NamespacedName

	BeforeEach(func() {
		leaseName = types.NamespacedName{
			Name:      "slack-lease",
			Namespace: "default",
		}
		server = slacktest.NewTestServer()
		server.Start()
		url = server.GetAPIURL()
	})

	AfterEach(func() {
		lease := &coordinationv1.Lease{}
		Expect(k8sClient.Get(context.Background(), leaseName, lease)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), lease)).To(Succeed())
		server.Stop()
	})

	fetchMessages := func(g Gomega) []slack.Msg {
		msgs := make([]slack.Msg, 0)
		for _, outbound := range server.GetSeenOutboundMessages() {
			msg := slack.Msg{}
			g.Expect(json.Unmarshal([]byte(outbound), &msg)).To(Succeed())
			msgs = append(msgs, msg)
		}
		return msgs
	}

	It("should send a message and create its lease", func() {
		thread := impl.SlackThread{
			Token:     "",
			Title:     "title",
			Channel:   "#thechannel",
			Message:   "msg",
			LeaseName: leaseName,
			Period:    1 * time.Second,
		}
		thread.SetTestURL(url)
		err := thread.Notify(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Eventually(fetchMessages).Should(SatisfyAll(HaveLen(2), Satisfy(func(msgs []slack.Msg) bool {
			return msgs[0].Timestamp == msgs[1].ThreadTimestamp && msgs[0].Text == "title" && msgs[1].Text == "msg"
		})))
		Eventually(func() error {
			var lease coordinationv1.Lease
			err := k8sClient.Get(context.Background(), thread.LeaseName, &lease)
			return err
		}).Should(Succeed())
	})

	It("should use replies if the lease did not timeout", func() {
		thread := impl.SlackThread{
			Token:     "",
			Title:     "title",
			Channel:   "#thechannel",
			Message:   "msg",
			LeaseName: leaseName,
			Period:    5 * time.Second,
		}
		thread.SetTestURL(url)
		err := thread.Notify(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		err = thread.Notify(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Eventually(fetchMessages).Should(SatisfyAll(HaveLen(3), Satisfy(func(msgs []slack.Msg) bool {
			return msgs[0].Timestamp == msgs[1].ThreadTimestamp && msgs[0].Timestamp == msgs[2].ThreadTimestamp
		})))
	})

	It("creates a new thread once the lease times out", func() {
		thread := impl.SlackThread{
			Token:     "",
			Title:     "title",
			Channel:   "#thechannel",
			Message:   "msg",
			LeaseName: leaseName,
			Period:    1 * time.Second,
		}
		thread.SetTestURL(url)
		err := thread.Notify(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		time.Sleep(2 * time.Second)
		err = thread.Notify(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Eventually(fetchMessages).Should(SatisfyAll(HaveLen(4), Satisfy(func(msgs []slack.Msg) bool {
			return msgs[0].Timestamp == msgs[1].ThreadTimestamp && msgs[2].Timestamp == msgs[3].ThreadTimestamp
		})))
	})
})

var _ = Describe("The affinity plugin", func() {
	var firstNode *corev1.Node
	var secondNode *corev1.Node

	BeforeEach(func() {
		firstNode = &corev1.Node{}
		firstNode.Name = "firstnode"
		firstNode.Labels = map[string]string{constants.ProfileLabelKey: "block", "transition": constants.TrueStr}
		err := k8sClient.Create(context.Background(), firstNode)
		Expect(err).To(Succeed())

		secondNode = &corev1.Node{}
		secondNode.Name = "secondnode"
		secondNode.Labels = map[string]string{constants.ProfileLabelKey: "block", "transition": constants.TrueStr}
		err = k8sClient.Create(context.Background(), secondNode)
		Expect(err).To(Succeed())

		awaitMaintenanceRequired := func(node *corev1.Node) {
			Eventually(func(g Gomega) string {
				local := &corev1.Node{}
				err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(node), local)
				g.Expect(err).To(Succeed())
				if local.Labels == nil {
					return ""
				}
				if state, ok := local.Labels[constants.StateLabelKey]; ok {
					return state
				}
				return ""
			}).Should(Equal(string(state.Required)))
		}

		awaitMaintenanceRequired(firstNode)
		awaitMaintenanceRequired(secondNode)
	})

	AfterEach(func() {
		var podList corev1.PodList
		Expect(k8sClient.List(context.Background(), &podList)).To(Succeed())
		var gracePeriod int64
		for i := range podList.Items {
			pod := &podList.Items[i]
			Expect(k8sClient.Delete(context.Background(), pod,
				&client.DeleteOptions{GracePeriodSeconds: &gracePeriod})).To(Succeed())
		}
		Expect(k8sClient.Delete(context.Background(), firstNode)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), secondNode)).To(Succeed())
	})

	buildParams := func(node *corev1.Node) plugin.Parameters {
		return plugin.Parameters{
			Node:    node,
			State:   string(state.Required),
			Client:  k8sClient,
			Ctx:     context.Background(),
			Profile: "block",
		}
	}

	attachAffinityPod := func(nodeName string) {
		pod := &corev1.Pod{}
		pod.Namespace = "default"
		pod.Name = nodeName + "-container"
		pod.Spec.NodeName = nodeName
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		pod.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
					{
						Weight: 1,
						Preference: corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      constants.StateLabelKey,
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{string(state.Operational)},
								},
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), pod)).To(Succeed())
	}

	It("passes if current node has no affinity pod", func() {
		affinity := impl.Affinity{}
		result, err := affinity.Check(buildParams(firstNode))
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

	It("fails if current has an affinity pod and the others don't", func() {
		attachAffinityPod(firstNode.Name)
		affinity := impl.Affinity{}
		result, err := affinity.Check(buildParams(firstNode))
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

	It("passes if all nodes have affinity pods", func() {
		attachAffinityPod(firstNode.Name)
		attachAffinityPod(secondNode.Name)
		affinity := impl.Affinity{}
		result, err := affinity.Check(buildParams(firstNode))
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

	It("fails if node is not in maintenance-required", func() {
		params := buildParams(firstNode)
		params.State = string(state.Operational)
		affinity := impl.Affinity{}
		_, err := affinity.Check(params)
		Expect(err).To(HaveOccurred())
	})

	Context("with transitions caused by different profiles", func() {

		It("passes if one node has an affinity pod and the other has none", func() {
			attachAffinityPod(firstNode.Name)

			affinity := impl.Affinity{}
			params := buildParams(firstNode)
			params.Profile = "otherprofile"
			result, err := affinity.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
		})

	})

	It("does not crash if a pod has no affinity set at all", func() {
		pod := &corev1.Pod{}
		pod.Namespace = "default"
		pod.Name = "container"
		pod.Spec.NodeName = firstNode.Name
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		Expect(k8sClient.Create(context.Background(), pod)).To(Succeed())
		affinity := impl.Affinity{}
		result, err := affinity.Check(buildParams(firstNode))
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
		result, err = affinity.Check(buildParams(secondNode))
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

})

var _ = Describe("The nodecount plugin", func() {
	var node *corev1.Node

	BeforeEach(func() {
		node = &corev1.Node{}
		node.Name = "thenode"
		Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
	})

	It("returns true if a cluster has enough nodes", func() {
		count := impl.NodeCount{Count: 1}
		result, err := count.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

	It("returns false if a cluster does not have enough nodes", func() {
		count := impl.NodeCount{Count: 3}
		result, err := count.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})
})

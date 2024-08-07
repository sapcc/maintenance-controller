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
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slacktest"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/api"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/plugin/impl"
	"github.com/sapcc/maintenance-controller/state"
)

const targetNodeName = "targetnode"

var _ = Describe("The controller", func() {

	var targetNode *corev1.Node

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = targetNodeName
		Expect(k8sClient.Create(context.Background(), targetNode)).To(Succeed())

		events := &corev1.EventList{}
		Expect(k8sClient.List(context.Background(), events)).To(Succeed())
		for i := range events.Items {
			Expect(k8sClient.Delete(context.Background(), &events.Items[i])).To(Succeed())
		}
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	It("should label a previously unmanaged node", func() {
		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.StateLabelKey]
			return val
		}).Should(Equal(string(state.Operational)))
	})

	It("should add the data annotation", func() {
		Eventually(func(g Gomega) bool {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
			g.Expect(err).To(Succeed())

			val := node.Annotations[constants.DataAnnotationKey]
			return json.Valid([]byte(val))
		}).Should(BeTrue())
	})

	createNodeWithProfile := func(profile string) {
		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
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
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.StateLabelKey]
			return val
		}).Should(Equal(string(state.Required)))

		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels["alter"]).To(Equal(constants.TrueStr))
		data, err := state.ParseDataV2(node.Annotations[constants.DataAnnotationKey])
		Expect(err).To(Succeed())
		Expect(data.Profiles["test"].Current).To(Equal(state.Required))
		events := &corev1.EventList{}
		err = k8sClient.List(context.Background(), events)
		Expect(err).To(Succeed())
		Expect(events.Items).ToNot(BeEmpty())
		Expect(events.Items[0].InvolvedObject.UID).To(BeEquivalentTo(targetNodeName))
	})

	It("should follow profiles concurrently", func() {
		createNodeWithProfile("block--multi")

		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.StateLabelKey]
			return val
		}).Should(Equal(string(state.InMaintenance)))

		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels).To(HaveKey("alter"))
		data, err := state.ParseDataV2(node.Annotations[constants.DataAnnotationKey])
		Expect(err).To(Succeed())
		Expect(data.Profiles["block"].Current).To(Equal(state.Required))
		Expect(data.Profiles["multi"].Current).To(Equal(state.InMaintenance))
	})

	// more or less a copy of "should follow profiles concurrently" to ensure we don't break
	// stuff when using detailed logs
	It("should follow profiles concurrently even with detailed logs", func() {
		createNodeWithProfile("block--multi")
		var node corev1.Node
		err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
		Expect(err).To(Succeed())
		unmodified := node.DeepCopy()
		node.Labels[constants.LogDetailsLabelKey] = "true"
		err = k8sClient.Patch(context.Background(), &node, client.MergeFrom(unmodified))
		Expect(err).To(Succeed())

		Eventually(func(g Gomega) string {
			var n corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &n)
			g.Expect(err).To(Succeed())

			val := n.Labels[constants.StateLabelKey]
			return val
		}).Should(Equal(string(state.InMaintenance)))

		err = k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
		Expect(err).To(Succeed())
		Expect(node.Labels).To(HaveKey("alter"))
		data, err := state.ParseDataV2(node.Annotations[constants.DataAnnotationKey])
		Expect(err).To(Succeed())
		Expect(data.Profiles["block"].Current).To(Equal(state.Required))
		Expect(data.Profiles["multi"].Current).To(Equal(state.InMaintenance))
	})

	It("should use a profile even if other specified profiles have not been configured", func() {
		createNodeWithProfile("does-not-exist--test")

		Eventually(func(g Gomega) map[string]*state.ProfileData {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
			g.Expect(err).To(Succeed())

			data, err := state.ParseDataV2(node.Annotations[constants.DataAnnotationKey])
			g.Expect(err).To(Succeed())
			return data.Profiles
		}).Should(SatisfyAll(
			Not(HaveKey("does-not-exist")),
			Satisfy(func(ps map[string]*state.ProfileData) bool {
				return ps["test"] != nil && ps["test"].Current == state.Required
			}),
		))
	})

	It("should only allow one profile to be in-maintenance concurrently", func() {
		createNodeWithProfile("multi--to-maintenance")

		Eventually(func(g Gomega) string {
			var node corev1.Node
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)).To(Succeed())
			return node.Labels[constants.StateLabelKey]
		}).Should(Equal("in-maintenance"))

		Consistently(func(g Gomega) int {
			var node corev1.Node
			g.Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)).To(Succeed())
			data, err := state.ParseDataV2(node.Annotations[constants.DataAnnotationKey])
			g.Expect(err).To(Succeed())
			var maintenanceCounter int
			for _, val := range data.Profiles {
				if val.Current == state.InMaintenance {
					maintenanceCounter++
				}
			}
			return maintenanceCounter
		}).Should(Equal(1))
	})

	It("should cleanup the profile-state map in the data annotation", func() {
		createNodeWithProfile("multi--otherprofile1--otherprofile2")

		Eventually(func(g Gomega) map[string]*state.ProfileData {
			var node corev1.Node
			err := k8sClient.Get(context.Background(), client.ObjectKey{Name: targetNodeName}, &node)
			g.Expect(err).To(Succeed())

			data, err := state.ParseDataV2(node.Annotations[constants.DataAnnotationKey])
			g.Expect(err).To(Succeed())
			return data.Profiles
		}).Should(HaveLen(1))
	})

	It("should parse the count profile", func() {
		config, err := ucfgwrap.FromYAML([]byte(config))
		Expect(err).To(Succeed())
		conf, err := LoadConfig(&config)
		Expect(err).To(Succeed())
		Expect(conf.Profiles).To(HaveKey("count"))
		profile := conf.Profiles["count"]
		Expect(profile.Name).To(Equal("count"))
		operational := profile.Chains[state.Operational]
		Expect(operational.Transitions[0].Check.Plugins).To(HaveLen(1))
		Expect(operational.Notification.Plugins).To(BeEmpty())
		Expect(operational.Transitions[0].Trigger.Plugins).To(HaveLen(1))
		Expect(operational.Enter.Plugins).To(BeEmpty())
		required := profile.Chains[state.Required]
		Expect(required.Transitions[0].Check.Plugins).To(HaveLen(2))
		Expect(required.Notification.Plugins).To(BeEmpty())
		Expect(required.Transitions[0].Trigger.Plugins).To(BeEmpty())
		Expect(required.Enter.Plugins).To(BeEmpty())
		maintenance := profile.Chains[state.InMaintenance]
		Expect(maintenance.Transitions[0].Check.Plugins).To(HaveLen(3))
		Expect(maintenance.Notification.Plugins).To(BeEmpty())
		Expect(maintenance.Transitions[0].Trigger.Plugins).To(BeEmpty())
		Expect(maintenance.Enter.Plugins).To(HaveLen(1))
	})

})

var _ = Describe("The MaxMaintenance plugin", func() {

	var targetNode *corev1.Node

	awaitNodeState := func(node *corev1.Node, state state.NodeStateLabel) {
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
		}).Should(Equal(string(state)))
	}

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = targetNodeName
		targetNode.Labels = make(map[string]string)
		targetNode.Labels[constants.ProfileLabelKey] = "to-maintenance"
		targetNode.Labels["transition"] = constants.TrueStr
		err := k8sClient.Create(context.Background(), targetNode)
		Expect(err).To(Succeed())
		awaitNodeState(targetNode, state.InMaintenance)
	})

	AfterEach(func() {
		err := k8sClient.Delete(context.Background(), targetNode)
		Expect(err).To(Succeed())
	})

	// The test below requires a connection to an api server,
	// which is not simulated within the plugin/impl package
	It("should fail if a node is in maintenance", func() {
		max := impl.MaxMaintenance{MaxNodes: 1}
		result, err := max.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

	It("should pass if no node is in maintenance", func() {
		patched := targetNode.DeepCopy()
		patched.Labels[constants.ProfileLabelKey] = "block"
		err := k8sClient.Patch(context.Background(), patched, client.MergeFrom(targetNode))
		Expect(err).To(Succeed())
		awaitNodeState(patched, state.Required)
		max := impl.MaxMaintenance{MaxNodes: 1}
		result, err := max.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

})

var _ = Describe("The stagger plugin", func() {

	var firstNode *corev1.Node
	var secondNode *corev1.Node
	var leaseName types.NamespacedName

	BeforeEach(func() {
		leaseName = types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
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
		err = stagger.OnTransition(plugin.Parameters{Client: k8sClient, Node: node, Ctx: context.Background()})
		Expect(err).To(Succeed())
		return result.Passed
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
			Duration:       1 * time.Second,
			LeaseName:      leaseName.Name,
			LeaseNamespace: leaseName.Namespace,
			Parallel:       1,
		}
		checkNode(&stagger, firstNode)
		time.Sleep(2 * time.Second)
		result := checkNode(&stagger, secondNode)
		Expect(result).To(BeTrue())
		lease := &coordinationv1.Lease{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
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

	It("does not grab the lease if it did not contribute to passing the check chain", func() {
		stagger := impl.Stagger{
			Duration:       3 * time.Second,
			LeaseName:      leaseName.Name,
			LeaseNamespace: leaseName.Namespace,
			Parallel:       1,
		}
		result := checkNode(&stagger, firstNode)
		Expect(result).To(BeTrue())
		result = checkNode(&stagger, secondNode)
		Expect(result).To(BeFalse())
		lease := &coordinationv1.Lease{}
		err := k8sClient.Get(context.Background(), types.NamespacedName{
			Namespace: metav1.NamespaceDefault,
			Name:      stagger.LeaseName + "-0",
		}, lease)
		Expect(err).To(Succeed())
		Expect(*lease.Spec.HolderIdentity).To(Equal("firstnode"))
	})

})

var _ = Describe("The slack thread plugin", func() {
	var server *slacktest.Server
	var url string
	var leaseName types.NamespacedName

	BeforeEach(func() {
		leaseName = types.NamespacedName{
			Name:      "slack-lease",
			Namespace: metav1.NamespaceDefault,
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
			return msgs[0].Timestamp == msgs[1].ThreadTimestamp &&
				msgs[0].Text == "title" && msgs[1].Text == "msg"
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
		pod.Namespace = metav1.NamespaceDefault
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
		Expect(result.Passed).To(BeTrue())
	})

	It("fails if current has an affinity pod and the others don't", func() {
		attachAffinityPod(firstNode.Name)
		affinity := impl.Affinity{}
		result, err := affinity.Check(buildParams(firstNode))
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

	It("passes if all nodes have affinity pods", func() {
		attachAffinityPod(firstNode.Name)
		attachAffinityPod(secondNode.Name)
		affinity := impl.Affinity{}
		result, err := affinity.Check(buildParams(firstNode))
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

	It("fails if node is not in maintenance-required", func() {
		params := buildParams(firstNode)
		params.State = string(state.Operational)
		affinity := impl.Affinity{}
		_, err := affinity.Check(params)
		Expect(err).To(HaveOccurred())
	})

	It("does not crash if a pod has no affinity set at all", func() {
		pod := &corev1.Pod{}
		pod.Namespace = metav1.NamespaceDefault
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
		Expect(result.Passed).To(BeTrue())
		result, err = affinity.Check(buildParams(secondNode))
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

	Context("with transitions caused by different profiles", func() {

		It("passes if one node has an affinity pod and the other has none", func() {
			attachAffinityPod(firstNode.Name)

			affinity := impl.Affinity{}
			params := buildParams(firstNode)
			params.Profile = "otherprofile"
			result, err := affinity.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

	})

	Context("with a third operational node and minOperational set to 1", func() {

		var thirdNode *corev1.Node

		BeforeEach(func() {
			thirdNode = &corev1.Node{}
			thirdNode.Name = "thirdnode"
			thirdNode.Labels = map[string]string{constants.ProfileLabelKey: "block"}
			err := k8sClient.Create(context.Background(), thirdNode)
			Expect(err).To(Succeed())

			Eventually(func(g Gomega) map[string]string {
				g.Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(thirdNode), thirdNode)).To(Succeed())
				return thirdNode.Annotations
			}).Should(HaveKey(constants.DataAnnotationKey))
		})

		AfterEach(func() {
			Expect(k8sClient.Delete(context.Background(), thirdNode)).To(Succeed())
		})

		It("passes if current has an affinity pod and the others don't", func() {
			attachAffinityPod(firstNode.Name)
			affinity := impl.Affinity{MinOperational: 1}
			result, err := affinity.Check(buildParams(firstNode))
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

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
		Expect(result.Passed).To(BeTrue())
	})

	It("returns false if a cluster does not have enough nodes", func() {
		count := impl.NodeCount{Count: 3}
		result, err := count.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background()})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})
})

var _ = Describe("The clusterSemver plugin", func() {
	var maxnode *corev1.Node
	var minnode *corev1.Node
	var invalid *corev1.Node
	var noversion *corev1.Node

	BeforeEach(func() {
		maxnode = &corev1.Node{}
		maxnode.Name = "maxnode"
		maxnode.Labels = map[string]string{"version": "2.3.1"}
		Expect(k8sClient.Create(context.Background(), maxnode)).To(Succeed())
		minnode = &corev1.Node{}
		minnode.Name = "minnode"
		minnode.Labels = map[string]string{"version": "1.34.7"}
		Expect(k8sClient.Create(context.Background(), minnode)).To(Succeed())
		invalid = &corev1.Node{}
		invalid.Name = "invalid"
		invalid.Labels = map[string]string{"version": "thiswillnotparse"}
		Expect(k8sClient.Create(context.Background(), invalid)).To(Succeed())
		noversion = &corev1.Node{}
		noversion.Name = "noversion"
		Expect(k8sClient.Create(context.Background(), noversion)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), maxnode)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), minnode)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), invalid)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), noversion)).To(Succeed())
	})

	It("returns false if for an up-to-date node", func() {
		cs := impl.ClusterSemver{Key: "version"}
		result, err := cs.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background(), Node: maxnode})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

	It("passes for an outdated node", func() {
		cs := impl.ClusterSemver{Key: "version"}
		result, err := cs.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background(), Node: minnode})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

	It("returns false for a cluster-wide outdated node if scoped to a profile with no nodes", func() {
		cs := impl.ClusterSemver{Key: "version", ProfileScoped: true}
		result, err := cs.Check(plugin.Parameters{
			Client:  k8sClient,
			Ctx:     context.Background(),
			Node:    minnode,
			Profile: "does-not-exist",
		})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

	It("fails for checked node with invalid version label", func() {
		cs := impl.ClusterSemver{Key: "version"}
		_, err := cs.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background(), Node: invalid})
		Expect(err).ToNot(Succeed())
	})

	It("fails for checked node without version label", func() {
		cs := impl.ClusterSemver{Key: "version"}
		_, err := cs.Check(plugin.Parameters{Client: k8sClient, Ctx: context.Background(), Node: noversion})
		Expect(err).ToNot(Succeed())
	})
})

var _ = Describe("The api server", func() {

	var targetNode *corev1.Node
	var dsPod *corev1.Pod
	var rsPod *corev1.Pod
	var ssPod *corev1.Pod
	var daemonSet *appsv1.DaemonSet
	var replicaSet *appsv1.ReplicaSet
	var statefulSet *appsv1.StatefulSet
	var stopServer context.CancelFunc
	var metricsServer api.Server

	BeforeEach(func() {
		targetNode = &corev1.Node{}
		targetNode.Name = targetNodeName
		targetNode.Labels = map[string]string{constants.ProfileLabelKey: "multi"}
		Expect(k8sClient.Create(context.Background(), targetNode)).To(Succeed())
		elected := make(chan struct{})
		close(elected)

		metricsServer = api.Server{
			Address:       ":15423",
			WaitTimeout:   50 * time.Millisecond,
			Log:           GinkgoLogr,
			NodeInfoCache: nodeInfoCache,
			StaticPath:    "../static",
			Namespace:     metav1.NamespaceDefault,
			Client:        k8sClient,
			Elected:       elected,
		}
		withCancel, cancel := context.WithCancel(context.Background())
		stopServer = cancel
		go func() {
			defer GinkgoRecover()
			err := metricsServer.Start(withCancel)
			Expect(err).To(Succeed())
		}()
	})

	AfterEach(func() {
		stopServer()
		err := k8sClient.Delete(context.Background(), targetNode)
		Expect(err).To(Succeed())
		Eventually(metricsServer.Done(), 2).Should(BeClosed())
	})

	parseMetric := func(all, metric string) string {
		splitted := strings.Split(all, "\n")
		for _, line := range splitted {
			if strings.HasPrefix(line, metric) {
				return strings.Split(line, " ")[1]
			}
		}
		return ""
	}

	parseMetrics := func(all string, metrics []string) []string {
		result := make([]string, 0)
		for _, metric := range metrics {
			result = append(result, parseMetric(all, metric))
		}
		return result
	}

	It("should create DaemonSet shuffle metrics", func() {
		daemonSet = &appsv1.DaemonSet{}
		daemonSet.Name = "ds"
		daemonSet.Namespace = metav1.NamespaceDefault
		daemonSet.Spec.Template.Labels = map[string]string{"selector": "val"}
		daemonSet.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"selector": "val"}}
		daemonSet.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:  "container",
				Image: "nginx",
			},
		}
		Expect(k8sClient.Create(context.Background(), daemonSet)).To(Succeed())
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(daemonSet), daemonSet)).To(Succeed())

		dsPod = &corev1.Pod{}
		dsPod.Name = "a-happy-pod"
		dsPod.Namespace = metav1.NamespaceDefault
		dsPod.Spec.NodeName = targetNodeName
		dsPod.Spec.Containers = daemonSet.Spec.Template.Spec.Containers
		dsPod.OwnerReferences = []metav1.OwnerReference{
			{
				Kind:       "DaemonSet",
				Name:       "ds",
				APIVersion: "apps/v1",
				UID:        daemonSet.UID,
			},
		}
		Expect(k8sClient.Create(context.Background(), dsPod)).To(Succeed())

		// Trigger the maintenance after resource that should be recorded have been created
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(targetNode), targetNode)).To(Succeed())
		unmodified := targetNode.DeepCopy()
		targetNode.Labels["transition"] = constants.TrueStr
		Expect(k8sClient.Patch(context.Background(), targetNode, client.MergeFrom(unmodified))).To(Succeed())
		Eventually(func(g Gomega) []string {
			time.Sleep(2 * time.Second)
			res, err := http.Get("http://localhost:15423/metrics")
			g.Expect(err).To(Succeed())
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			g.Expect(err).To(Succeed())
			return parseMetrics(string(data), []string{
				"maintenance_controller_pod_shuffle_count{owner=\"daemon_set_ds\",profile=\"multi\"}",
				"maintenance_controller_pod_shuffles_per_replica{owner=\"daemon_set_ds\",profile=\"multi\"}",
			})
		}).Should(Equal([]string{"1", "+Inf"}))

		err := k8sClient.Delete(context.Background(), daemonSet)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), dsPod, client.GracePeriodSeconds(0))
		Expect(err).To(Succeed())
	})

	It("should create ReplicaSet shuffle metrics", func() {
		replicaSet = &appsv1.ReplicaSet{}
		replicaSet.Name = "rs"
		replicaSet.Namespace = metav1.NamespaceDefault
		replicas := int32(1)
		replicaSet.Spec.Replicas = &replicas
		replicaSet.Spec.Template.Labels = map[string]string{"selector": "val2"}
		replicaSet.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"selector": "val2"}}
		replicaSet.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:  "container",
				Image: "nginx",
			},
		}
		Expect(k8sClient.Create(context.Background(), replicaSet)).To(Succeed())
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(replicaSet), replicaSet)).To(Succeed())

		rsPod = &corev1.Pod{}
		rsPod.Name = "another-happy-pod"
		rsPod.Namespace = metav1.NamespaceDefault
		rsPod.Spec.NodeName = targetNodeName
		rsPod.Spec.Containers = replicaSet.Spec.Template.Spec.Containers
		rsPod.OwnerReferences = []metav1.OwnerReference{
			{
				Kind:       "ReplicaSet",
				Name:       "rs",
				APIVersion: "apps/v1",
				UID:        replicaSet.UID,
			},
		}
		Expect(k8sClient.Create(context.Background(), rsPod)).To(Succeed())

		// Trigger the maintenance after resource that should be recorded have been created
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(targetNode), targetNode)).To(Succeed())
		unmodified := targetNode.DeepCopy()
		targetNode.Labels["transition"] = constants.TrueStr
		Expect(k8sClient.Patch(context.Background(), targetNode, client.MergeFrom(unmodified))).To(Succeed())

		Eventually(func(g Gomega) []string {
			res, err := http.Get("http://localhost:15423/metrics")
			g.Expect(err).To(Succeed())
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			g.Expect(err).To(Succeed())
			return parseMetrics(string(data), []string{
				"maintenance_controller_pod_shuffle_count{owner=\"replica_set_rs\",profile=\"multi\"}",
				"maintenance_controller_pod_shuffles_per_replica{owner=\"replica_set_rs\",profile=\"multi\"}",
			})
		}).Should(Equal([]string{"1", "1"}))
		err := k8sClient.Delete(context.Background(), replicaSet)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), rsPod, client.GracePeriodSeconds(0))
		Expect(err).To(Succeed())
	})

	It("should create StatefulSet shuffle metrics", func() {
		replicas := int32(1)
		statefulSet = &appsv1.StatefulSet{}
		statefulSet.Name = "ss"
		statefulSet.Namespace = metav1.NamespaceDefault
		statefulSet.Spec.Replicas = &replicas
		statefulSet.Spec.Template.Labels = map[string]string{"selector": "val3"}
		statefulSet.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{"selector": "val3"}}
		statefulSet.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:  "container",
				Image: "nginx",
			},
		}
		Expect(k8sClient.Create(context.Background(), statefulSet)).To(Succeed())
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(statefulSet), statefulSet)).To(Succeed())

		ssPod = &corev1.Pod{}
		ssPod.Name = "stateful-happy-pod"
		ssPod.Namespace = metav1.NamespaceDefault
		ssPod.Spec.NodeName = targetNodeName
		ssPod.Spec.Containers = statefulSet.Spec.Template.Spec.Containers
		ssPod.OwnerReferences = []metav1.OwnerReference{
			{
				Kind:       "StatefulSet",
				Name:       "ss",
				APIVersion: "apps/v1",
				UID:        statefulSet.UID,
			},
		}
		Expect(k8sClient.Create(context.Background(), ssPod)).To(Succeed())

		// Trigger the maintenance after resource that should be recorded have been created
		Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(targetNode), targetNode)).To(Succeed())
		unmodified := targetNode.DeepCopy()
		targetNode.Labels["transition"] = constants.TrueStr
		Expect(k8sClient.Patch(context.Background(), targetNode, client.MergeFrom(unmodified))).To(Succeed())

		Eventually(func(g Gomega) []string {
			res, err := http.Get("http://localhost:15423/metrics")
			g.Expect(err).To(Succeed())
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			g.Expect(err).To(Succeed())
			return parseMetrics(string(data), []string{
				"maintenance_controller_pod_shuffle_count{owner=\"stateful_set_ss\",profile=\"multi\"}",
				"maintenance_controller_pod_shuffles_per_replica{owner=\"stateful_set_ss\",profile=\"multi\"}",
			})
		}).Should(Equal([]string{"1", "1"}))
		err := k8sClient.Delete(context.Background(), statefulSet)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(context.Background(), ssPod, client.GracePeriodSeconds(0))
		Expect(err).To(Succeed())
	})

	It("should create transition failure metrics", func(ctx SpecContext) {
		originalNode := targetNode.DeepCopy()
		targetNode.Labels = map[string]string{constants.ProfileLabelKey: "broken"}
		Expect(k8sClient.Patch(ctx, targetNode, client.MergeFrom(originalNode))).To(Succeed())

		Eventually(func(g Gomega) []string {
			res, err := http.Get("http://localhost:15423/metrics")
			g.Expect(err).To(Succeed())
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			g.Expect(err).To(Succeed())
			return parseMetrics(string(data), []string{
				"maintenance_controller_transition_failure_count{profile=\"broken\"}",
			})
		}).Should(Equal([]string{"5"}))
	})

	It("should return node infos", func() {
		// since the cache is global the precise number
		// of nodes is unknown for the cache
		Eventually(func(g Gomega) []any {
			res, err := http.Get("http://localhost:15423/api/v1/info")
			g.Expect(err).To(Succeed())
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			g.Expect(err).To(Succeed())
			nodes := make([]any, 0)
			Expect(json.Unmarshal(data, &nodes)).To(Succeed())
			return nodes
		}).ShouldNot(BeEmpty())
	})

	It("should serve the dashboard", func() {
		res, err := http.Get("http://localhost:15423")
		Expect(err).To(Succeed())
		Expect(res.StatusCode).To(Equal(http.StatusOK))
		Expect(res.ContentLength).To(BeNumerically(">", 0))
	})

})

var _ = Describe("The AnyLabel plugin", func() {

	var firstNode *corev1.Node
	var secondNode *corev1.Node

	BeforeEach(func() {
		firstNode = &corev1.Node{}
		firstNode.Name = "onenode"
		firstNode.Labels = map[string]string{"label": "gopher"}
		Expect(k8sClient.Create(context.Background(), firstNode)).To(Succeed())
		secondNode = &corev1.Node{}
		secondNode.Name = "twonode"
		Expect(k8sClient.Create(context.Background(), secondNode)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), firstNode)).To(Succeed())
		Expect(k8sClient.Delete(context.Background(), secondNode)).To(Succeed())
	})

	It("should pass if label=gopher is configured", func() {
		anyLabel := impl.AnyLabel{Key: "label", Value: "gopher"}
		result, err := anyLabel.Check(plugin.Parameters{Ctx: context.Background(), Client: k8sClient})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

	It("should pass if label='' is configured", func() {
		anyLabel := impl.AnyLabel{Key: "label", Value: ""}
		result, err := anyLabel.Check(plugin.Parameters{Ctx: context.Background(), Client: k8sClient})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

	It("should block if label=something", func() {
		anyLabel := impl.AnyLabel{Key: "label", Value: "something"}
		result, err := anyLabel.Check(plugin.Parameters{Ctx: context.Background(), Client: k8sClient})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

	It("should block if zone=''", func() {
		anyLabel := impl.AnyLabel{Key: "zone", Value: ""}
		result, err := anyLabel.Check(plugin.Parameters{Ctx: context.Background(), Client: k8sClient})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

})

var _ = Describe("The eviction plugin", func() {

	var node *corev1.Node
	var pod *corev1.Pod

	BeforeEach(func() {
		node = &corev1.Node{}
		node.Name = "evict-node"
		Expect(k8sClient.Create(context.Background(), node)).To(Succeed())

		pod = &corev1.Pod{}
		pod.Name = "evict-pod"
		pod.Namespace = metav1.NamespaceDefault
		pod.Spec.NodeName = node.Name
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		Expect(k8sClient.Create(context.Background(), pod)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(
			context.Background(),
			pod,
			&client.DeleteOptions{GracePeriodSeconds: ptr.To(int64(0))},
		)).To(Succeed())
		Eventually(func(g Gomega) []corev1.Pod {
			pods, err := k8sClientset.CoreV1().Pods(metav1.NamespaceDefault).List(context.Background(), metav1.ListOptions{})
			g.Expect(err).To(Succeed())
			return pods.Items
		}).Should(BeEmpty())
		Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
	})

	It("should mark a node as unschedulable with cordon action", func(ctx SpecContext) {
		eviction := impl.Eviction{Action: impl.Cordon}
		err := eviction.Trigger(plugin.Parameters{Ctx: ctx, Client: k8sClient, Node: node})
		Expect(err).To(Succeed())
		Eventually(func(g Gomega) bool {
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(node), node)
			g.Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeTrue())
	})

	It("should mark a node as schedulable with uncordon action", func(ctx SpecContext) {
		originalNode := node.DeepCopy()
		node.Spec.Unschedulable = true
		Expect(k8sClient.Patch(ctx, node, client.MergeFrom(originalNode))).To(Succeed())

		eviction := impl.Eviction{Action: impl.Uncordon}
		err := eviction.Trigger(plugin.Parameters{Ctx: ctx, Client: k8sClient, Node: node})
		Expect(err).To(Succeed())
		Eventually(func(g Gomega) bool {
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(node), node)
			g.Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeFalse())
	})

	It("should evict pods with the drain action", func(ctx SpecContext) {
		eviction := impl.Eviction{Action: impl.Drain, DeletionTimeout: time.Second, EvictionTimeout: time.Minute}
		params := plugin.Parameters{Ctx: ctx, Client: k8sClient, Clientset: k8sClientset, Node: node, Log: GinkgoLogr}
		err := eviction.Trigger(params)
		Expect(err).To(HaveOccurred()) // awaiting the pod deletions fails because there is no kubelet running
		Eventually(func(g Gomega) bool {
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(node), node)
			g.Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeTrue())
		Eventually(func(g Gomega) *metav1.Time {
			err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(pod), pod)
			g.Expect(err).To(Succeed())
			return pod.DeletionTimestamp
		}).ShouldNot(BeNil())
	})

})

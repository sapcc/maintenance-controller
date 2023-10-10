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

package e2e_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	coordiantionv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

const TrueString = "true"

var k8sClient client.Client
var maintainedKey client.ObjectKey

func leadingPodName() string {
	var leaderLease coordiantionv1.Lease
	err := k8sClient.Get(context.Background(), types.NamespacedName{
		Namespace: "kube-system",
		Name:      constants.LeaderElectionID,
	}, &leaderLease)
	Expect(err).To(Succeed())
	Expect(leaderLease.Spec.HolderIdentity).ToNot(BeNil())
	return strings.Split(*leaderLease.Spec.HolderIdentity, "_")[0]
}

func leadingNode() *corev1.Node {
	leadingPod := &corev1.Pod{}
	err := k8sClient.Get(context.Background(), types.NamespacedName{
		Namespace: "kube-system",
		Name:      leadingPodName(),
	}, leadingPod)
	Expect(err).To(Succeed())
	leadingNodeName := types.NamespacedName{Namespace: "default", Name: leadingPod.Spec.NodeName}
	leadingNode := &corev1.Node{}
	err = k8sClient.Get(context.Background(), leadingNodeName, leadingNode)
	Expect(err).To(Succeed())
	return leadingNode
}

var _ = BeforeSuite(func() {
	SetDefaultEventuallyTimeout(1 * time.Minute)
	// Sleep so flatcar agent can come up
	time.Sleep(20 * time.Second)

	cfg := config.GetConfigOrDie()
	client, err := client.New(cfg, client.Options{})
	Expect(err).To(Succeed())
	k8sClient = client

	clientset, err := kubernetes.NewForConfig(cfg)
	Expect(err).To(Succeed())
	req := clientset.CoreV1().Pods("kube-system").GetLogs(leadingPodName(), &v1.PodLogOptions{})
	logs, err := req.Stream(context.Background())
	Expect(err).To(Succeed())
	defer logs.Close()
	var buf bytes.Buffer
	_, err = io.Copy(&buf, logs)
	Expect(err).To(Succeed())
	str := buf.String()
	Expect(str).To(ContainSubstring("Starting worker"))
	// Failure below would indicate a permission issue
	Expect(str).ToNot(ContainSubstring("is forbidden"))
})

var _ = Describe("The maintenance controller", func() {

	It("performs the flatcar maintenance", func() {
		By("node precheck")
		nodeList := &corev1.NodeList{}
		err := k8sClient.List(context.Background(), nodeList)
		Expect(err).To(Succeed())
		for _, node := range nodeList.Items {
			Expect(node.Labels[constants.StateLabelKey]).To(Equal("operational"))
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady {
					Expect(condition.Status).To(Equal(corev1.ConditionTrue))
				}
			}
		}

		By("setup nodes")
		for i := range nodeList.Items {
			node := &nodeList.Items[i]
			unmodified := node.DeepCopy()
			node.Labels[constants.ProfileLabelKey] = "flatcar"
			err = k8sClient.Patch(context.Background(), node, client.MergeFrom(unmodified))
			Expect(err).To(Succeed())
		}
		maintainedNode := leadingNode()
		maintainedKey = client.ObjectKeyFromObject(maintainedNode)
		unmodified := maintainedNode.DeepCopy()
		maintainedNode.Annotations["flatcar-linux-update.v1.flatcar-linux.net/reboot-needed"] = TrueString
		err = k8sClient.Patch(context.Background(), maintainedNode, client.MergeFrom(unmodified))
		Expect(err).To(Succeed())

		Eventually(func(g Gomega) string {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			return node.Labels[constants.StateLabelKey]
		}).Should(Equal("maintenance-required"))

		By("approve maintenance")
		unmodified = maintainedNode.DeepCopy()
		maintainedNode.Annotations["cloud.sap/maintenance-approved"] = TrueString
		err = k8sClient.Patch(context.Background(), maintainedNode, client.MergeFrom(unmodified))
		Expect(err).To(Succeed())

		Eventually(func(g Gomega) string {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			return node.Labels[constants.StateLabelKey]
		}).Should(Equal("in-maintenance"))
		Eventually(func(g Gomega) bool {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			return node.Annotations["flatcar-linux-update.v1.flatcar-linux.net/reboot-ok"] == TrueString &&
				node.Annotations["flatcar-linux-update.v1.flatcar-linux.net/reboot-needed"] == TrueString
		}).Should(BeTrue())

		// node may reboot to fast to become NotReady
		By("check node schedulable")
		Eventually(func(g Gomega) bool {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}, 2*time.Minute).Should(BeTrue())

		Eventually(func(g Gomega) bool {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}, 2*time.Minute).Should(BeFalse())

		By("check operational")
		Eventually(func(g Gomega) string {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			return node.Labels[constants.StateLabelKey]
		}).Should(Equal("operational"))
	})

	It("should generate events for the maintained node", func() {
		eventList := &corev1.EventList{}
		err := k8sClient.List(context.Background(), eventList, client.MatchingFields{
			"involvedObject.name": maintainedKey.Name,
			"reason":              "ChangedMaintenanceState",
		})
		Expect(err).To(Succeed())
		Expect(eventList.Items).To(HaveLen(3))
	})

	It("should remove the maintenance approval", func() {
		Eventually(func(g Gomega) bool {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			_, exists := node.Labels["cloud.sap/maintenance-approved"]
			return exists
		}).Should(BeFalse())
	})

	It("should remove the flatcar approval", func() {
		Eventually(func(g Gomega) bool {
			node := &corev1.Node{}
			err := k8sClient.Get(context.Background(), maintainedKey, node)
			g.Expect(err).To(Succeed())
			_, exists := node.Labels["flatcar-linux-update.v1.flatcar-linux.net/reboot-ok"]
			return exists
		}).Should(BeFalse())
	})

	It("should recreate nodes with the kubernikus controller", func() {
		By("fetch node names")
		nodes := &corev1.NodeList{}
		Expect(k8sClient.List(context.Background(), nodes)).To(Succeed())
		nodeNames := make([]string, 0)
		for _, node := range nodes.Items {
			nodeNames = append(nodeNames, node.Name)
		}
		By("mark node for deletion")
		toDelete := &nodes.Items[0]
		unmodified := toDelete.DeepCopy()
		toDelete.Labels[constants.DeleteNodeLabelKey] = constants.TrueStr
		Expect(k8sClient.Patch(context.Background(), toDelete, client.MergeFrom(unmodified))).To(Succeed())
		By("assert node gets deleted")
		Eventually(func(g Gomega) []string {
			nodes := &corev1.NodeList{}
			g.Expect(k8sClient.List(context.Background(), nodes)).To(Succeed())
			nodeNames := make([]string, 0)
			for _, node := range nodes.Items {
				nodeNames = append(nodeNames, node.Name)
			}
			return nodeNames
		}, 5*time.Minute).Should(HaveLen(1))
		By("assert an other node gets added")
		Eventually(func(g Gomega) []string {
			nodes := &corev1.NodeList{}
			g.Expect(k8sClient.List(context.Background(), nodes)).To(Succeed())
			nodeNames := make([]string, 0)
			for _, node := range nodes.Items {
				nodeNames = append(nodeNames, node.Name)
			}
			return nodeNames
		}, 5*time.Minute).ShouldNot(Equal(nodeNames))
		By("assert that all nodes become ready")
		Eventually(func(g Gomega) bool {
			nodes := &corev1.NodeList{}
			g.Expect(k8sClient.List(context.Background(), nodes)).To(Succeed())
			for _, node := range nodes.Items {
				for _, condition := range node.Status.Conditions {
					if condition.Type == corev1.NodeReady && condition.Status != corev1.ConditionTrue {
						return false
					}
				}
			}
			return true
		}, 5*time.Minute).Should(BeTrue())
	})

})

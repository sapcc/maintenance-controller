// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package kubernikus

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
)

var _ = Describe("The kubernikus controller", func() {
	var node *v1.Node
	var nodeName types.NamespacedName

	initNode := func(ctx context.Context, version string) {
		node = &v1.Node{}
		node.Name = nodeName.Name
		node.Status.NodeInfo.KubeletVersion = version
		Expect(k8sClient.Create(ctx, node)).To(Succeed())
	}

	makePod := func(ctx context.Context, podName, nodeName string, custom ...func(*v1.Pod)) error {
		var graceSeconds int64
		var pod v1.Pod
		pod.Namespace = "default"
		pod.Name = podName
		pod.Spec.NodeName = nodeName
		pod.Spec.Containers = []v1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		pod.Spec.TerminationGracePeriodSeconds = &graceSeconds
		for _, cust := range custom {
			cust(&pod)
		}
		return k8sClient.Create(ctx, &pod)
	}

	BeforeEach(func() {
		node = nil
		nodeName = types.NamespacedName{Namespace: "default", Name: "thenode"}
	})

	AfterEach(func(ctx SpecContext) {
		if node != nil {
			Expect(k8sClient.Delete(ctx, node)).To(Succeed())
			Eventually(func(g Gomega) int {
				var nodes v1.NodeList
				g.Expect(k8sClient.List(ctx, &nodes)).To(Succeed())
				return len(nodes.Items)
			}).Should(Equal(0))
		}
	})

	It("marks an outdated node for update", func(ctx SpecContext) {
		initNode(ctx, "v1.1.0")
		Eventually(func(g Gomega) string {
			result := &v1.Node{}
			g.Expect(k8sClient.Get(ctx, nodeName, result)).To(Succeed())
			g.Expect(result.Labels).To(HaveKey(constants.KubeletUpdateLabelKey))
			return result.Labels[constants.KubeletUpdateLabelKey]
		}).Should(Equal(constants.TrueStr))
	})

	It("marks an up-to-date node as not needing an update", func(ctx SpecContext) {
		clientset, err := kubernetes.NewForConfig(cfg)
		Expect(err).To(Succeed())
		version, err := common.GetAPIServerVersion(clientset)
		Expect(err).To(Succeed())
		initNode(ctx, fmt.Sprintf("v%s", version))
		Eventually(func(g Gomega) string {
			result := &v1.Node{}
			g.Expect(k8sClient.Get(ctx, nodeName, result)).To(Succeed())
			g.Expect(result.Labels).To(HaveKey(constants.KubeletUpdateLabelKey))
			return result.Labels[constants.KubeletUpdateLabelKey]
		}).Should(Equal("false"))
	})

	It("marks a node needing a downgrade", func(ctx SpecContext) {
		initNode(ctx, "v1.156.2")
		Eventually(func(g Gomega) string {
			result := &v1.Node{}
			g.Expect(k8sClient.Get(ctx, nodeName, result)).To(Succeed())
			g.Expect(result.Labels).To(HaveKey(constants.KubeletUpdateLabelKey))
			return result.Labels[constants.KubeletUpdateLabelKey]
		}).Should(Equal(constants.TrueStr))
	})

	It("deletes nodes marked for deletion", func(ctx SpecContext) {
		initNode(ctx, "v1.19.2")
		Expect(makePod(ctx, "thepod", nodeName.Name)).To(Succeed())
		unmodified := node.DeepCopy()
		node.Labels = map[string]string{constants.DeleteNodeLabelKey: constants.TrueStr}
		Expect(k8sClient.Patch(ctx, node, client.MergeFrom(unmodified))).To(Succeed())
		Eventually(func(g Gomega) bool {
			node := &v1.Node{}
			g.Expect(k8sClient.Get(ctx, nodeName, node)).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeTrue())
		Eventually(func(g Gomega) []v1.Pod {
			pods := &v1.PodList{}
			g.Expect(k8sClient.List(ctx, pods)).To(Succeed())
			return pods.Items
		}, 10*time.Second).Should(BeEmpty())
		// don't check for VM deletion here, won't spin up an Openstack setup
	})

	It("loads the openstack config from a file", func(ctx SpecContext) {
		conf, err := common.LoadOSConfig(ctx, k8sClient, types.NamespacedName{})
		Expect(err).To(Succeed())
		Expect(conf.AuthURL).To(Equal("https://localhost/garbage/"))
		Expect(conf.Password).To(Equal("pw"))
		Expect(conf.Region).To(Equal("qa-de-1"))
		Expect(conf.Username).To(Equal("user"))
		Expect(conf.Domainname).To(Equal("kubernikus"))
		Expect(conf.ProjectID).To(Equal("id"))
	})

	It("loads the openstack config from a secret", func(ctx SpecContext) {
		var secret v1.Secret
		secret.Name = "cloudprovider"
		secret.Namespace = metav1.NamespaceDefault
		secret.Data = map[string][]byte{
			"cloudprovider.conf": []byte(`[Global]
auth-url = https://localhost/garbage/
username = user
password = pw
region = qa-de-1
domain-name = kubernikus
tenant-id = id
`),
		}
		Expect(k8sClient.Create(ctx, &secret)).To(Succeed())
		conf, err := common.LoadOSConfig(ctx, k8sClient, client.ObjectKeyFromObject(&secret))
		Expect(err).To(Succeed())
		Expect(conf.AuthURL).To(Equal("https://localhost/garbage/"))
		Expect(conf.Password).To(Equal("pw"))
		Expect(conf.Password).To(Equal("pw"))
		Expect(conf.Region).To(Equal("qa-de-1"))
		Expect(conf.Username).To(Equal("user"))
		Expect(conf.Domainname).To(Equal("kubernikus"))
		Expect(conf.ProjectID).To(Equal("id"))
	})
})

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

package kubernikus

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("The kubernikus controller", func() {
	var node *v1.Node
	var nodeName types.NamespacedName

	initNode := func(version string) {
		node = &v1.Node{}
		node.Name = nodeName.Name
		node.Status.NodeInfo.KubeletVersion = version
		Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
	}

	makePod := func(podName, nodeName string, custom ...func(*v1.Pod)) error {
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
		return k8sClient.Create(context.Background(), &pod)
	}

	BeforeEach(func() {
		nodeName = types.NamespacedName{Namespace: "default", Name: "thenode"}
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
	})

	It("marks an outdated node for update", func() {
		initNode("v1.1.0")
		Eventually(func() string {
			result := &v1.Node{}
			Expect(k8sClient.Get(context.Background(), nodeName, result)).To(Succeed())
			return result.Labels[constants.KubeletUpdateLabelKey]
		}).Should(Equal(constants.TrueStr))
	})

	It("marks an up-to-date node as not needing an update", func() {
		initNode("v1.19.2")
		Eventually(func() string {
			result := &v1.Node{}
			Expect(k8sClient.Get(context.Background(), nodeName, result)).To(Succeed())
			return result.Labels[constants.KubeletUpdateLabelKey]
		}).Should(Equal("false"))
	})

	It("marks a node needing a downgrade", func() {
		initNode("v1.20.2")
		Eventually(func() string {
			result := &v1.Node{}
			Expect(k8sClient.Get(context.Background(), nodeName, result)).To(Succeed())
			return result.Labels[constants.KubeletUpdateLabelKey]
		}).Should(Equal(constants.TrueStr))
	})

	It("deletes nodes marked for deletion", func() {
		initNode("v1.19.2")
		Expect(makePod("thepod", nodeName.Name)).To(Succeed())
		unmodified := node.DeepCopy()
		node.Labels = map[string]string{constants.DeleteNodeLabelKey: constants.TrueStr}
		Expect(k8sClient.Patch(context.Background(), node, client.MergeFrom(unmodified))).To(Succeed())
		Eventually(func() bool {
			node := &v1.Node{}
			Expect(k8sClient.Get(context.Background(), nodeName, node)).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeTrue())
		Eventually(func() []v1.Pod {
			pods := &v1.PodList{}
			Expect(k8sClient.List(context.Background(), pods)).To(Succeed())
			return pods.Items
		}, 10*time.Second).Should(HaveLen(0))
		// don't check for VM deletion here, won't spin up an Openstack setup
	})

	It("loads the openstack config", func() {
		// still need to initialize a node, so the cleanup works
		initNode("v1.19.2")
		conf, err := loadOpenStackConfig()
		Expect(err).To(Succeed())
		Expect(conf.AuthURL).To(Equal("https://identity-3.qa-de-1.cloud.sap/v3/"))
		Expect(conf.Password).To(Equal("pw"))
		Expect(conf.Region).To(Equal("qa-de-1"))
		Expect(conf.Username).To(Equal("user"))
		Expect(conf.Domainname).To(Equal("kubernikus"))
		Expect(conf.ProjectID).To(Equal("id"))
	})
})

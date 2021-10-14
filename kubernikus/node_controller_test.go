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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
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
})

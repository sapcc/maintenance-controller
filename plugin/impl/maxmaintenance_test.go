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

package impl

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("The MaxMaintenance plugin", func() {

	groupLabel := "group"

	makeParams := func(node *corev1.Node) plugin.Parameters {
		return plugin.Parameters{
			Log:  GinkgoLogr,
			Node: node,
		}
	}

	It("can parse its configuration", func() {
		configStr := "max: 296\nprofile: kappa\nskipAfter: 20s\ngroupLabel: " + groupLabel
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base MaxMaintenance
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		maxMaintenance := plugin.(*MaxMaintenance)
		Expect(maxMaintenance.MaxNodes).To(Equal(296))
		Expect(maxMaintenance.Profile).To(Equal("kappa"))
		Expect(maxMaintenance.SkipAfter).To(Equal(20 * time.Second))
		Expect(maxMaintenance.GroupLabel).To(Equal(groupLabel))
	})

	It("passes if the returned nodes are less the max value", func() {
		plugin := MaxMaintenance{MaxNodes: 2}
		result, err := plugin.checkInternal(makeParams(nil), []corev1.Node{{}})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

	It("fails if the returned nodes equal the max value", func() {
		plugin := MaxMaintenance{MaxNodes: 2}
		result, err := plugin.checkInternal(makeParams(nil), []corev1.Node{{}, {}})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

	It("filters out not matching profiles", func() {
		nodes := []corev1.Node{
			{
				ObjectMeta: v1.ObjectMeta{
					Labels: map[string]string{constants.ProfileLabelKey: "profile"},
				},
			},
			{},
		}
		plugin := MaxMaintenance{MaxNodes: 1, Profile: "profile"}
		result, err := plugin.checkInternal(makeParams(nil), nodes)
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
	})

	Context("with skipAfter", func() {
		var nodes []corev1.Node

		BeforeEach(func() {
			data := state.DataV2{
				Profiles: map[string]*state.ProfileData{
					"profile": {
						Transition: time.Now().Add(-20 * time.Second),
					},
				},
			}
			json, err := json.Marshal(&data)
			Expect(err).To(Succeed())
			nodes = []corev1.Node{
				{
					ObjectMeta: v1.ObjectMeta{
						Labels:      map[string]string{constants.ProfileLabelKey: "profile"},
						Annotations: map[string]string{constants.DataAnnotationKey: string(json)},
					},
				},
			}
		})

		It("skips counting nodes that exceed the duration for all profiles", func() {
			plugin := MaxMaintenance{MaxNodes: 1, SkipAfter: 5 * time.Second}
			result, err := plugin.checkInternal(makeParams(nil), nodes)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("does not skip counting nodes that satisfy the duration for all profiles", func() {
			plugin := MaxMaintenance{MaxNodes: 1, SkipAfter: 50 * time.Second}
			result, err := plugin.checkInternal(makeParams(nil), nodes)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("skips counting nodes that exceed the duration for a specific profile", func() {
			plugin := MaxMaintenance{MaxNodes: 1, SkipAfter: 5 * time.Second, Profile: "profile"}
			result, err := plugin.checkInternal(makeParams(nil), nodes)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("does not skip counting nodes that satisfy the duration for a specific profile", func() {
			plugin := MaxMaintenance{MaxNodes: 1, SkipAfter: 50 * time.Second, Profile: "profile"}
			result, err := plugin.checkInternal(makeParams(nil), nodes)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

	})

	Context("with groupLabel", func() {

		makeNodes := func(group1, group2 string) []corev1.Node {
			var node1 corev1.Node
			node1.Labels = map[string]string{groupLabel: group1}
			var node2 corev1.Node
			node2.Labels = map[string]string{groupLabel: group2}
			return []corev1.Node{node1, node2}
		}

		It("blocks when nodes within the same group are in-maintenance", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupLabel: groupLabel}
			nodes := makeNodes("a", "a")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), nodes[1:])
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("passes when nodes within the same group are out of maintenance", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupLabel: groupLabel}
			nodes := makeNodes("a", "a")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), []corev1.Node{{}})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("passes when nodes with the other groups are in-maintenance", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupLabel: groupLabel}
			nodes := makeNodes("b", "c")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), nodes[1:])
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("matches against the whole cluster when the label value is empty", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupLabel: groupLabel}
			nodes := makeNodes("", "d")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), nodes[1:])
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

	})

})

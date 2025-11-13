// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
)

var _ = Describe("The MaxMaintenance plugin", func() {
	groupLabel := "group"

	makeParams := func(node *corev1.Node) plugin.Parameters {
		return plugin.Parameters{
			Log:  GinkgoLogr,
			Node: node,
		}
	}

	It("can parse a current configuration", func() {
		configStr := "max: 481\ngroupBy: [\"firstLabel\", \"secondLabel\"]"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base MaxMaintenance
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		maxMaintenance, ok := plugin.(*MaxMaintenance)
		Expect(ok).To(BeTrue())
		Expect(maxMaintenance.MaxNodes).To(Equal(481))
		Expect(maxMaintenance.GroupBy).To(HaveExactElements("firstLabel", "secondLabel"))
	})

	It("can parse a legacy configuration", func() {
		configStr := "max: 296\nprofile: kappa\nskipAfter: 20s\ngroupLabel: " + groupLabel
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base MaxMaintenance
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		maxMaintenance, ok := plugin.(*MaxMaintenance)
		Expect(ok).To(BeTrue())
		Expect(maxMaintenance.MaxNodes).To(Equal(296))
		Expect(maxMaintenance.Profile).To(Equal("kappa"))
		Expect(maxMaintenance.SkipAfter).To(Equal(20 * time.Second))
		Expect(maxMaintenance.GroupBy).To(HaveExactElements(groupLabel))
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
			data := state.Data{
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

	Context("with groupBy", func() {

		makeNodes := func(group1, group2 string) []corev1.Node {
			var node1 corev1.Node
			node1.Labels = map[string]string{groupLabel: group1}
			var node2 corev1.Node
			node2.Labels = map[string]string{groupLabel: group2}
			return []corev1.Node{node1, node2}
		}

		It("blocks when nodes within the same group are in-maintenance", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupBy: []string{groupLabel}}
			nodes := makeNodes("a", "a")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), nodes[1:])
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("passes when nodes within the same group are out of maintenance", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupBy: []string{groupLabel}}
			nodes := makeNodes("a", "a")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), []corev1.Node{{}})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("passes when nodes with the other groups are in-maintenance", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupBy: []string{groupLabel}}
			nodes := makeNodes("b", "c")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), nodes[1:])
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("matches against the whole cluster when the label value is empty", func() {
			plugin := MaxMaintenance{MaxNodes: 1, GroupBy: []string{groupLabel}}
			nodes := makeNodes("", "d")
			result, err := plugin.checkInternal(makeParams(&nodes[0]), nodes[1:])
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})
	})
})

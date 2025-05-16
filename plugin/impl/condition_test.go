// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The Condition plugin", func() {
	It("can parse its config", func() {
		configStr := "type: Ready\nstatus: \"True\""
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base Condition
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&Condition{
			Type:   "Ready",
			Status: "True",
		}))
	})

	Context("with node Ready=True", func() {
		params := plugin.Parameters{
			Node: &corev1.Node{
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			Client: nil,
			Log:    GinkgoLogr,
		}

		It("matches when configured Ready=True", func() {
			plugin := Condition{Type: "Ready", Status: "True"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("does not match when configured Ready=Unknown", func() {
			plugin := Condition{Type: "Ready", Status: "Unknown"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("does not match when configured DiskPressure=True", func() {
			plugin := Condition{Type: "DiskPressure", Status: "True"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})
	})
})

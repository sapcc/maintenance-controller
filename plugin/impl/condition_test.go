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
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("The Condition plugin", func() {

	It("can parse its config", func() {
		configStr := "type: Ready\nstatus: \"True\""
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base Condition
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*Condition).Type).To(Equal("Ready"))
		Expect(plugin.(*Condition).Status).To(Equal("True"))
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
			Log:    logr.Discard(),
		}

		It("matches when configured Ready=True", func() {
			plugin := Condition{Type: "Ready", Status: "True"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
		})

		It("does not match when configured Ready=Unknown", func() {
			plugin := Condition{Type: "Ready", Status: "Unknown"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeFalse())
		})

		It("does not match when configured DiskPressure=True", func() {
			plugin := Condition{Type: "DiskPressure", Status: "True"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeFalse())
		})

	})
})

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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("The HasLabel plugin", func() {

	It("can parse its config", func() {
		configStr := "key: key\nvalue: value"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base HasLabel
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*HasLabel).Key).To(Equal("key"))
		Expect(plugin.(*HasLabel).Value).To(Equal("value"))
	})

	Context("with label key=value", func() {

		params := plugin.Parameters{
			Node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Labels: map[string]string{"key": "value"},
				},
			},
			Client: nil,
			Log:    GinkgoLogr,
		}

		It("matches the label with empty value", func() {
			plugin := HasLabel{Key: "key", Value: ""}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("matches the label with correct value", func() {
			plugin := HasLabel{Key: "key", Value: "value"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("does not match the label with wrong value", func() {
			plugin := HasLabel{Key: "key", Value: "assdas"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("does not match the label with wrong key", func() {
			plugin := HasLabel{Key: "sdasdasda", Value: ""}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

	})

})

var _ = Describe("The AnyLabel plugin", func() {

	It("can parse its config", func() {
		configStr := "key: key\nvalue: test"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base AnyLabel
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*AnyLabel).Key).To(Equal("key"))
		Expect(plugin.(*AnyLabel).Value).To(Equal("test"))
	})

})

var _ = Describe("The AlterLabel plugin", func() {

	It("can parse its config", func() {
		configStr := "key: key\nvalue: value\nremove: true"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base AlterLabel
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*AlterLabel).Key).To(Equal("key"))
		Expect(plugin.(*AlterLabel).Value).To(Equal("value"))
		Expect(plugin.(*AlterLabel).Remove).To(BeTrue())
	})

	Context("with label key=value", func() {

		var params plugin.Parameters

		BeforeEach(func() {
			params = plugin.Parameters{
				Node: &corev1.Node{
					ObjectMeta: v1.ObjectMeta{
						Labels: map[string]string{"key": "value"},
					},
				},
				Client: nil,
				Log:    GinkgoLogr,
			}
		})

		It("removes the label if remove is true", func() {
			plugin := AlterLabel{Key: "key", Remove: true, Value: "value"}
			err := plugin.Trigger(params)
			Expect(err).To(Succeed())
			Expect(params.Node.Labels).To(HaveLen(0))
		})

		It("adds a new label if key is not 'key' and remove is false", func() {
			plugin := AlterLabel{Key: "key2", Remove: false, Value: "value"}
			err := plugin.Trigger(params)
			Expect(err).To(Succeed())
			Expect(params.Node.Labels).To(HaveLen(2))
			Expect(params.Node.Labels["key2"]).To(Equal("value"))
		})

		It("updates the label value if key is 'key' and remove is false", func() {
			plugin := AlterLabel{Key: "key", Remove: false, Value: "abc"}
			err := plugin.Trigger(params)
			Expect(err).To(Succeed())
			Expect(params.Node.Labels).To(HaveLen(1))
			Expect(params.Node.Labels["key"]).To(Equal("abc"))
		})

	})

})

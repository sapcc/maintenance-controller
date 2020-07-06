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
	"github.com/elastic/go-ucfg/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("The HasAnnotation plugin", func() {

	It("can parse its config", func() {
		configStr := "key: key\nvalue: value"
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())

		var base HasAnnotation
		plugin, err := base.New(config)
		Expect(err).To(Succeed())
		Expect(plugin.(*HasAnnotation).Key).To(Equal("key"))
		Expect(plugin.(*HasAnnotation).Value).To(Equal("value"))
	})

	Context("with annotation key=value", func() {

		params := plugin.Parameters{
			Node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
				},
			},
			Client: nil,
			Log:    nil,
		}

		It("matches the annotation with empty value", func() {
			plugin := HasAnnotation{Key: "key", Value: ""}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
		})

		It("matches the annotation with correct value", func() {
			plugin := HasAnnotation{Key: "key", Value: "value"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
		})

		It("does not match the annotation with wrong value", func() {
			plugin := HasAnnotation{Key: "key", Value: "assdas"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeFalse())
		})

		It("does not match the annotation with wrong key", func() {
			plugin := HasAnnotation{Key: "sdasdasda", Value: ""}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result).To(BeFalse())
		})

	})

})

var _ = Describe("The AlterAnnotation plugin", func() {

	It("can parse its config", func() {
		configStr := "key: key\nvalue: value\nremove: true"
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())

		var base AlterAnnotation
		plugin, err := base.New(config)
		Expect(err).To(Succeed())
		Expect(plugin.(*AlterAnnotation).Key).To(Equal("key"))
		Expect(plugin.(*AlterAnnotation).Value).To(Equal("value"))
		Expect(plugin.(*AlterAnnotation).Remove).To(BeTrue())
	})

	Context("with annotation key=value", func() {

		var params plugin.Parameters

		BeforeEach(func() {
			params = plugin.Parameters{
				Node: &corev1.Node{
					ObjectMeta: v1.ObjectMeta{
						Annotations: map[string]string{"key": "value"},
					},
				},
				Client: nil,
				Log:    nil,
			}
		})

		It("removes the annotation if remove is true", func() {
			plugin := AlterAnnotation{Key: "key", Remove: true, Value: "value"}
			err := plugin.Trigger(params)
			Expect(err).To(Succeed())
			Expect(params.Node.Annotations).To(HaveLen(0))
		})

		It("adds a new annotation if key is not 'key' and remove is false", func() {
			plugin := AlterAnnotation{Key: "key2", Remove: false, Value: "value"}
			err := plugin.Trigger(params)
			Expect(err).To(Succeed())
			Expect(params.Node.Annotations).To(HaveLen(2))
			Expect(params.Node.Annotations["key2"]).To(Equal("value"))
		})

		It("updates the annotation value if key is 'key' and remove is false", func() {
			plugin := AlterAnnotation{Key: "key", Remove: false, Value: "abc"}
			err := plugin.Trigger(params)
			Expect(err).To(Succeed())
			Expect(params.Node.Annotations).To(HaveLen(1))
			Expect(params.Node.Annotations["key"]).To(Equal("abc"))
		})

	})

})

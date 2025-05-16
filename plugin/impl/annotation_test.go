// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The HasAnnotation plugin", func() {
	It("can parse its config", func() {
		configStr := "key: key\nvalue: value"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base HasAnnotation
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&HasAnnotation{
			Key:   "key",
			Value: "value",
		}))
	})

	Context("with annotation key=value", func() {
		params := plugin.Parameters{
			Node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{"key": "value"},
				},
			},
			Client: nil,
			Log:    GinkgoLogr,
		}

		It("matches the annotation with empty value", func() {
			plugin := HasAnnotation{Key: "key", Value: ""}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("matches the annotation with correct value", func() {
			plugin := HasAnnotation{Key: "key", Value: "value"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("does not match the annotation with wrong value", func() {
			plugin := HasAnnotation{Key: "key", Value: "assdas"}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("does not match the annotation with wrong key", func() {
			plugin := HasAnnotation{Key: "sdasdasda", Value: ""}
			result, err := plugin.Check(params)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

	})

})

var _ = Describe("The AlterAnnotation plugin", func() {
	It("can parse its config", func() {
		configStr := "key: key\nvalue: value\nremove: true"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base AlterAnnotation
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&AlterAnnotation{
			Key:    "key",
			Value:  "value",
			Remove: true,
		}))
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
				Log:    GinkgoLogr,
			}
		})

		It("removes the annotation if remove is true", func() {
			plugin := AlterAnnotation{Key: "key", Remove: true, Value: "value"}
			err := plugin.Trigger(params)
			Expect(err).To(Succeed())
			Expect(params.Node.Annotations).To(BeEmpty())
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

// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The Finalizer plugin", func() {
	Describe("AlterFinalizer", func() {
		It("fails parsing incorrect config", func() {
			_, err := ucfgwrap.FromYAML([]byte("invalid_yaml"))
			errMsg := "type 'string' is not supported on top level of config, only dictionary or list"
			Expect(err).To(MatchError(errMsg))

			config, err := ucfgwrap.FromYAML([]byte("value: test"))
			Expect(err).To(Not(HaveOccurred()))

			var base AlterFinalizer
			_, err = base.New(&config)
			Expect(err).To(MatchError("string value is not set accessing 'key'"))
		})

		It("has valid configuration", func() {
			config, err := ucfgwrap.FromYAML([]byte("key: test.com/finalizer\nremove: true"))
			Expect(err).To(Not(HaveOccurred()))

			var base AlterFinalizer
			plugin, err := base.New(&config)
			Expect(err).To(Succeed())
			Expect(plugin).To(Equal(&AlterFinalizer{
				Key:    "test.com/finalizer",
				Remove: true,
			}))
		})

		It("adds finalizer when not present", func() {
			config, err := ucfgwrap.FromYAML([]byte("key: test.com/finalizer\nremove: false"))
			Expect(err).To(Not(HaveOccurred()))

			var base AlterFinalizer
			addFinalizer, err := base.New(&config)
			Expect(err).To(Not(HaveOccurred()))

			testNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
			err = addFinalizer.Trigger(plugin.Parameters{Node: testNode})
			Expect(err).To(Not(HaveOccurred()))
			Expect(testNode.Finalizers).To(ContainElement("test.com/finalizer"))
		})

		It("removes finalizer when present", func() {
			testNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{
				Name:       "test-node",
				Finalizers: []string{"test.com/finalizer"},
			}}

			config, err := ucfgwrap.FromYAML([]byte("key: test.com/finalizer\nremove: true"))
			Expect(err).To(Succeed())

			var base AlterFinalizer
			removeFinalizer, err := base.New(&config)
			Expect(err).To(Not(HaveOccurred()))

			err = removeFinalizer.Trigger(plugin.Parameters{Node: testNode})
			Expect(err).To(Not(HaveOccurred()))
			Expect(testNode.Finalizers).ToNot(ContainElement("test.com/finalizer"))
		})
	})
})

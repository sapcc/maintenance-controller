// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
)

var _ = Describe("The hypervisor plugin", func() {
	Describe("CheckHypervisor", func() {
		It("parses with correct config", func() {
			config, err := ucfgwrap.FromYAML([]byte("evicted: true"))
			Expect(err).To(Succeed())

			var base CheckHypervisor
			plugin, err := base.New(&config)

			Expect(err).To(Succeed())
			Expect(plugin).To(Equal(&CheckHypervisor{
				Fields: &map[string]any{"Evicted": true},
			}))
		})

		It("fails parsing incorrect config", func() {
			config, err := ucfgwrap.FromYAML([]byte("value: test"))
			Expect(err).To(Succeed())

			var base CheckHypervisor
			_, err = base.New(&config)
			Expect(err).To(MatchError("field value not found in Hypervisor spec"))
		})
	})

	Describe("AlterHypervisor", func() {
		It("fails parsing incorrect config", func() {
			config, err := ucfgwrap.FromYAML([]byte("value: test"))
			Expect(err).To(Succeed())

			var base AlterHypervisor
			_, err = base.New(&config)
			Expect(err).To(MatchError("field value not found in Hypervisor spec"))
		})

		It("has valid configuration", func() {
			config, err := ucfgwrap.FromYAML([]byte("maintenance: true"))
			Expect(err).To(Succeed())

			var base AlterHypervisor
			plugin, err := base.New(&config)
			Expect(err).To(Succeed())
			Expect(plugin).To(Equal(&AlterHypervisor{
				Fields: &map[string]any{"Maintenance": true},
			}))
		})
	})
})

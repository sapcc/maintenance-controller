// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The Affinity plugin", func() {

	It("can parse its configuration", func() {
		configStr := "minOperational: 5"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base Affinity
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&Affinity{MinOperational: 5}))
	})

	It("can initialize from null config", func() {
		configStr := "null"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base Affinity
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&Affinity{MinOperational: 0}))
	})

	It("can initialize when config is nil", func() {
		var base Affinity
		plugin, err := base.New(nil)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&Affinity{MinOperational: 0}))
	})

	It("loads correctly into registry when config is nil", func() {
		configStr := "check:\n- type: affinity\n  name: check_affinity\n  config: null"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var instance plugin.InstancesDescriptor
		Expect(config.Unpack(&instance)).To(Succeed())
		registry := plugin.NewRegistry()
		addChecker := func(checker plugin.Checker) {
			registry.CheckPlugins[checker.ID()] = checker
		}
		addChecker(&Affinity{})
		Expect(registry.LoadInstances(&config, &instance)).To(Succeed())
	})

})

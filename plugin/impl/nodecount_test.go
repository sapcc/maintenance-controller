// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The nodecount plugin", func() {
	It("can parse its configuration", func() {
		configStr := "count: 154"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base NodeCount
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&NodeCount{Count: 154}))
	})

	It("does not fail in AfterEval", func() {
		var count NodeCount
		Expect(count.OnTransition(plugin.Parameters{})).To(Succeed())
	})
})

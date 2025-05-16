// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
)

var _ = Describe("The Stagger plugin", func() {
	It("can parse its configuration", func() {
		configStr := "duration: 1m\nleaseName: mc-lease\nleaseNamespace: default"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base Stagger
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&Stagger{
			Duration:       time.Minute,
			LeaseName:      "mc-lease",
			LeaseNamespace: "default",
			Parallel:       1,
		}))
	})
})

// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
)

var _ = Describe("The ClusterSemver plugin", func() {
	It("can parse its configuration", func() {
		configStr := "key: alge\nprofileScoped: yes"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base ClusterSemver
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&ClusterSemver{
			Key:           "alge",
			ProfileScoped: true,
		}))
	})
})

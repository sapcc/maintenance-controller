// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("The KubernikusCount plugin", func() {
	It("can parse its configuration", func() {
		configStr := "cluster: aCluster"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base KubernikusCount
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&KubernikusCount{
			Cluster: "aCluster",
		}))
	})

	It("can parse its configuration with a cloudprovider secret", func() {
		configStr := `cluster: aCluster
cloudProviderSecret:
  name: aSecret
  namespace: aNamespace`
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base KubernikusCount
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&KubernikusCount{
			Cluster: "aCluster",
			CloudProviderSecret: client.ObjectKey{
				Name:      "aSecret",
				Namespace: "aNamespace",
			},
		}))
	})
})

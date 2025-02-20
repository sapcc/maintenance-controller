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

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
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("The MaxMaintenance plugin", func() {

	It("can parse its configuration", func() {
		configStr := "max: 296"
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())
		var base MaxMaintenance
		plugin, err := base.New(config)
		Expect(err).To(Succeed())
		Expect(plugin.(*MaxMaintenance).MaxNodes).To(Equal(296))
	})

	It("passes if the returned nodes are less the max value", func() {
		nodes := corev1.NodeList{
			Items: []corev1.Node{{}},
		}
		plugin := MaxMaintenance{MaxNodes: 2}
		result, err := plugin.checkInternal(&nodes)
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

	It("fails if the returned nodes equal the max value", func() {
		nodes := corev1.NodeList{
			Items: []corev1.Node{{}, {}},
		}
		plugin := MaxMaintenance{MaxNodes: 2}
		result, err := plugin.checkInternal(&nodes)
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

})

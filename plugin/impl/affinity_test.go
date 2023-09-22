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
)

var _ = Describe("The Affinity plugin", func() {

	It("can parse its configuration", func() {
		configStr := "minOperational: 5"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base Affinity
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*Affinity).MinOperational).To(Equal(5))
	})

	It("can initialize from null config", func() {
		configStr := "null"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base Affinity
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*Affinity).MinOperational).To(Equal(0))
	})

})

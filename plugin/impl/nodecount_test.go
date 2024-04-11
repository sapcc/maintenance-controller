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
		Expect(plugin.(*NodeCount).Count).To(Equal(154))
	})

	It("does not fail in AfterEval", func() {
		var count NodeCount
		Expect(count.OnTransition(plugin.Parameters{})).To(Succeed())
	})

})

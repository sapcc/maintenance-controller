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
	"time"

	"github.com/elastic/go-ucfg/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The wait plugin", func() {

	It("can parse its config", func() {
		base := Wait{}
		configStr := "duration: 0h12m"
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())
		plugin, err := base.New(config)
		Expect(err).To(Succeed())
		Expect(plugin.(*Wait).Duration.Minutes()).To(Equal(12.0))
	})

	It("passes if the defined time has passed", func() {
		wait := Wait{Duration: 10 * time.Minute}
		result, err := wait.Check(plugin.Parameters{
			LastTransition: time.Now().Add(-12 * time.Minute),
		})
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

	It("fails if the time has not passed", func() {
		wait := Wait{Duration: 15 * time.Minute}
		result, err := wait.Check(plugin.Parameters{
			LastTransition: time.Now().Add(-12 * time.Minute),
		})
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

})

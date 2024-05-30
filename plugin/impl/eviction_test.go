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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
)

var _ = Describe("The eviction plugin", func() {

	It("has default timeout values", func() {
		config, err := ucfgwrap.FromYAML([]byte("action: drain"))
		Expect(err).To(Succeed())

		var base Eviction
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*Eviction).DeletionTimeout).To(Equal(10 * time.Minute))
		Expect(plugin.(*Eviction).EvictionTimeout).To(Equal(10 * time.Minute))
	})

	It("can parse it's configuration", func() {
		configStr := "action: drain\ndeletionTimeout: 11m\nevictionTimeout: 532ms"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())

		var base Eviction
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*Eviction).Action).To(Equal(Drain))
		Expect(plugin.(*Eviction).DeletionTimeout).To(Equal(11 * time.Minute))
		Expect(plugin.(*Eviction).EvictionTimeout).To(Equal(532 * time.Millisecond))
	})

})

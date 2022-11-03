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

package common

import (
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("ucfg.Config wrapper", func() {

	It("expands env vars", func() {
		os.Setenv("MAINTENANCE_CONTROLLER_TEST", "stone")
		defer os.Unsetenv("MAINTENANCE_CONTROLLER_TEST")
		yaml := "key: ${MAINTENANCE_CONTROLLER_TEST}"
		config, err := NewConfigFromYAML([]byte(yaml))
		Expect(err).To(Succeed())
		data := struct {
			Key string
		}{Key: ""}
		err = config.Unpack(&data)
		Expect(err).To(Succeed())
		Expect(data.Key).To(Equal("stone"))
	})

})

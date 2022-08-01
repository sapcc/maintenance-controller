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

package esx

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config", func() {

	It("alarms as set should contain all alarms", func() {
		config := Config{
			Alarms: []string{"bread", "butter"},
		}
		Expect(config.AlarmsAsSet()).To(HaveKey("bread"))
		Expect(config.AlarmsAsSet()).To(HaveKey("butter"))
	})

})

var _ = Describe("VCenters", func() {

	It("should create valid vCenter URLs", func() {
		vCenters := VCenters{
			Template: "https://region-$AZ.local",
			Credentials: map[string]Credential{
				"a": {
					Username: "user1",
					Password: "pw1",
				},
				"b": {
					Username: "user2",
					Password: "pw2",
				},
			},
		}
		urlA, err := vCenters.URL("a")
		Expect(err).To(Succeed())
		Expect(urlA.String()).To(Equal("https://user1:pw1@region-a.local/sdk"))
		urlB, err := vCenters.URL("b")
		Expect(err).To(Succeed())
		Expect(urlB.String()).To(Equal("https://user2:pw2@region-b.local/sdk"))
		_, err = vCenters.URL("c")
		Expect(err).To(HaveOccurred())
	})

})

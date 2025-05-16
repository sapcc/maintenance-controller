// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package esx

import (
	. "github.com/onsi/ginkgo/v2"
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

// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCommon(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Common Suite")
}

var _ = Describe("WeekdayFromString", func() {

	It("should parse monday", func() {
		weekday, err := WeekdayFromString("monday")
		Expect(err).To(Succeed())
		Expect(weekday).To(Equal(time.Monday))
	})

	It("should parse wed", func() {
		weekday, err := WeekdayFromString("wed")
		Expect(err).To(Succeed())
		Expect(weekday).To(Equal(time.Wednesday))
	})

	It("should not parse abcde", func() {
		_, err := WeekdayFromString("abcde")
		Expect(err).ToNot(Succeed())
	})

})

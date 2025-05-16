// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("filterNodeLabels", func() {

	It("keeps the key-value pairs specified by the keys parameter", func() {
		labels := map[string]string{"a": "b", "c": "d", "e": "f"}
		result := filterNodeLabels(labels, []string{"a", "e"})
		Expect(result).To(HaveLen(2))
		Expect(result).To(HaveKeyWithValue("a", "b"))
		Expect(result).To(HaveKeyWithValue("e", "f"))
	})

	It("filters key-value pairs not specified by the keys parameter", func() {
		labels := map[string]string{"x": "y"}
		result := filterNodeLabels(labels, nil)
		Expect(result).To(BeEmpty())
	})

})

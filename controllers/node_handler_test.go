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
		Expect(result).To(HaveLen(0))
	})

})

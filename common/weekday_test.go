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
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCommon(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Suite")
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

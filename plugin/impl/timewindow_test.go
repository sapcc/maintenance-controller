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
)

var _ = Describe("The Timewindow plugin", func() {

	It("can parse its config", func() {
		configStr := "weekdays: [mon]\nstart: \"11:00\"\nend: \"19:30\""
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())
		var base TimeWindow
		plugin, err := base.New(config)
		Expect(err).To(Succeed())
		start := plugin.(*TimeWindow).Start
		end := plugin.(*TimeWindow).End
		weekdays := plugin.(*TimeWindow).Weekdays
		Expect(start.Hour()).To(Equal(11))
		Expect(start.Minute()).To(Equal(0))
		Expect(end.Hour()).To(Equal(19))
		Expect(end.Minute()).To(Equal(30))
		Expect(weekdays).To(HaveLen(1))
		Expect(weekdays[0]).To(Equal(time.Monday))
	})

	It("should fail creation if no weekdays are provided", func() {
		configStr := "start: \"11:00\"\nend: \"19:30\""
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())
		var base TimeWindow
		_, err = base.New(config)
		Expect(err).To(HaveOccurred())
	})

	It("should fail creation if no start is after end", func() {
		configStr := "weekdays: [mon]\nstart: \"18:00\"\nend: \"17:30\""
		config, err := yaml.NewConfig([]byte(configStr))
		Expect(err).To(Succeed())
		var base TimeWindow
		_, err = base.New(config)
		Expect(err).To(HaveOccurred())
	})

	Context("with every monday and tuesday between 10:30 and 15:20", func() {

		start, _ := time.Parse(timeFormat, "10:30")
		end, _ := time.Parse(timeFormat, "15:20")

		plugin := TimeWindow{
			Start:    start,
			End:      end,
			Weekdays: []time.Weekday{time.Monday, time.Tuesday},
		}

		It("passes at 11:00 on monday", func() {
			targetDate := time.Date(2020, time.June, 29, 11, 0, 0, 0, time.UTC)
			result := plugin.checkInternal(targetDate)
			Expect(result).To(BeTrue())
		})

		It("passes at 15:00 on tuesday", func() {
			targetDate := time.Date(2020, time.June, 30, 15, 0, 0, 0, time.UTC)
			result := plugin.checkInternal(targetDate)
			Expect(result).To(BeTrue())
		})

		It("fails at 15:30 on tuesday", func() {
			targetDate := time.Date(2020, time.June, 30, 15, 30, 0, 0, time.UTC)
			result := plugin.checkInternal(targetDate)
			Expect(result).To(BeFalse())
		})

		It("fails at 10:29 on monday", func() {
			targetDate := time.Date(2020, time.June, 29, 10, 29, 0, 0, time.UTC)
			result := plugin.checkInternal(targetDate)
			Expect(result).To(BeFalse())
		})

		It("fails at 11:00 on thursday", func() {
			targetDate := time.Date(2020, time.June, 25, 11, 0, 0, 0, time.UTC)
			result := plugin.checkInternal(targetDate)
			Expect(result).To(BeFalse())
		})

		It("fails at 15:00 on thursday", func() {
			targetDate := time.Date(2020, time.June, 25, 15, 0, 0, 0, time.UTC)
			result := plugin.checkInternal(targetDate)
			Expect(result).To(BeFalse())
		})

	})

})

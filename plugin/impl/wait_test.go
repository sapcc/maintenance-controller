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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
)

var _ = Describe("The wait plugin", func() {

	It("can parse its config", func() {
		base := Wait{}
		configStr := "duration: 0h12m"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*Wait).Duration.Minutes()).To(Equal(12.0))
	})

	It("passes if the defined time has passed", func() {
		wait := Wait{Duration: 10 * time.Minute}
		result, err := wait.Check(plugin.Parameters{
			LastTransition: time.Now().UTC().Add(-12 * time.Minute),
		})
		Expect(err).To(Succeed())
		Expect(result).To(BeTrue())
	})

	It("fails if the time has not passed", func() {
		wait := Wait{Duration: 15 * time.Minute}
		result, err := wait.Check(plugin.Parameters{
			LastTransition: time.Now().UTC().Add(-12 * time.Minute),
		})
		Expect(err).To(Succeed())
		Expect(result).To(BeFalse())
	})

})

var _ = Describe("The waitExclude plugin", func() {

	It("can parse its config", func() {
		base := WaitExclude{}
		configStr := "duration: 17m\nexclude: [\"tue\"]"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*WaitExclude).Duration).To(Equal(17 * time.Minute))
		Expect(plugin.(*WaitExclude).Exclude).To(ContainElement(time.Tuesday))
	})

	checkWaitExclude := func(we *WaitExclude, transition, now time.Time) bool {
		return we.checkInternal(&plugin.Parameters{LastTransition: transition}, now)
	}

	Context("with a duration of one hour and no exclusions", func() {

		It("fails between 10:00 and 10:30", func() {
			we := WaitExclude{Duration: 1 * time.Hour, Exclude: make([]time.Weekday, 0)}
			lastTransition := time.Date(2022, time.March, 15, 10, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 15, 10, 30, 00, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeFalse())
		})

		It("passes between 10:00 and 11:30", func() {
			we := WaitExclude{Duration: 1 * time.Hour, Exclude: make([]time.Weekday, 0)}
			lastTransition := time.Date(2022, time.March, 15, 10, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 15, 11, 30, 00, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeTrue())
		})

	})

	Context("with a duration of 30 hours and exclusions on monday and wednesday", func() {

		It("fails between sun 12:00 and tue 17:00", func() {
			we := WaitExclude{Duration: 30 * time.Hour, Exclude: []time.Weekday{time.Monday, time.Wednesday}}
			lastTransition := time.Date(2022, time.March, 6, 12, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 8, 17, 00, 00, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeFalse())
		})

		It("passes between sun 12:00 and tue 18:10", func() {
			we := WaitExclude{Duration: 30 * time.Hour, Exclude: []time.Weekday{time.Monday, time.Wednesday}}
			lastTransition := time.Date(2022, time.March, 6, 12, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 8, 18, 10, 00, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeTrue())
		})

		It("fails between mon 12:00 and thu 5:00", func() {
			we := WaitExclude{Duration: 30 * time.Hour, Exclude: []time.Weekday{time.Monday, time.Wednesday}}
			lastTransition := time.Date(2022, time.March, 7, 12, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 10, 5, 00, 00, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeFalse())
		})

		It("passes between mon 12:00 and thu 6:10", func() {
			we := WaitExclude{Duration: 30 * time.Hour, Exclude: []time.Weekday{time.Monday, time.Wednesday}}
			lastTransition := time.Date(2022, time.March, 7, 12, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 10, 6, 10, 00, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeTrue())
		})

		It("fails between son 22:00 and mon 17:00", func() {
			we := WaitExclude{Duration: 30 * time.Hour, Exclude: []time.Weekday{time.Monday, time.Wednesday}}
			lastTransition := time.Date(2022, time.March, 5, 22, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 7, 17, 00, 00, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeFalse())
		})

	})

	Context("with a duration of 1 second and exclusions on monday and wednesday", func() {

		It("passes on sunday after a second", func() {
			we := WaitExclude{Duration: 1 * time.Second, Exclude: []time.Weekday{time.Monday, time.Wednesday}}
			lastTransition := time.Date(2022, time.March, 6, 12, 00, 00, 00, time.UTC)
			now := time.Date(2022, time.March, 6, 12, 00, 02, 00, time.UTC)
			result := checkWaitExclude(&we, lastTransition, now)
			Expect(result).To(BeTrue())
		})

	})

})

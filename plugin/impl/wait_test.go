// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The wait plugin", func() {
	It("can parse its config", func() {
		base := Wait{}
		configStr := "duration: 0h12m"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin).To(Equal(&Wait{Duration: 12 * time.Minute}))
	})

	It("passes if the defined time has passed", func() {
		wait := Wait{Duration: 10 * time.Minute}
		result, err := wait.Check(plugin.Parameters{
			LastTransition: time.Now().UTC().Add(-12 * time.Minute),
		})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeTrue())
	})

	It("fails if the time has not passed", func() {
		wait := Wait{Duration: 15 * time.Minute}
		result, err := wait.Check(plugin.Parameters{
			LastTransition: time.Now().UTC().Add(-12 * time.Minute),
		})
		Expect(err).To(Succeed())
		Expect(result.Passed).To(BeFalse())
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
		Expect(plugin).To(Equal(&WaitExclude{
			Duration: 17 * time.Minute,
			Exclude:  []time.Weekday{time.Tuesday},
		}))
	})

	checkWaitExclude := func(we *WaitExclude, transition, now time.Time) bool {
		return we.checkInternal(&plugin.Parameters{LastTransition: transition}, now).Passed
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

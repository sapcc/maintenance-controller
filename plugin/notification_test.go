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

package plugin

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
)

type successfulNotification struct {
	Invoked int
}

func (n *successfulNotification) Notify(params Parameters) error {
	n.Invoked++
	return nil
}

func (n *successfulNotification) New(config *ucfgwrap.Config) (Notifier, error) {
	return &successfulNotification{}, nil
}

func (n *successfulNotification) ID() string {
	return "success"
}

type failingNotification struct {
	Invoked int
}

func (n *failingNotification) Notify(params Parameters) error {
	n.Invoked++
	return errors.New("this notification is expected to fail")
}

func (n *failingNotification) New(config *ucfgwrap.Config) (Notifier, error) {
	return &failingNotification{}, nil
}

func (n *failingNotification) ID() string {
	return "fail"
}

var _ = Describe("NotificationChain", func() {

	emptyParams := Parameters{Log: GinkgoLogr}

	Context("is empty", func() {

		var chain NotificationChain
		It("should not error", func() {
			err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
		})

	})

	Context("contains plugins", func() {

		var (
			success NotificationInstance
			failing NotificationInstance
		)

		BeforeEach(func() {
			success = NotificationInstance{
				Plugin: &successfulNotification{},
				Name:   "success",
			}
			failing = NotificationInstance{
				Plugin: &failingNotification{},
				Name:   "failing",
			}
		})

		It("should run all plugins", func() {
			chain := NotificationChain{
				Plugins: []NotificationInstance{success, success},
			}
			err := chain.Execute(emptyParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(success.Plugin.(*successfulNotification).Invoked).To(Equal(2))
		})

		It("should propagate errors", func() {
			chain := NotificationChain{
				Plugins: []NotificationInstance{success, failing, success},
			}
			err := chain.Execute(emptyParams)
			Expect(err).To(HaveOccurred())
			Expect(success.Plugin.(*successfulNotification).Invoked).To(Equal(1))
			Expect(failing.Plugin.(*failingNotification).Invoked).To(Equal(1))
		})

	})

})

var _ = Describe("The notification", func() {

	It("should render its template", func() {
		result, err := RenderNotificationTemplate("{{.State}}", &Parameters{State: "def"})
		Expect(err).To(Succeed())
		Expect(result).To(Equal("def"))
	})

})

var _ = Describe("NotifyPeriodic", func() {

	It("can parse its configuration", func() {
		configStr := "interval: 5m"
		conf, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		np, err := newNotifyPeriodic(&conf)
		Expect(err).To(Succeed())
		Expect(np.Interval).To(Equal(5 * time.Minute))
	})

})

var _ = Describe("NotifyScheduled", func() {

	SchedLog := SchedulingLogger{
		Log:        GinkgoLogr,
		LogDetails: true,
	}

	makeSchedule := func() *NotifyScheduled {
		instant, err := time.Parse("15:04", "12:00")
		Expect(err).To(Succeed())
		return &NotifyScheduled{
			Instant:  instant,
			Weekdays: []time.Weekday{time.Monday},
		}
	}

	It("can parse its configuration", func() {
		configStr := "instant: \"15:23\"\nweekdays: [\"fri\", \"sat\"]\n"
		conf, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		ns, err := newNotifyScheduled(&conf)
		Expect(err).To(Succeed())
		Expect(ns.Instant.Hour()).To(Equal(15))
		Expect(ns.Instant.Minute()).To(Equal(23))
		Expect(ns.Weekdays).To(ContainElements(time.Friday, time.Saturday))
	})

	Context("on tuesdays", func() {

		It("should not trigger before 12:00", func() {
			currentDate := time.Date(2022, time.February, 22, 11, 0, 0, 0, time.UTC)
			result := makeSchedule().ShouldNotify(ShouldNotifyParams{
				Current: NotificationData{
					State: "operational",
					Time:  currentDate,
				},
				Last: NotificationData{
					State: "operational",
					Time:  currentDate.Add(-25 * time.Hour),
				},
				Log: SchedLog,
			})
			Expect(result).To(BeFalse())
		})

		It("should not trigger after 12:00", func() {
			currentDate := time.Date(2022, time.February, 22, 13, 0, 0, 0, time.UTC)
			result := makeSchedule().ShouldNotify(ShouldNotifyParams{
				Current: NotificationData{
					State: "operational",
					Time:  currentDate,
				},
				Last: NotificationData{
					State: "operational",
					Time:  currentDate.Add(-25 * time.Hour),
				},
				Log: SchedLog,
			})
			Expect(result).To(BeFalse())
		})

	})

	Context("on mondays", func() {

		It("should not trigger before 12:00", func() {
			currentDate := time.Date(2022, time.February, 21, 11, 0, 0, 0, time.UTC)
			result := makeSchedule().ShouldNotify(ShouldNotifyParams{
				Current: NotificationData{
					State: "operational",
					Time:  currentDate,
				},
				Last: NotificationData{
					State: "operational",
					Time:  currentDate.Add(-25 * time.Hour),
				},
				Log: SchedLog,
			})
			Expect(result).To(BeFalse())
		})

		It("should trigger after 12:00", func() {
			currentDate := time.Date(2022, time.February, 21, 13, 0, 0, 0, time.UTC)
			result := makeSchedule().ShouldNotify(ShouldNotifyParams{
				Current: NotificationData{
					State: "operational",
					Time:  currentDate,
				},
				Last: NotificationData{
					State: "operational",
					Time:  currentDate.Add(-25 * time.Hour),
				},
				Log: SchedLog,
			})
			Expect(result).To(BeTrue())
		})

		It("should trigger after 12:00 with previous time being zero value", func() {
			currentDate := time.Date(2022, time.February, 21, 13, 0, 0, 0, time.UTC)
			result := makeSchedule().ShouldNotify(ShouldNotifyParams{
				Current: NotificationData{
					State: "operational",
					Time:  currentDate,
				},
				Last: NotificationData{
					State: "operational",
					Time:  time.Time{},
				},
				Log: SchedLog,
			})
			Expect(result).To(BeTrue())
		})

		It("should not trigger more than once a day", func() {
			currentDate := time.Date(2022, time.February, 21, 14, 0, 0, 0, time.UTC)
			result := makeSchedule().ShouldNotify(ShouldNotifyParams{
				Current: NotificationData{
					State: "operational",
					Time:  currentDate,
				},
				Last: NotificationData{
					State: "operational",
					Time:  currentDate.Add(-1 * time.Hour),
				},
				Log: SchedLog,
			})
			Expect(result).To(BeFalse())
		})

	})

})

var _ = Describe("NotifyOneshot", func() {

	SchedLog := SchedulingLogger{
		Log:        GinkgoLogr,
		LogDetails: true,
	}

	It("can parse its configuration", func() {
		configStr := "delay: 3h"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		no, err := newNotifyOneshot(&config)
		Expect(err).To(Succeed())
		Expect(no.Delay).To(Equal(3 * time.Hour))
	})

	It("does not trigger when the state changed and the delay did not pass", func() {
		schedule := NotifyOneshot{Delay: 5 * time.Minute}
		now := time.Now()
		result := schedule.ShouldNotify(ShouldNotifyParams{
			Current: NotificationData{
				State: "operational",
				Time:  now,
			},
			Last: NotificationData{
				State: "in-maintenance",
				Time:  now,
			},
			Log:         SchedLog,
			StateChange: now,
		})
		Expect(result).To(BeFalse())
	})

	It("does trigger when the state changed and the delay passes", func() {
		schedule := NotifyOneshot{Delay: -5 * time.Minute}
		now := time.Now()
		result := schedule.ShouldNotify(ShouldNotifyParams{
			Current: NotificationData{
				State: "operational",
				Time:  now,
			},
			Last: NotificationData{
				State: "in-maintenance",
				Time:  now,
			},
			Log:         SchedLog,
			StateChange: now,
		})
		Expect(result).To(BeTrue())
	})

	It("does not trigger when the did not state change and the delay passes", func() {
		schedule := NotifyOneshot{Delay: -5 * time.Minute}
		now := time.Now()
		result := schedule.ShouldNotify(ShouldNotifyParams{
			Current: NotificationData{
				State: "operational",
				Time:  now,
			},
			Last: NotificationData{
				State: "operational",
				Time:  now,
			},
			Log:         SchedLog,
			StateChange: now,
		})
		Expect(result).To(BeFalse())
	})

	It("does not trigger when StateChange is zero", func() {
		schedule := NotifyOneshot{Delay: -5 * time.Minute}
		now := time.Now()
		result := schedule.ShouldNotify(ShouldNotifyParams{
			Current: NotificationData{
				State: "operational",
				Time:  now,
			},
			Last: NotificationData{
				State: "in-maintenance",
				Time:  now,
			},
			Log: SchedLog,
		})
		Expect(result).To(BeFalse())
	})

})

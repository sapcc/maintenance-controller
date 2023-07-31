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
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/go-logr/logr"
	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/ucfgwrap"
)

// Notifier is the interface that notification plugins need to implement.
// It is recommend to make notification plugins idempotent, as the same message might be send multiple times.
// A zero-initialized notification plugin should not actually work as it is used to create
// the actual usable configured instances.
type Notifier interface {
	Notify(params Parameters) error
	New(config *ucfgwrap.Config) (Notifier, error)
	ID() string
}

// NotificationInstance represents a configured and named instance of a notification plugin.
type NotificationInstance struct {
	Plugin   Notifier
	Schedule Scheduler
	Name     string
}

// NotificationChain represents a collection of multiple NotificationInstance that can be executed one after another.
type NotificationChain struct {
	Plugins []NotificationInstance
}

// Execute invokes Notify on each NotificationInstance in the chain and aborts when a plugin returns an error.
func (chain *NotificationChain) Execute(params Parameters) error {
	for _, notifier := range chain.Plugins {
		err := notifier.Plugin.Notify(params)
		if err != nil {
			return &ChainError{
				Message: fmt.Sprintf("Notification instance %v failed", notifier.Name),
				Err:     err,
			}
		}
		params.Log.Info("Executed notification instance", "instance", notifier.Name)
	}
	return nil
}

// Renders the given template string using the provided parameters.
func RenderNotificationTemplate(templateStr string, params *Parameters) (string, error) {
	templateObj, err := template.New("template").Parse(templateStr)
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	err = templateObj.Execute(&buf, params)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Data used to determine, whether to notify or not.
type NotificationData struct {
	// Technically state.NodeStateLabel, but that causes a cyclic import
	State       string
	Time        time.Time
	StateChange time.Time
}

// Used to log scheduling decisions.
type SchedulingLogger struct {
	Log        logr.Logger
	LogDetails bool
}

type ShouldNotifyParams struct {
	Current     NotificationData
	Last        NotificationData
	StateChange time.Time
	Log         SchedulingLogger
}

// Interface notification schedulers need to implement.
type Scheduler interface {
	// Determines if a notification is required.
	ShouldNotify(params ShouldNotifyParams) bool
}

// Notifies on state changes and after passing the interval since the
// last notification if not the operational state.
type NotifyPeriodic struct {
	Interval time.Duration
}

func newNotifyPeriodic(config *ucfgwrap.Config) (*NotifyPeriodic, error) {
	conf := struct {
		Interval time.Duration
	}{Interval: time.Hour}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &NotifyPeriodic{Interval: conf.Interval}, nil
}

func (np *NotifyPeriodic) ShouldNotify(params ShouldNotifyParams) bool {
	current := params.Current
	last := params.Last
	log := params.Log
	if current.State != last.State {
		return true
	} else if log.LogDetails {
		log.Log.Info("NotifyPeriodic: no state change")
	}
	if current.Time.Sub(last.Time) >= np.Interval && last.State != "operational" {
		return true
	} else if log.LogDetails {
		log.Log.Info("NotifyPeriodic: interval not passed or operational")
	}
	return false
}

// Notifies when the given instant passed on an allowed weekday.
type NotifyScheduled struct {
	Instant  time.Time
	Weekdays []time.Weekday
}

func newNotifyScheduled(config *ucfgwrap.Config) (*NotifyScheduled, error) {
	conf := struct {
		Instant  string
		Weekdays []string
	}{}
	err := config.Unpack(&conf)
	if err != nil {
		return nil, err
	}
	// sanity check
	if len(conf.Weekdays) == 0 {
		return nil, errors.New("a notification schedule needs to have weekdays specified")
	}
	instant, err := time.Parse("15:04", conf.Instant)
	if err != nil {
		return nil, err
	}
	scheduled := &NotifyScheduled{Instant: instant}
	for _, weekdayStr := range conf.Weekdays {
		weekday, err := common.WeekdayFromString(weekdayStr)
		if err != nil {
			return nil, err
		}
		scheduled.Weekdays = append(scheduled.Weekdays, weekday)
	}
	return scheduled, nil
}

func (ns *NotifyScheduled) ShouldNotify(params ShouldNotifyParams) bool {
	current := params.Current
	last := params.Last
	log := params.Log
	// check that a notification can be triggered on the current weekday
	containsWeekday := false
	for _, weekday := range ns.Weekdays {
		if weekday == current.Time.Weekday() {
			containsWeekday = true
			break
		}
	}
	if !containsWeekday {
		if log.LogDetails {
			log.Log.Info("NotifyScheduled: weekday not contained", "weekday", current.Time.Weekday())
		}
		return false
	}
	// ensure the notification triggers after that the specified instant
	// it is required to set the date to the configured values only keeping the time
	compare := time.Date(ns.Instant.Year(), ns.Instant.Month(), ns.Instant.Day(), current.Time.Hour(),
		current.Time.Minute(), current.Time.Second(), current.Time.Nanosecond(), time.UTC)
	if compare.Before(ns.Instant) {
		if log.LogDetails {
			log.Log.Info("NotifyScheduled: current time is before specified instant")
		}
		return false
	}
	// ensure the notification triggers only once a day
	if last.Time.Day() == current.Time.Day() {
		if log.LogDetails {
			log.Log.Info("NotifyScheduled: already triggered today")
		}
		return false
	}
	return true
}

type NotifyOneshot struct {
	Delay time.Duration
}

func newNotifyOneshot(config *ucfgwrap.Config) (*NotifyOneshot, error) {
	conf := struct {
		Delay time.Duration
	}{Delay: time.Minute}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &NotifyOneshot{Delay: conf.Delay}, nil
}

func (no *NotifyOneshot) ShouldNotify(params ShouldNotifyParams) bool {
	log := params.Log
	if params.StateChange.IsZero() {
		log.Log.Info("NotifyOneshot: StateChange is zero")
		return false
	}
	if params.Current.State == params.Last.State {
		if log.LogDetails {
			log.Log.Info("NotifyOneshot: no change in state")
		}
		return false
	}
	return time.Since(params.StateChange) >= no.Delay
}

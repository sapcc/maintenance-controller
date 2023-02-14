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

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
)

const day = 24 * time.Hour

type Wait struct {
	Duration time.Duration
}

func (w *Wait) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Duration string `config:"duration" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	duration, err := time.ParseDuration(conf.Duration)
	if err != nil {
		return nil, err
	}
	return &Wait{Duration: duration}, nil
}

func (w *Wait) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	if time.Since(params.LastTransition) > w.Duration {
		return plugin.Passed(nil), nil
	}
	return plugin.Failed(nil), nil
}

func (w *Wait) OnTransition(params plugin.Parameters) error {
	return nil
}

type WaitExclude struct {
	Duration time.Duration
	Exclude  []time.Weekday
}

func (we *WaitExclude) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Duration string   `config:"duration" validate:"required"`
		Exclude  []string `config:"exclude" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	duration, err := time.ParseDuration(conf.Duration)
	if err != nil {
		return nil, err
	}
	weekdays := make([]time.Weekday, 0)
	for _, weekdayStr := range conf.Exclude {
		weekday, err := common.WeekdayFromString(weekdayStr)
		if err != nil {
			return nil, err
		}
		weekdays = append(weekdays, weekday)
	}
	return &WaitExclude{Duration: duration, Exclude: weekdays}, nil
}

func (we *WaitExclude) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	return we.checkInternal(&params, time.Now().UTC()), nil
}

func (we *WaitExclude) checkInternal(params *plugin.Parameters, now time.Time) plugin.CheckResult {
	timestamp := params.LastTransition
	since := now.Sub(params.LastTransition)
	// "since" currently includes excluded days.
	// So, we loop through each day between timestamp (included) and today (included)
	// and subtract 24 hours if that weekday was excluded
	for !timestamp.After(now) {
		if !we.isExcluded(timestamp.Weekday()) {
			// not excluded => check the next day
			timestamp = timestamp.Add(day)
			continue
		}
		sub := day
		// We can only remove the full 24 hours, if the full day can be considered
		// as excluded. That does not hold for "params.LastTransition" and today.
		// To make matters worse, both can be the same day.
		if isSameDay(timestamp, params.LastTransition) {
			// Day is the same as params.LastTransition so only the time from
			// params.LastTransition to 00:00:00 can be subtracted.
			// So if params.LastTransition and now are on the same day
			// sub will be greater then since => sub becomes negative.
			// In the end we compare against a positive duration, so this is fine.
			hour, min, sec := params.LastTransition.Clock()
			sub = day - time.Duration(hour)*time.Hour - time.Duration(min)*time.Minute - time.Duration(sec)*time.Second
		}
		// subtract since and move to the next day
		since -= sub
		timestamp = timestamp.Add(day)
	}
	// if now is an excluded day and we have not accounted for it already,
	// the time from 00:00:00 to now has to be subtracted.
	if !isSameDay(params.LastTransition, now) && we.isExcluded(now.Weekday()) {
		hour, min, sec := now.Clock()
		sub := time.Duration(hour)*time.Hour + time.Duration(min)*time.Minute + time.Duration(sec)*time.Second
		since -= sub
	}
	if since > we.Duration {
		return plugin.Passed(nil)
	}
	return plugin.Failed(nil)
}

func (we *WaitExclude) isExcluded(weekday time.Weekday) bool {
	for _, excluded := range we.Exclude {
		if weekday == excluded {
			return true
		}
	}
	return false
}

func isSameDay(t, u time.Time) bool {
	tyear, tmonth, tday := t.Date()
	uyear, umonth, uday := u.Date()
	return tyear == uyear && tmonth == umonth && tday == uday
}

func (we *WaitExclude) OnTransition(params plugin.Parameters) error {
	return nil
}

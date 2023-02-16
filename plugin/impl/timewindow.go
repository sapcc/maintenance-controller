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
	"errors"
	"fmt"
	"time"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
)

const timeFormat = "15:04"
const dayMonthFormat = "Jan 2"

// TimeWindow is a check plugin that checks whether it is invoked on a certain weekday with a specified timewindow.
type TimeWindow struct {
	Start    time.Time
	End      time.Time
	Weekdays []time.Weekday
	Exclude  []time.Time
}

// New creates a new TimeWindow instance with the given config.
func (tw *TimeWindow) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Start    string
		End      string
		Weekdays []string
		Exclude  []string
	}{}
	err := config.Unpack(&conf)
	if err != nil {
		return nil, err
	}
	// sanity check
	if len(conf.Weekdays) == 0 {
		return nil, errors.New("a timewindow needs to have weekdays specified")
	}
	start, err := time.Parse(timeFormat, conf.Start)
	if err != nil {
		return nil, err
	}
	end, err := time.Parse(timeFormat, conf.End)
	if err != nil {
		return nil, err
	}
	// sanity check
	if start.After(end) {
		return nil, fmt.Errorf("the end time '%v' should be after the start time '%v'", end, start)
	}
	timewindow := &TimeWindow{Start: start, End: end}
	for _, weekdayStr := range conf.Weekdays {
		weekday, err := common.WeekdayFromString(weekdayStr)
		if err != nil {
			return nil, err
		}
		timewindow.Weekdays = append(timewindow.Weekdays, weekday)
	}
	for _, excludeStr := range conf.Exclude {
		exclude, err := time.Parse(dayMonthFormat, excludeStr)
		if err != nil {
			return nil, err
		}
		timewindow.Exclude = append(timewindow.Exclude, exclude)
	}
	return timewindow, nil
}

func (tw *TimeWindow) ID() string {
	return "timeWindow"
}

// Check checks whether the current time is within specified time window on allowed weekdays.
func (tw *TimeWindow) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	return tw.checkInternal(time.Now().UTC()), nil
}

// checkInternal expects a time in UTC.
func (tw *TimeWindow) checkInternal(current time.Time) plugin.CheckResult {
	containsWeekday := false
	for _, weekday := range tw.Weekdays {
		if weekday == current.Weekday() {
			containsWeekday = true
			break
		}
	}
	if !containsWeekday {
		return plugin.Failed(nil)
	}
	isExcluded := false
	for _, exclude := range tw.Exclude {
		if exclude.Day() == current.Day() && exclude.Month() == current.Month() {
			isExcluded = true
			break
		}
	}
	if isExcluded {
		return plugin.Failed(nil)
	}
	// It is required to set the date to the configured values only keeping the time
	compare := time.Date(tw.Start.Year(), tw.Start.Month(), tw.Start.Day(), current.Hour(),
		current.Minute(), current.Second(), current.Nanosecond(), time.UTC)
	return plugin.CheckResult{Passed: compare.After(tw.Start) && compare.Before(tw.End)}
}

func (tw *TimeWindow) OnTransition(params plugin.Parameters) error {
	return nil
}

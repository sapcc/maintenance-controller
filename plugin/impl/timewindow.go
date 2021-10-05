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
	"strings"
	"time"

	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/plugin"
)

var weekdayMap = map[string]time.Weekday{
	"monday":    time.Monday,
	"mon":       time.Monday,
	"tuesday":   time.Tuesday,
	"tue":       time.Tuesday,
	"wednesday": time.Wednesday,
	"wed":       time.Wednesday,
	"thursday":  time.Thursday,
	"thu":       time.Thursday,
	"friday":    time.Friday,
	"fri":       time.Friday,
	"saturday":  time.Saturday,
	"sat":       time.Saturday,
	"sunday":    time.Sunday,
	"sun":       time.Sunday,
}

const timeFormat = "15:04"

// TimeWindow is a check plugin that checks whether it is invoked on a certain weekday with a specified timewindow.
type TimeWindow struct {
	Start    time.Time
	End      time.Time
	Weekdays []time.Weekday
}

// New creates a new TimeWindow instance with the given config.
func (tw *TimeWindow) New(config *ucfg.Config) (plugin.Checker, error) {
	conf := struct {
		Start    string
		End      string
		Weekdays []string
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
		weekday, err := weekdayFromString(weekdayStr)
		if err != nil {
			return nil, err
		}
		timewindow.Weekdays = append(timewindow.Weekdays, weekday)
	}
	return timewindow, nil
}

func weekdayFromString(s string) (time.Weekday, error) {
	weekday, ok := weekdayMap[strings.ToLower(s)]
	if !ok {
		return time.Monday, fmt.Errorf("'%v' is not a known weekday", s)
	}
	return weekday, nil
}

// Check checks whether the current time is within specified time window on allowed weekdays.
func (tw *TimeWindow) Check(params plugin.Parameters) (bool, error) {
	return tw.checkInternal(time.Now().UTC()), nil
}

// checkInternal expects a time in UTC.
func (tw *TimeWindow) checkInternal(current time.Time) bool {
	containsWeekday := false
	for _, weekday := range tw.Weekdays {
		if weekday == current.Weekday() {
			containsWeekday = true
		}
	}
	if !containsWeekday {
		return false
	}
	// It is required to set the date to the configured values only keeping the time
	compare := time.Date(tw.Start.Year(), tw.Start.Month(), tw.Start.Day(), current.Hour(),
		current.Minute(), current.Second(), current.Nanosecond(), time.UTC)
	return compare.After(tw.Start) && compare.Before(tw.End)
}

func (tw *TimeWindow) AfterEval(chainResult bool, params plugin.Parameters) error {
	return nil
}

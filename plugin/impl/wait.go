// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"slices"
	"time"

	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/plugin"
)

const day time.Duration = 24 * time.Hour

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

func (w *Wait) ID() string {
	return "wait"
}

func (w *Wait) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	if time.Since(params.LastTransition) > w.Duration {
		return plugin.Passed(nil), nil
	}
	remaining := w.Duration - time.Since(params.LastTransition)
	return plugin.Failed(map[string]any{"remaining_seconds": remaining.Seconds()}), nil
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

func (we *WaitExclude) ID() string {
	return "waitExclude"
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
			hour, minute, sec := params.LastTransition.Clock()
			sub = day - time.Duration(hour)*time.Hour - time.Duration(minute)*time.Minute - time.Duration(sec)*time.Second
		}
		// subtract since and move to the next day
		since -= sub
		timestamp = timestamp.Add(day)
	}
	// if now is an excluded day and we have not accounted for it already,
	// the time from 00:00:00 to now has to be subtracted.
	if !isSameDay(params.LastTransition, now) && we.isExcluded(now.Weekday()) {
		hour, minute, sec := now.Clock()
		sub := time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute + time.Duration(sec)*time.Second
		since -= sub
	}
	if since > we.Duration {
		return plugin.Passed(nil)
	}
	remaining := we.Duration - since
	return plugin.Failed(map[string]any{"remaining_seconds": remaining.Seconds()})
}

func (we *WaitExclude) isExcluded(weekday time.Weekday) bool {
	return slices.Contains(we.Exclude, weekday)
}

func isSameDay(t, u time.Time) bool {
	tyear, tmonth, tday := t.Date()
	uyear, umonth, uday := u.Date()
	return tyear == uyear && tmonth == umonth && tday == uday
}

func (we *WaitExclude) OnTransition(params plugin.Parameters) error {
	return nil
}

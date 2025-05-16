// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"
	"strings"
	"time"
)

var WeekdayMap = map[string]time.Weekday{
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

func WeekdayFromString(s string) (time.Weekday, error) {
	weekday, ok := WeekdayMap[strings.ToLower(s)]
	if !ok {
		return time.Monday, fmt.Errorf("'%v' is not a known weekday", s)
	}
	return weekday, nil
}

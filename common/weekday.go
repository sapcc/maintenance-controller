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

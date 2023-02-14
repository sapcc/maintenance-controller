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

import "strings"

// when the maintenance-controller is using go 1.20 the errs slice can be wrapped properly.
func ConcatErrors(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	errStrings := make([]string, 0)
	for _, err := range errs {
		errStrings = append(errStrings, err.Error())
	}
	return strings.Join(errStrings, ", ")
}

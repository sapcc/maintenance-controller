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
	"fmt"

	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"

	"github.com/sapcc/maintenance-controller/plugin"
)

// Condition is a check plugin that checks if a node
// has a certain status for a defined condition.
type Condition struct {
	Type   string
	Status string
}

// New creates a new Condition instance with the given config.
func (c *Condition) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Type   string `config:"type" validate:"required"`
		Status string `config:"status" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &Condition{Type: conf.Type, Status: conf.Status}, nil
}

func (c *Condition) ID() string {
	return "condition"
}

// Check asserts that the given status matches the specified condition.
func (c *Condition) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	for _, condition := range params.Node.Status.Conditions {
		if condition.Type == v1.NodeConditionType(c.Type) {
			info := map[string]any{"current": condition.Status, "expected": c.Status}
			return plugin.CheckResult{Passed: condition.Status == v1.ConditionStatus(c.Status), Info: info}, nil
		}
	}
	return plugin.FailedWithReason(fmt.Sprintf("condition %s not present", c.Type)), nil
}

func (c *Condition) OnTransition(params plugin.Parameters) error {
	return nil
}

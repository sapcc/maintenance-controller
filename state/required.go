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

package state

import (
	"fmt"

	"github.com/sapcc/maintenance-controller/plugin"
)

// maintenanceRequired implements the transition and notification logic if a node is in the MaintenanceRequired state.
type maintenanceRequired struct {
	chains PluginChains
	label  NodeStateLabel
}

func newMaintenanceRequired(chains PluginChains) NodeState {
	return &maintenanceRequired{chains: chains, label: Required}
}

func (s *maintenanceRequired) Label() NodeStateLabel {
	return s.label
}

func (s *maintenanceRequired) Enter(params plugin.Parameters, data *Data) error {
	return nil
}

func (s *maintenanceRequired) Notify(params plugin.Parameters, data *Data) error {
	return notifyDefault(params, data, &s.chains.Notification, s.label, data.PreviousStates[params.Profile])
}

func (s *maintenanceRequired) Trigger(params plugin.Parameters, next NodeStateLabel, data *Data) error {
	for _, transition := range s.chains.Transitions {
		if transition.Next == next {
			return transition.Trigger.Execute(params)
		}
	}
	return fmt.Errorf("could not find triggers from %s to %s", s.Label(), next)
}

func (s *maintenanceRequired) Transition(params plugin.Parameters, data *Data) (TransitionsResult, error) {
	return transitionDefault(params, s.Label(), s.chains.Transitions)
}

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
	"time"

	"github.com/sapcc/maintenance-controller/plugin"
)

// maintenanceRequired implements the transition and notification logic if a node is in the MaintenanceRequired state
type maintenanceRequired struct {
	chains   PluginChains
	label    NodeStateLabel
	interval time.Duration
}

func newMaintenanceRequired(chains PluginChains, interval time.Duration) NodeState {
	return &maintenanceRequired{chains: chains, interval: interval, label: Required}
}

func (s *maintenanceRequired) Label() NodeStateLabel {
	return s.label
}

func (s *maintenanceRequired) Notify(params plugin.Parameters, data Data) error {
	return notifyDefault(params, data, s.interval, &s.chains.Notification, s.label)
}

func (s *maintenanceRequired) Trigger(params plugin.Parameters, data Data) error {
	return s.chains.Trigger.Execute(params)
}

func (s *maintenanceRequired) Transition(params plugin.Parameters, data Data) (NodeStateLabel, error) {
	// if there are no checks configured the node can be put into maintenance, because no additional approvals are required
	if len(s.chains.Check.Plugins) == 0 {
		return InMaintenance, nil
	}
	result, err := s.chains.Check.Execute(params)
	if err != nil {
		return Required, err
	}
	if result {
		return InMaintenance, nil
	}
	return Required, nil
}

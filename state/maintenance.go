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

// inMaintenance implements the transition and notification logic if a node is in the InMaintenance state
type inMaintenance struct {
	chains   PluginChains
	label    NodeStateLabel
	interval time.Duration
}

func newInMaintenance(chains PluginChains, interval time.Duration) NodeState {
	return &inMaintenance{chains: chains, interval: interval, label: InMaintenance}
}

func (s *inMaintenance) Label() NodeStateLabel {
	return s.label
}

func (s *inMaintenance) Notify(params plugin.Parameters, data Data) error {
	return notifyDefault(params, data, s.interval, &s.chains.Notification, s.label)
}

func (s *inMaintenance) Trigger(params plugin.Parameters, data Data) error {
	return s.chains.Trigger.Execute(params)
}

func (s *inMaintenance) Transition(params plugin.Parameters, data Data) (NodeStateLabel, error) {
	// if no checks are configured the node is considered as operational again
	if len(s.chains.Check.Plugins) == 0 {
		return Operational, nil
	}
	result, err := s.chains.Check.Execute(params)
	if err != nil {
		return InMaintenance, err
	}
	if result {
		return Operational, nil
	}
	return InMaintenance, nil
}

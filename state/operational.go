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

// operational implements the transition and notification logic if a node is in the operational state.
type operational struct {
	chains   PluginChains
	label    NodeStateLabel
	interval time.Duration
}

func newOperational(chains PluginChains, interval time.Duration) NodeState {
	return &operational{chains: chains, interval: interval, label: Operational}
}

func (s *operational) Label() NodeStateLabel {
	return s.label
}

func (s *operational) Notify(params plugin.Parameters, data *Data) error {
	if data.LastNotificationState == Operational {
		return nil
	}
	err := s.chains.Notification.Execute(params)
	if err != nil {
		return err
	}
	data.LastNotification = time.Now()
	data.LastNotificationState = Operational
	return nil
}

func (s *operational) Trigger(params plugin.Parameters, data *Data) error {
	return s.chains.Trigger.Execute(params)
}

func (s *operational) Transition(params plugin.Parameters, data *Data) (NodeStateLabel, error) {
	// if no checks are configured the node will be ignored by the controller
	// (by staying in operational state all the time)
	if len(s.chains.Check.Plugins) == 0 {
		return Operational, nil
	}
	result, err := s.chains.Check.Execute(params)
	if err != nil {
		return Operational, err
	}
	if result {
		return Required, nil
	}
	return Operational, nil
}

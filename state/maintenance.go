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

	"github.com/sapcc/maintenance-controller/metrics"
	"github.com/sapcc/maintenance-controller/plugin"
)

// inMaintenance implements the transition and notification logic if a node is in the InMaintenance state.
type inMaintenance struct {
	chains PluginChains
	label  NodeStateLabel
}

func newInMaintenance(chains PluginChains) NodeState {
	return &inMaintenance{chains: chains, label: InMaintenance}
}

func (s *inMaintenance) Label() NodeStateLabel {
	return s.label
}

func (s *inMaintenance) Enter(params plugin.Parameters, data *DataV2) error {
	if err := s.chains.Enter.Execute(params); err != nil {
		return err
	}
	if err := metrics.RecordShuffles(
		params.Ctx,
		params.Client,
		params.Node,
		params.Profile,
	); err != nil {
		params.Log.Info("failed to record shuffle metrics", "profile", params.Profile, "error", err)
	}
	return nil
}

func (s *inMaintenance) Notify(params plugin.Parameters, data *DataV2) error {
	return notifyDefault(params, data, &s.chains.Notification)
}

func (s *inMaintenance) Trigger(params plugin.Parameters, next NodeStateLabel, data *DataV2) error {
	for _, transition := range s.chains.Transitions {
		if transition.Next == next {
			return transition.Trigger.Execute(params)
		}
	}
	return fmt.Errorf("could not find triggers from %s to %s", s.Label(), next)
}

func (s *inMaintenance) Transition(params plugin.Parameters, data *DataV2) (TransitionsResult, error) {
	return transitionDefault(params, s.Label(), s.chains.Transitions)
}

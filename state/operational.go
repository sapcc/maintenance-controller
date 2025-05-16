// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
package state

import (
	"fmt"

	"github.com/sapcc/maintenance-controller/plugin"
)

// operational implements the transition and notification logic if a node is in the operational state.
type operational struct {
	chains PluginChains
	label  NodeStateLabel
}

func newOperational(chains PluginChains) NodeState {
	return &operational{chains: chains, label: Operational}
}

func (s *operational) Label() NodeStateLabel {
	return s.label
}

func (s *operational) Enter(params plugin.Parameters, data *DataV2) error {
	return s.chains.Enter.Execute(params)
}

func (s *operational) Notify(params plugin.Parameters, data *DataV2) error {
	return notifyDefault(params, data, &s.chains.Notification)
}

func (s *operational) Trigger(params plugin.Parameters, next NodeStateLabel, data *DataV2) error {
	for _, transition := range s.chains.Transitions {
		if transition.Next == next {
			return transition.Trigger.Execute(params)
		}
	}
	return fmt.Errorf("could not find triggers from %s to %s", s.Label(), next)
}

func (s *operational) Transition(params plugin.Parameters, data *DataV2) (TransitionsResult, error) {
	return transitionDefault(params, s.Label(), s.chains.Transitions)
}

// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//nolint:dupl
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
	return s.chains.Enter.Execute(params)
}

func (s *maintenanceRequired) Notify(params plugin.Parameters, data *Data) error {
	return notifyDefault(params, data, &s.chains.Notification)
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

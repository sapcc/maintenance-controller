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

// NodeStateLabel reprensents labels which nodes a marked with
type NodeStateLabel string

// Operational is a label that marks a node which is operational
const Operational NodeStateLabel = "operational"

// Required is a label that marks a node which needs to be maintenaned
const Required NodeStateLabel = "required"

// InMaintenance is a label that marks a node which is currently in maintenance
const InMaintenance NodeStateLabel = "in-maintenance"

// PluginChains is a struct containing a plugin chain of each plugin type
type PluginChains struct {
	Check        plugin.CheckChain
	Notification plugin.NotificationChain
	Trigger      plugin.TriggerChain
}

// Data represents global state which is saved with a node annotation
type Data struct {
	LastTransition        time.Time
	LastNotification      time.Time
	LastNotificationState NodeStateLabel
}

// NodeState represents the state a node can be in
type NodeState interface {
	// Label is the Label associated with the state
	Label() NodeStateLabel
	// Notify executes the notification chain if required
	Notify(params plugin.Parameters, data Data) error
	// Trigger executes the trigger chain
	Trigger(params plugin.Parameters, data Data) error
	// Trigger executes the check chain and determines, which state should be the next one
	Transition(params plugin.Parameters, data Data) (NodeStateLabel, error)
}

// notifyDefault is a default NodeState.Notify implemention that executes the notification chain again after a specified interval
func notifyDefault(params plugin.Parameters, data Data, interval time.Duration, chain *plugin.NotificationChain, label NodeStateLabel) error {
	// ensure there is a new state or the interval has passed
	if label == data.LastNotificationState && time.Since(data.LastNotification) <= interval {
		return nil
	}
	err := chain.Execute(params)
	if err != nil {
		return err
	}
	data.LastNotification = time.Now()
	data.LastNotificationState = label
	return nil
}

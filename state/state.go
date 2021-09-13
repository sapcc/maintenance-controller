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
	"strings"
	"time"

	"github.com/sapcc/maintenance-controller/event"
	"github.com/sapcc/maintenance-controller/plugin"
	v1 "k8s.io/api/core/v1"
)

// NodeStateLabel reprensents labels which nodes a marked with.
type NodeStateLabel string

// Operational is a label that marks a node which is operational.
const Operational NodeStateLabel = "operational"

// Required is a label that marks a node which needs to be maintenaned.
const Required NodeStateLabel = "maintenance-required"

// InMaintenance is a label that marks a node which is currently in maintenance.
const InMaintenance NodeStateLabel = "in-maintenance"

// profileSeparator is used to split the maintenance profile label string into multple profile names.
const profileSeparator string = "-"

// PluginChains is a struct containing a plugin chain of each plugin type.
type PluginChains struct {
	Check        plugin.CheckChain
	Notification plugin.NotificationChain
	Trigger      plugin.TriggerChain
}

type Profile struct {
	Name   string
	Chains map[NodeStateLabel]PluginChains
}

// Data represents global state which is saved with a node annotation.
type Data struct {
	LastTransition        time.Time
	LastNotification      time.Time
	LastNotificationState NodeStateLabel
	LastProfile           string
}

// NodeState represents the state a node can be in.
type NodeState interface {
	// Label is the Label associated with the state
	Label() NodeStateLabel
	// Notify executes the notification chain if required
	Notify(params plugin.Parameters, data *Data) error
	// Trigger executes the trigger chain
	Trigger(params plugin.Parameters, data *Data) error
	// Trigger executes the check chain and determines, which state should be the next one.
	// If an error is returned the NodeStateLabel must match the current state.
	Transition(params plugin.Parameters, data *Data) (NodeStateLabel, error)
}

// FromLabel creates a new NodeState instance identified by the label with given chains and notification interval.
func FromLabel(label NodeStateLabel, chains PluginChains, interval time.Duration) (NodeState, error) {
	switch label {
	case Operational:
		return newOperational(chains, interval), nil
	case Required:
		return newMaintenanceRequired(chains, interval), nil
	case InMaintenance:
		return newInMaintenance(chains, interval), nil
	}
	return nil, fmt.Errorf("node state \"%v\" is not known", label)
}

// Calls all Notification Plugins, checks for a transition
// and invokes all trigger plugins if a transitions happens.
// Returns the next node state.
// In case of an error state.Label() is retuned alongside with the error.
func Apply(state NodeState, node *v1.Node, data *Data, params plugin.Parameters) (NodeStateLabel, error) {
	recorder := params.Recorder.(*event.SourcingRecorder)
	source := v1.EventSource{
		Component: "maintenance-controller",
		Host:      node.Name,
	}
	// invoke notifications and check for transition
	err := state.Notify(params, data)
	if err != nil {
		recorder.SourcedEventf(node, source, "Normal", "ChangeMaintenanceStateFailed",
			"At least one notification plugin failed for profile %v: Will stay in %v state",
			params.Profile.Current, params.State)
		return state.Label(), fmt.Errorf("failed to notify for profile %v: %w", params.Profile.Current, err)
	}
	next, err := state.Transition(params, data)
	if err != nil {
		recorder.SourcedEventf(node, source, "Normal", "ChangeMaintenanceStateFailed",
			"At least one check plugin failed for profile %v: Will stay in %v state",
			params.Profile.Current, params.State)
		params.Log.Error(err, "Failed to check for state transition", "state", params.State,
			"profile", params.Profile.Current)
		return state.Label(), err
	}

	// check if a transition should happen
	if next != state.Label() {
		err = state.Trigger(params, data)
		if err != nil {
			params.Log.Error(err, "Failed to execute triggers", "state", params.State, "profile", params.Profile.Current)
			recorder.SourcedEventf(node, source, "Normal", "ChangeMaintenanceStateFailed",
				"At least one trigger plugin failed for profile %v: Will stay in %v state", params.Profile.Current, params.State)
			return state.Label(), err
		} else {
			params.Log.Info("Moved node to next state", "state", string(next), "profile", params.Profile.Current)
			recorder.SourcedEventf(node, source, "Normal", "ChangedMaintenanceState",
				"The node is now in the %v state caused by profile %v", string(next), params.Profile.Current)
			return next, nil
		}
	}
	return state.Label(), nil
}

// notifyDefault is a default NodeState.Notify implemention that executes
// the notification chain again after a specified interval.
func notifyDefault(params plugin.Parameters, data *Data, interval time.Duration,
	chain *plugin.NotificationChain, label NodeStateLabel) error {
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

type ProfileSelector struct {
	NodeState         NodeStateLabel
	NodeProfiles      string
	AvailableProfiles map[string]Profile
	Data              Data
}

// Gets a slice of applicable profiles based on the current state,
// the profile label and the profile that caused the last transition.
// If a nodes state is operational all profiles can be used for a transition.
// If a nodes state is required or in-maintenance transitions can only happen
// based on the profile that caused the whole maintenance "procedure".
func GetApplicableProfiles(selector ProfileSelector) ([]Profile, error) {
	all, err := getProfiles(selector.NodeProfiles, selector.AvailableProfiles)
	if err != nil {
		return nil, err
	}
	switch selector.NodeState {
	case Operational:
		return all, err
	case InMaintenance, Required:
		for _, profile := range all {
			if profile.Name == selector.Data.LastProfile {
				return []Profile{profile}, nil
			}
		}
	}
	return nil, fmt.Errorf("No applicable profiles found for the current state %v.", string(selector.NodeState))
}

// parses the value ProfileLabelKey into a slice of profiles (which are sourced from the available Profiles).
func getProfiles(profilesStr string, availableProfiles map[string]Profile) ([]Profile, error) {
	profiles := make([]Profile, 0)
	for _, iterProfile := range strings.Split(profilesStr, profileSeparator) {
		profile, ok := availableProfiles[iterProfile]
		if !ok {
			return nil, fmt.Errorf("cannot find the requested maintenance profile %v", iterProfile)
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

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
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/metrics"
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
const profileSeparator string = "--"

func ValidateLabel(s string) (NodeStateLabel, error) {
	switch s {
	case string(Operational):
		return Operational, nil
	case string(Required):
		return Required, nil
	case string(InMaintenance):
		return InMaintenance, nil
	}
	return Operational, fmt.Errorf("'%s' is not a valid NodeStateLabel", s)
}

type Transition struct {
	Check   plugin.CheckChain
	Trigger plugin.TriggerChain
	Next    NodeStateLabel
}

// PluginChains is a struct containing a plugin chain of each plugin type.
type PluginChains struct {
	Notification plugin.NotificationChain
	Transitions  []Transition
}

type Profile struct {
	Name   string
	Chains map[NodeStateLabel]PluginChains
}

// Data represents global state which is saved with a node annotation.
type Data struct {
	LastTransition time.Time
	// Maps a notification instance name to the last time it was triggered.
	LastNotificationTimes map[string]time.Time
	// Current states of assigned profiles
	ProfileStates map[string]NodeStateLabel
	// States of profiles of the previous reconciliation
	PreviousStates map[string]NodeStateLabel
}

func ParseData(node *v1.Node) (Data, error) {
	dataStr := node.Annotations[constants.DataAnnotationKey]
	var data Data
	if dataStr != "" {
		err := json.Unmarshal([]byte(dataStr), &data)
		if err != nil {
			return Data{}, fmt.Errorf("failed to parse json value in data annotation: %w", err)
		}
	}
	if data.LastNotificationTimes == nil {
		data.LastNotificationTimes = make(map[string]time.Time)
	}
	if data.PreviousStates == nil {
		data.PreviousStates = make(map[string]NodeStateLabel)
	}
	return data, nil
}

// NodeState represents the state a node can be in.
type NodeState interface {
	// Label is the Label associated with the state
	Label() NodeStateLabel
	// Notify executes the notification chain if required
	Notify(params plugin.Parameters, data *Data) error
	// Trigger executes the trigger chain
	Trigger(params plugin.Parameters, next NodeStateLabel, data *Data) error
	// Trigger executes the check chain and determines, which state should be the next one.
	// If an error is returned the NodeStateLabel must match the current state.
	Transition(params plugin.Parameters, data *Data) (NodeStateLabel, error)
}

// FromLabel creates a new NodeState instance identified by the label with given chains and notification interval.
func FromLabel(label NodeStateLabel, chains PluginChains) (NodeState, error) {
	switch label {
	case Operational:
		return newOperational(chains), nil
	case Required:
		return newMaintenanceRequired(chains), nil
	case InMaintenance:
		return newInMaintenance(chains), nil
	}
	return nil, fmt.Errorf("node state \"%v\" is not known", label)
}

// Calls all Notification Plugins, checks for a transition
// and invokes all trigger plugins if a transitions happens.
// Returns the next node state.
// In case of an error state.Label() is retuned alongside with the error.
func Apply(state NodeState, node *v1.Node, data *Data, params plugin.Parameters) (NodeStateLabel, error) {
	recorder := params.Recorder
	// invoke notifications and check for transition
	err := state.Notify(params, data)
	if err != nil {
		recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
			"At least one notification plugin failed for profile %v: Will stay in %v state",
			params.Profile, params.State)
		params.Log.Error(err, "Failed to notify", "state", params.State,
			"profile", params.Profile)
		return state.Label(), fmt.Errorf("failed to notify for profile %v: %w", params.Profile, err)
	}
	next, err := state.Transition(params, data)
	if err != nil {
		recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
			"At least one check plugin failed for profile %v: Will stay in %v state",
			params.Profile, params.State)
		params.Log.Error(err, "Failed to check for state transition", "state", params.State,
			"profile", params.Profile)
		return state.Label(), err
	}

	// check if a transition should happen
	if next != state.Label() {
		err = state.Trigger(params, next, data)
		if err != nil {
			params.Log.Error(err, "Failed to execute triggers", "state", params.State, "profile", params.Profile)
			recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
				"At least one trigger plugin failed for profile %v: Will stay in %v state", params.Profile, params.State)
			return state.Label(), err
		}
		params.Log.Info("Moved node to next state", "state", string(next), "profile", params.Profile)
		recorder.Eventf(node, "Normal", "ChangedMaintenanceState",
			"The node is now in the %v state caused by profile %v", string(next), params.Profile)
		return next, nil
	}
	return state.Label(), nil
}

// transitionDefault is a default NodeState.Transition implementation that checks
// each specified transition in order and returns the next state. If len(trans)
// is 0, the current state is returned.
func transitionDefault(params plugin.Parameters, current NodeStateLabel, trans []Transition) (NodeStateLabel, error) {
	for _, transition := range trans {
		shouldTransition, err := transition.Check.Execute(params)
		if err != nil {
			return current, err
		}
		if !shouldTransition {
			continue
		}
		// Shuffles need to be recorded when entering the in-maintenance state.
		// So we do it here, instead of checking in Transition() of each NodeState
		// implementation.
		if transition.Next == InMaintenance {
			// ensure only one profile can be in-maintenance at a time.
			if params.InMaintenance {
				return current, nil
			}
			if err := metrics.RecordShuffles(
				params.Ctx,
				params.Client,
				params.Node,
				params.Profile,
			); err != nil {
				params.Log.Info("failed to record shuffle metrics", "profile", params.Profile, "error", err)
			}
		}
		return transition.Next, nil
	}
	return current, nil
}

// notifyDefault is a default NodeState.Notify implemention that executes
// the notification chain again after a specified interval.
func notifyDefault(params plugin.Parameters, data *Data, chain *plugin.NotificationChain,
	currentState NodeStateLabel, previousState NodeStateLabel) error {
	for _, notifyPlugin := range chain.Plugins {
		if notifyPlugin.Schedule == nil {
			return fmt.Errorf("notification plugin instance %s has no schedule assigned", notifyPlugin.Name)
		}
		_, ok := data.LastNotificationTimes[notifyPlugin.Name]
		if !ok {
			data.LastNotificationTimes[notifyPlugin.Name] = time.Time{}
		}
		now := time.Now().UTC()
		shouldNotify := notifyPlugin.Schedule.ShouldNotify(plugin.NotificationData{
			State: string(currentState),
			Time:  now,
		}, plugin.NotificationData{
			State: string(previousState),
			Time:  data.LastNotificationTimes[notifyPlugin.Name],
		})
		if !shouldNotify {
			continue
		}
		if err := notifyPlugin.Plugin.Notify(params); err != nil {
			return err
		}
		params.Log.Info("Executed notification instance", "instance", notifyPlugin.Name)
		data.LastNotificationTimes[notifyPlugin.Name] = now
	}
	return nil
}

type ProfileSelector struct {
	NodeState         NodeStateLabel
	NodeProfiles      string
	AvailableProfiles map[string]Profile
	Data              Data
}

// Parses the value ProfileLabelKey into a slice of profiles (which are sourced from the available Profiles).
// Skips a possible profile if profileStr contains a profile, which is not part of availableProfiles.
func getProfiles(profilesStr string, availableProfiles map[string]Profile) []Profile {
	profiles := make([]Profile, 0)
	for _, iterProfile := range strings.Split(profilesStr, profileSeparator) {
		profile, ok := availableProfiles[iterProfile]
		if !ok {
			continue
		}
		profiles = append(profiles, profile)
	}
	return profiles
}

type ProfileState struct {
	Profile Profile
	State   NodeStateLabel
}

func (d *Data) GetProfilesWithState(profilesStr string, availableProfiles map[string]Profile) []ProfileState {
	if d.ProfileStates == nil {
		d.ProfileStates = make(map[string]NodeStateLabel)
	}
	// cleanup unused states
	toRemove := make([]string, 0)
	for profileName := range d.ProfileStates {
		if !strings.Contains(profilesStr, profileName) {
			toRemove = append(toRemove, profileName)
		}
	}
	for _, remove := range toRemove {
		delete(d.ProfileStates, remove)
	}
	// fetch old and add new states
	result := make([]ProfileState, 0)
	profiles := getProfiles(profilesStr, availableProfiles)
	for _, profile := range profiles {
		if state, ok := d.ProfileStates[profile.Name]; ok {
			result = append(result, ProfileState{
				Profile: profile,
				State:   state,
			})
		} else {
			d.ProfileStates[profile.Name] = Operational
			result = append(result, ProfileState{
				Profile: profile,
				State:   Operational,
			})
		}
	}
	return result
}

func (d *Data) MaintainPreviousStates(profilesStr string, availableProfiles map[string]Profile) {
	if d.PreviousStates == nil {
		d.PreviousStates = make(map[string]NodeStateLabel)
	}
	// cleanup unused states
	toRemove := make([]string, 0)
	for profileName := range d.PreviousStates {
		if !strings.Contains(profilesStr, profileName) {
			toRemove = append(toRemove, profileName)
		}
	}
	for _, remove := range toRemove {
		delete(d.PreviousStates, remove)
	}
	// initialize missing previous values using the current values
	profiles := getProfiles(profilesStr, availableProfiles)
	for _, profile := range profiles {
		if _, ok := d.PreviousStates[profile.Name]; !ok {
			d.PreviousStates[profile.Name] = d.ProfileStates[profile.Name]
		}
	}
}

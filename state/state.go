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

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
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

// Returns whether profile is contained in the profile label value.
func ContainsProfile(allProfiles, profile string) bool {
	for _, oneProfile := range strings.Split(allProfiles, profileSeparator) {
		if profile == oneProfile {
			return true
		}
	}
	return false
}

// Returns whether s as NodeStateLabel if it is valid.
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

type TransitionResult struct {
	Passed bool                    `json:"passed"`
	Target NodeStateLabel          `json:"target"`
	Chain  plugin.CheckChainResult `json:"chain"`
	Error  string                  `json:"error"`
}

type TransitionsResult struct {
	Next  NodeStateLabel     `json:"next"`
	Infos []TransitionResult `json:"infos"`
}

type ApplyResult struct {
	Next        NodeStateLabel     `json:"next"`
	Transitions []TransitionResult `json:"transitions"`
	Error       string             `json:"error"`
}

type ProfileResult struct {
	Applied ApplyResult    `json:"applied"`
	Name    string         `json:"name"`
	State   NodeStateLabel `json:"state"`
}

type NodeInfo struct {
	Node     string          `json:"node"`
	Profiles []ProfileResult `json:"profiles"`
	Updated  time.Time       `json:"updated"`
}

// PluginChains is a struct containing a plugin chain of each plugin type.
type PluginChains struct {
	Notification plugin.NotificationChain
	Transitions  []Transition
}

// Profile contains its name and attached plugin chains.
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
	// Enter is executed when a node enters a new state.
	// Its not executed when a profile gets freshly attached.
	Enter(params plugin.Parameters, data *Data) error
	// Notify executes the notification chain if required
	Notify(params plugin.Parameters, data *Data) error
	// Trigger executes the trigger chain
	Trigger(params plugin.Parameters, next NodeStateLabel, data *Data) error
	// Trigger executes the check chain and determines, which state should be the next one.
	// If an error is returned the NodeStateLabel must match the current state.
	Transition(params plugin.Parameters, data *Data) (TransitionsResult, error)
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
func Apply(state NodeState, node *v1.Node, data *Data, params plugin.Parameters) (ApplyResult, error) {
	recorder := params.Recorder
	result := ApplyResult{Next: state.Label(), Transitions: []TransitionResult{}}
	if data.PreviousStates[params.Profile] != data.ProfileStates[params.Profile] {
		if err := state.Enter(params, data); err != nil {
			recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
				"Failed to enter state for profile %v: Will stay in %v state",
				params.Profile, params.State)
			result.Error = err.Error()
			return result, fmt.Errorf("failed to enter state %v for profile %v: %w", state.Label(), params.Profile, err)
		}
	}
	// invoke notifications and check for transition
	err := state.Notify(params, data)
	if err != nil {
		recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
			"At least one notification plugin failed for profile %v: Will stay in %v state",
			params.Profile, params.State)
		params.Log.Error(err, "Failed to notify", "state", params.State,
			"profile", params.Profile)
		result.Error = err.Error()
		return result, fmt.Errorf("failed to notify for profile %v: %w", params.Profile, err)
	}
	transitions, err := state.Transition(params, data)
	result.Transitions = transitions.Infos
	if err != nil {
		recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
			"At least one check plugin failed for profile %v: Will stay in %v state",
			params.Profile, params.State)
		params.Log.Error(err, "Failed to check for state transition", "state", params.State,
			"profile", params.Profile)
		result.Error = err.Error()
		return result, fmt.Errorf("failed transition for profile %v: %w", params.Profile, err)
	}

	// check if a transition should happen
	if transitions.Next != state.Label() {
		err = state.Trigger(params, transitions.Next, data)
		if err != nil {
			params.Log.Error(err, "Failed to execute triggers", "state", params.State, "profile", params.Profile)
			recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
				"At least one trigger plugin failed for profile %v: Will stay in %v state", params.Profile, params.State)
			result.Error = err.Error()
			return result, err
		}
		params.Log.Info("Moved node to next state", "state", string(transitions.Next), "profile", params.Profile)
		recorder.Eventf(node, "Normal", "ChangedMaintenanceState",
			"The node is now in the %v state caused by profile %v", string(transitions.Next), params.Profile)
		result.Next = transitions.Next
		return result, nil
	}
	return result, nil
}

// transitionDefault is a default NodeState.Transition implementation that checks
// each specified transition in order and returns the next state. If len(trans)
// is 0, the current state is returned.
func transitionDefault(params plugin.Parameters, current NodeStateLabel, ts []Transition) (TransitionsResult, error) {
	results := make([]TransitionResult, 0)
	errs := make([]error, 0)
	for _, transtition := range ts {
		result, err := transtition.Execute(params)
		results = append(results, result)
		if err != nil {
			errs = append(errs, err)
		}
	}
	final := TransitionsResult{
		Next:  current,
		Infos: results,
	}
	if len(errs) > 0 {
		return final, fmt.Errorf("had failed transition checks: %s", common.ConcatErrors(errs))
	}
	for i, result := range results {
		if !result.Passed {
			continue
		}
		for _, check := range ts[i].Check.Plugins {
			err := check.Plugin.OnTransition(params)
			if err != nil {
				return final, err
			}
		}
		final.Next = result.Target
		return final, nil
	}
	return final, nil
}

// notifyDefault is a default NodeState.Notify implemention that executes
// the notification chain again after a specified interval.
func notifyDefault(params plugin.Parameters, data *Data, chain *plugin.NotificationChain,
	currentState NodeStateLabel, previousState NodeStateLabel) error {
	for _, notifyInstance := range chain.Plugins {
		if notifyInstance.Schedule == nil {
			return fmt.Errorf("Notification plugin instance %s has no schedule assigned", notifyInstance.Name)
		}
		_, ok := data.LastNotificationTimes[notifyInstance.Name]
		if !ok {
			data.LastNotificationTimes[notifyInstance.Name] = time.Time{}
		}
		now := time.Now().UTC()
		shouldNotify := notifyInstance.Schedule.ShouldNotify(plugin.NotificationData{
			State: string(currentState),
			Time:  now,
		}, plugin.NotificationData{
			State: string(previousState),
			Time:  data.LastNotificationTimes[notifyInstance.Name],
		}, plugin.SchedulingLogger{
			Log:        params.Log,
			LogDetails: params.LogDetails,
		})
		if !shouldNotify {
			if params.LogDetails {
				params.Log.Info("Notification instance is not scheduled to run",
					"node", params.Node.Name, "instance", notifyInstance.Name)
			}
			continue
		}
		if err := notifyInstance.Plugin.Notify(params); err != nil {
			return err
		}
		params.Log.Info("Executed notification instance", "instance", notifyInstance.Name)
		data.LastNotificationTimes[notifyInstance.Name] = now
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

func (t *Transition) Execute(params plugin.Parameters) (TransitionResult, error) {
	chainResult, err := t.Check.Execute(params)
	if err != nil {
		return TransitionResult{Passed: false, Target: t.Next, Chain: chainResult, Error: err.Error()}, err
	}
	// ensure only one profile can be in-maintenance at a time.
	if !chainResult.Passed || (t.Next == InMaintenance && params.InMaintenance) {
		return TransitionResult{Passed: false, Target: t.Next, Chain: chainResult}, nil
	}
	return TransitionResult{Passed: true, Target: t.Next, Chain: chainResult}, nil
}

// Returns a Profile instance with its corresponding state for each profile named in profileStr.
// If profileStr is an empty string, falls back to the default profile.
// Call MaintainProfileStates before.
func (d *Data) GetProfilesWithState(profilesStr string, availableProfiles map[string]Profile) []ProfileState {
	// if no profile is attached, use the default profile
	if profilesStr == "" {
		profilesStr = constants.DefaultProfileName
	}
	result := make([]ProfileState, 0)
	profiles := getProfiles(profilesStr, availableProfiles)
	for _, profile := range profiles {
		state := d.ProfileStates[profile.Name]
		result = append(result, ProfileState{
			Profile: profile,
			State:   state,
		})
	}
	return result
}

// Removes state data for removed profile and initializes it for added profiles.
func (d *Data) MaintainProfileStates(profilesStr string, availableProfiles map[string]Profile) {
	if d.ProfileStates == nil {
		d.ProfileStates = make(map[string]NodeStateLabel)
	}
	// if no profile is attached, use the default profile
	if profilesStr == "" {
		profilesStr = constants.DefaultProfileName
	}
	// cleanup unused states
	toRemove := make([]string, 0)
	for profileName := range d.ProfileStates {
		if !ContainsProfile(profilesStr, profileName) {
			toRemove = append(toRemove, profileName)
		}
	}
	for _, remove := range toRemove {
		delete(d.ProfileStates, remove)
	}
	// initialize new states
	profiles := getProfiles(profilesStr, availableProfiles)
	for _, profile := range profiles {
		if _, ok := d.ProfileStates[profile.Name]; !ok {
			d.ProfileStates[profile.Name] = Operational
		}
	}
}

// Removes previous state data for removed profile and initializes it for added profiles.
func (d *Data) MaintainPreviousStates(profilesStr string, availableProfiles map[string]Profile) {
	if d.PreviousStates == nil {
		d.PreviousStates = make(map[string]NodeStateLabel)
	}
	// if no profile is attached, use the default profile
	if profilesStr == "" {
		profilesStr = constants.DefaultProfileName
	}
	// cleanup unused states
	toRemove := make([]string, 0)
	for profileName := range d.PreviousStates {
		if !ContainsProfile(profilesStr, profileName) {
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

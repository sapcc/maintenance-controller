// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/metrics"
	"github.com/sapcc/maintenance-controller/plugin"
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
	Node     string            `json:"node"`
	Profiles []ProfileResult   `json:"profiles"`
	Labels   map[string]string `json:"labels"`
	Updated  time.Time         `json:"updated"`
}

// PluginChains is a struct containing a plugin chain of each plugin type.
type PluginChains struct {
	Enter        plugin.TriggerChain
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

type ProfileData struct {
	Transition time.Time
	Current    NodeStateLabel
	Previous   NodeStateLabel
}

type DataV2 struct {
	Profiles map[string]*ProfileData
	// Maps a notification instance name to the last time it was triggered.
	Notifications map[string]time.Time
}

func ParseData(dataStr string) (Data, error) {
	// dataStr := node.Annotations[constants.DataAnnotationKey]
	var data Data
	if dataStr != "" {
		decoder := json.NewDecoder(strings.NewReader(dataStr))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&data)
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

func ParseDataV2(dataStr string) (DataV2, error) {
	// dataStr := node.Annotations[constants.DataAnnotationKey]
	var data DataV2
	if dataStr != "" {
		decoder := json.NewDecoder(strings.NewReader(dataStr))
		decoder.DisallowUnknownFields()
		err := decoder.Decode(&data)
		if err != nil {
			return DataV2{}, fmt.Errorf("failed to parse json value in data annotation: %w", err)
		}
	}
	if data.Notifications == nil {
		data.Notifications = make(map[string]time.Time)
	}
	return data, nil
}

func ParseMigrateDataV2(dataStr string, log logr.Logger) (DataV2, error) {
	// dataStr := node.Annotations[constants.DataAnnotationKey]
	if dataStr == "" {
		return DataV2{}, nil
	}
	data2, err := ParseDataV2(dataStr)
	if err == nil {
		return data2, nil
	}
	log.Info("failed to parse annotation as data v1, will try to migrate", "err", err)
	data, err := ParseData(dataStr)
	if err != nil {
		return DataV2{}, err
	}
	data2 = DataV2{
		Profiles:      make(map[string]*ProfileData),
		Notifications: data.LastNotificationTimes,
	}
	for profile, current := range data.ProfileStates {
		previous, ok := data.PreviousStates[profile]
		if !ok {
			previous = current
		}
		data2.Profiles[profile] = &ProfileData{
			Transition: data.LastTransition,
			Current:    current,
			Previous:   previous,
		}
	}
	return data2, nil
}

// NodeState represents the state a node can be in.
type NodeState interface {
	// Label is the Label associated with the state
	Label() NodeStateLabel
	// Enter is executed when a node enters a new state.
	// Its not executed when a profile gets freshly attached.
	Enter(params plugin.Parameters, data *DataV2) error
	// Notify executes the notification chain if required
	Notify(params plugin.Parameters, data *DataV2) error
	// Trigger executes the trigger chain
	Trigger(params plugin.Parameters, next NodeStateLabel, data *DataV2) error
	// Trigger executes the check chain and determines, which state should be the next one.
	// If an error is returned the NodeStateLabel must match the current state.
	Transition(params plugin.Parameters, data *DataV2) (TransitionsResult, error)
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
func Apply(state NodeState, node *v1.Node, data *DataV2, params plugin.Parameters) (ApplyResult, error) {
	recorder := params.Recorder
	result := ApplyResult{Next: state.Label(), Transitions: []TransitionResult{}}

	handleTransitionError := func(err error, prefix string) (ApplyResult, error) {
		metrics.RecordTransitionFailure(params.Profile)
		params.Log.Error(
			err, prefix,
			"state", params.State,
			"profile", params.Profile,
			"node", node.Name,
		)
		recorder.Eventf(
			node, "Normal", "ChangeMaintenanceStateFailed",
			"%v for profile %v: Will stay in %v state",
			prefix, params.Profile, params.State,
		)
		result.Error = err.Error()
		return result, fmt.Errorf("%v for profile %v: %w", strings.ToLower(prefix), params.Profile, err)
	}

	stateInfo, ok := data.Profiles[params.Profile]
	if !ok {
		err := fmt.Errorf("could not find profile '%s' in state data", params.Profile)
		result.Error = err.Error()
		return result, err
	}
	if stateInfo.Previous != stateInfo.Current {
		if err := state.Enter(params, data); err != nil {
			return handleTransitionError(err, fmt.Sprintf("Failed to enter state %s", state.Label()))
		}
	}
	// invoke notifications and check for transition
	err := state.Notify(params, data)
	if err != nil {
		return handleTransitionError(err, "At least one notification plugin failed")
	}
	transitions, err := state.Transition(params, data)
	result.Transitions = transitions.Infos
	if err != nil {
		return handleTransitionError(err, "At least one check plugin failed")
	}

	// check if a transition should happen
	if transitions.Next != state.Label() {
		err = state.Trigger(params, transitions.Next, data)
		if err != nil {
			return handleTransitionError(err, "At least one trigger plugin failed")
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
		return final, fmt.Errorf("had failed transition checks: %w", errors.Join(errs...))
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
func notifyDefault(params plugin.Parameters, data *DataV2, chain *plugin.NotificationChain) error {
	for _, notifyInstance := range chain.Plugins {
		if notifyInstance.Schedule == nil {
			return fmt.Errorf("notification plugin instance %s has no schedule assigned", notifyInstance.Name)
		}
		_, ok := data.Notifications[notifyInstance.Name]
		if !ok {
			data.Notifications[notifyInstance.Name] = time.Time{}
		}
		now := time.Now().UTC()
		currentState, ok := data.Profiles[params.Profile]
		if !ok {
			return fmt.Errorf(
				"cannot determine state of profile %s because state data does not contain information regarding that profile",
				params.Profile,
			)
		}
		if currentState == nil {
			return fmt.Errorf(
				"cannot determine state of profile %s because state data for it is nil",
				params.Profile,
			)
		}
		shouldNotify := notifyInstance.Schedule.ShouldNotify(plugin.ShouldNotifyParams{
			Current: plugin.NotificationData{
				State: string(currentState.Current),
				Time:  now,
			},
			Last: plugin.NotificationData{
				State: string(currentState.Previous),
				Time:  data.Notifications[notifyInstance.Name],
			},
			Log: plugin.SchedulingLogger{
				Log:        params.Log,
				LogDetails: params.LogDetails,
			},
			StateChange: currentState.Transition,
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
		data.Notifications[notifyInstance.Name] = now
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
func (d *DataV2) GetProfilesWithState(profilesStr string, availableProfiles map[string]Profile) []ProfileState {
	// if no profile is attached, use the default profile
	if profilesStr == "" {
		profilesStr = constants.DefaultProfileName
	}
	result := make([]ProfileState, 0)
	profiles := getProfiles(profilesStr, availableProfiles)
	for _, profile := range profiles {
		state := d.Profiles[profile.Name].Current
		result = append(result, ProfileState{
			Profile: profile,
			State:   state,
		})
	}
	return result
}

// Removes state data for removed profile and initializes it for added profiles.
func (d *DataV2) MaintainProfileStates(profilesStr string, availableProfiles map[string]Profile) {
	if d.Profiles == nil {
		d.Profiles = make(map[string]*ProfileData)
	}
	// if no profile is attached, use the default profile
	if profilesStr == "" {
		profilesStr = constants.DefaultProfileName
	}
	// cleanup unused states
	toRemove := make([]string, 0)
	for profileName := range d.Profiles {
		if !ContainsProfile(profilesStr, profileName) {
			toRemove = append(toRemove, profileName)
		}
	}
	for _, remove := range toRemove {
		delete(d.Profiles, remove)
	}
	// initialize new states
	profiles := getProfiles(profilesStr, availableProfiles)
	for _, profile := range profiles {
		if _, ok := d.Profiles[profile.Name]; !ok {
			d.Profiles[profile.Name] = &ProfileData{
				Transition: time.Now(),
				Current:    Operational,
				Previous:   Operational,
			}
		}
	}
}

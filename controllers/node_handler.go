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

package controllers

import (
	"fmt"
	"time"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/metrics"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
)

type NodeHandler = func(params reconcileParameters, data *state.DataV2) error

var handlers []NodeHandler = []NodeHandler{
	EnsureLabelMap,
	MaintainProfileStates,
	ApplyProfiles,
	UpdateMaintenanceStateLabel,
}

func HandleNode(params reconcileParameters, data *state.DataV2) error {
	for _, handler := range handlers {
		if err := handler(params, data); err != nil {
			return err
		}
	}
	return nil
}

func EnsureLabelMap(params reconcileParameters, data *state.DataV2) error {
	if params.node.Labels == nil {
		params.node.Labels = make(map[string]string)
	}
	return nil
}

// ensure a profile is assigned beforehand.
func MaintainProfileStates(params reconcileParameters, data *state.DataV2) error {
	profilesStr := params.node.Labels[constants.ProfileLabelKey]
	data.MaintainProfileStates(profilesStr, params.config.Profiles)
	return nil
}

// ensure a profile is assigned and profile states have been maintained beforehand.
func ApplyProfiles(params reconcileParameters, data *state.DataV2) error {
	profilesStr := params.node.Labels[constants.ProfileLabelKey]
	profileStates := data.GetProfilesWithState(profilesStr, params.config.Profiles)
	profileResults, errs := make([]state.ProfileResult, 0), make([]error, 0)
	for _, ps := range profileStates {
		err := metrics.TouchShuffles(params.ctx, params.client, params.node, ps.Profile.Name)
		if err != nil {
			params.log.Info("failed to touch shuffle metrics", "profile", ps.Profile.Name, "error", err)
		}
		// construct state
		stateObj, err := state.FromLabel(ps.State, ps.Profile.Chains[ps.State])
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create internal state from unknown label value: %w", err))
			continue
		}

		logDetails := false
		if params.node.Labels[constants.LogDetailsLabelKey] == "true" {
			logDetails = true
		}
		// build plugin arguments
		pluginParams := plugin.Parameters{Client: params.client, Ctx: params.ctx, Log: params.log,
			Profile: ps.Profile.Name, Node: params.node, InMaintenance: anyInMaintenance(profileStates),
			State: string(ps.State), LastTransition: data.Profiles[ps.Profile.Name].Transition,
			Recorder: params.recorder, LogDetails: logDetails}

		applied, err := state.Apply(stateObj, params.node, data, pluginParams)
		profileResults = append(profileResults, state.ProfileResult{
			Applied: applied,
			Name:    ps.Profile.Name,
			State:   stateObj.Label(),
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	params.nodeInfoCache.Update(state.NodeInfo{
		Node:     params.node.Name,
		Profiles: profileResults,
	})
	if len(errs) > 0 {
		return fmt.Errorf("failed to apply current state: %s", common.ConcatErrors(errs))
	}
	for i, ps := range profileStates {
		result := profileResults[i]
		// check if a transition happened
		if ps.State != result.Applied.Next {
			data.Profiles[ps.Profile.Name].Transition = time.Now().UTC()
			data.Profiles[ps.Profile.Name].Current = result.Applied.Next
		}
		// track the state of this reconciliation for the next run
		data.Profiles[ps.Profile.Name].Previous = result.State
	}
	return nil
}

func anyInMaintenance(profileStates []state.ProfileState) bool {
	for _, ps := range profileStates {
		if ps.State == state.InMaintenance {
			return true
		}
	}
	return false
}

func UpdateMaintenanceStateLabel(params reconcileParameters, data *state.DataV2) error {
	profilesStr := params.node.Labels[constants.ProfileLabelKey]
	profileStates := data.GetProfilesWithState(profilesStr, params.config.Profiles)
	if params.node.Labels == nil {
		params.node.Labels = make(map[string]string)
	}
	for _, ps := range profileStates {
		if ps.State == state.InMaintenance {
			params.node.Labels[constants.StateLabelKey] = string(ps.State)
			return nil
		}
	}
	for _, ps := range profileStates {
		if ps.State == state.Required {
			params.node.Labels[constants.StateLabelKey] = string(ps.State)
			return nil
		}
	}
	params.node.Labels[constants.StateLabelKey] = string(state.Operational)
	return nil
}

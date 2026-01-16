// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/metrics"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
)

type NodeHandler = func(ctx context.Context, params reconcileParameters, data *state.Data) error

var handlers []NodeHandler = []NodeHandler{
	EnsureLabelMap,
	MaintainProfileStates,
	ApplyProfiles,
	UpdateMaintenanceStateLabel,
}

func HandleNode(ctx context.Context, params reconcileParameters, data *state.Data) error {
	for _, handler := range handlers {
		if err := handler(ctx, params, data); err != nil {
			return err
		}
	}
	return nil
}

func EnsureLabelMap(ctx context.Context, params reconcileParameters, data *state.Data) error {
	if params.node.Labels == nil {
		params.node.Labels = make(map[string]string)
	}
	return nil
}

// ensure a profile is assigned beforehand.
func MaintainProfileStates(ctx context.Context, params reconcileParameters, data *state.Data) error {
	profilesStr := params.node.Labels[constants.ProfileLabelKey]
	data.MaintainProfileStates(profilesStr, params.config.Profiles)
	return nil
}

// ensure a profile is assigned and profile states have been maintained beforehand.
func ApplyProfiles(ctx context.Context, params reconcileParameters, data *state.Data) error {
	profilesStr := params.node.Labels[constants.ProfileLabelKey]
	profileStates := data.GetProfilesWithState(profilesStr, params.config.Profiles)
	profileResults, errs := make([]state.ProfileResult, 0), make([]error, 0)
	profilesWithRetryError := make(map[string]struct{})

	for _, ps := range profileStates {
		err := metrics.TouchShuffles(ctx, params.client, params.node, ps.Profile.Name)
		if err != nil {
			params.log.Info("failed to touch shuffle metrics", "profile", ps.Profile.Name, "error", err)
		}
		// construct state
		stateObj, err := state.FromLabel(ps.State, ps.Profile.Chains[ps.State])
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create internal state from unknown label value: %w", err))
			continue
		}

		logDetails := params.node.Labels[constants.LogDetailsLabelKey] == "true"
		// build plugin arguments
		pluginParams := plugin.Parameters{Client: params.client, Clientset: params.clientset, Ctx: ctx,
			Log: params.log, Profile: ps.Profile.Name, Node: params.node, InMaintenance: anyInMaintenance(profileStates),
			State: string(ps.State), LastTransition: data.Profiles[ps.Profile.Name].Transition,
			Recorder: params.recorder, LogDetails: logDetails}

		applied, err := state.Apply(stateObj, params.node, data, pluginParams)
		profileResults = append(profileResults, state.ProfileResult{
			Applied: applied,
			Name:    ps.Profile.Name,
			State:   stateObj.Label(),
		})
		var retryErr *plugin.RetryError
		if err != nil && !errors.As(err, &retryErr) {
			errs = append(errs, err)
		} else if err != nil && errors.As(err, &retryErr) {
			profilesWithRetryError[ps.Profile.Name] = struct{}{}
		}
	}
	params.nodeInfoCache.Update(state.NodeInfo{
		Node:     params.node.Name,
		Profiles: profileResults,
		Labels:   filterNodeLabels(params.node.Labels, params.config.DashboardLabelFilter),
	})
	if len(errs) > 0 {
		return fmt.Errorf("failed to apply current state: %w", errors.Join(errs...))
	}
	for i, ps := range profileStates {
		if _, ok := profilesWithRetryError[ps.Profile.Name]; ok {
			continue
		}
		result := profileResults[i]
		// check if a transition happened
		if ps.State != result.Applied.Next {
			data.Profiles[ps.Profile.Name].Transition = time.Now().UTC()
			data.Profiles[ps.Profile.Name].Current = result.Applied.Next
		}
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

func filterNodeLabels(nodeLabels map[string]string, keys []string) map[string]string {
	result := make(map[string]string)
	for _, key := range keys {
		val, ok := nodeLabels[key]
		if ok {
			result[key] = val
		}
	}
	return result
}

func UpdateMaintenanceStateLabel(ctx context.Context, params reconcileParameters, data *state.Data) error {
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

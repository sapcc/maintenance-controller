// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/common"
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
	SaveDrainedPods,
	CheckDrainProgress,
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
		if err != nil {
			errs = append(errs, err)
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

// SaveDrainedPods saves the pods currently on the node to the drain state after a drain trigger has been executed.
// This handler stores the pods so they can be tracked for completion in subsequent reconcile loops.
func SaveDrainedPods(ctx context.Context, params reconcileParameters, data *state.Data) error {
	profilesStr := params.node.Labels[constants.ProfileLabelKey]
	profileStates := data.GetProfilesWithState(profilesStr, params.config.Profiles)

	for _, ps := range profileStates {
		profileName := ps.Profile.Name
		// Only track pods for profiles that are or were recently in maintenance
		if ps.State != state.InMaintenance {
			continue
		}

		// Check if we already have pods tracked for this profile
		_, alreadyTracking := data.Drain.Pods[profileName]
		if alreadyTracking {
			// Already tracking, don't overwrite
			continue
		}

		// Get all pods for this node
		var podList corev1.PodList
		err := params.client.List(ctx, &podList, client.MatchingFields{"spec.nodeName": params.node.Name})
		if err != nil {
			params.log.Error(err, "failed to list pods for drain tracking", "profile", profileName)
			continue
		}

		// Filter pods similar to GetPodsForDrain
		filteredPods := make([]corev1.Pod, 0)
		for _, pod := range podList.Items {
			// skip mirror pods
			if _, ok := pod.Annotations[corev1.MirrorPodAnnotationKey]; ok {
				continue
			}
			// skip daemonsets
			skip := false
			for _, ref := range pod.OwnerReferences {
				if ref.Kind == "DaemonSet" {
					skip = true
				}
			}
			if skip {
				continue
			}
			filteredPods = append(filteredPods, pod)
		}

		// Save the pods to track
		if len(filteredPods) > 0 {
			data.Drain.Pods[profileName] = convertPodsToReferences(filteredPods)
			data.Drain.InitiatedAt[profileName] = time.Now().UTC()
			params.log.Info("Saved pods for drain tracking", "profile", profileName, "podCount", len(filteredPods))
		}
	}

	return nil
}

// CheckDrainProgress monitors ongoing drain operations and retries eviction/deletion
// for pods that are still present. This handler runs in every reconcile loop.
func CheckDrainProgress(ctx context.Context, params reconcileParameters, data *state.Data) error {
	profilesStr := params.node.Labels[constants.ProfileLabelKey]
	profileStates := data.GetProfilesWithState(profilesStr, params.config.Profiles)

	for _, ps := range profileStates {
		profileName := ps.Profile.Name
		pods, hasPendingPods := data.Drain.Pods[profileName]
		if !hasPendingPods || len(pods) == 0 {
			continue
		}

		// Check if any of the tracked pods are still present
		stillPending := make([]corev1.Pod, 0)
		for _, podRef := range pods {
			var pod corev1.Pod
			err := params.client.Get(ctx, client.ObjectKey{Namespace: podRef.Namespace, Name: podRef.Name}, &pod)
			if err != nil {
				if !k8serrors.IsNotFound(err) {
					params.log.Error(err, "failed to check pod status during drain", "pod", podRef.Name, "namespace", podRef.Namespace)
				}
				// Pod is gone or we can't check, skip
				continue
			}
			// Pod still exists
			stillPending = append(stillPending, pod)
		}

		// If there are still pods, retry the drain
		if len(stillPending) > 0 {
			params.log.Info("Retrying drain for still-pending pods", "profile", profileName, "podCount", len(stillPending), "node", params.node.Name)
			hasPendingAfterRetry, err := common.EnsureDrain(ctx, params.node, params.log, common.DrainParameters{
				AwaitDeletion: common.WaitParameters{
					Period:  common.DefaultDrainRetryPeriod,
					Timeout: 30 * time.Second,
				},
				Eviction: common.WaitParameters{
					Period:  common.DefaultDrainRetryPeriod,
					Timeout: 10 * time.Second,
				},
				Client:        params.client,
				Clientset:     params.clientset,
				ForceEviction: true,
			})
			if err != nil {
				params.log.Error(err, "failed to retry drain", "profile", profileName)
			}
			if hasPendingAfterRetry {
				// Update the tracked pods
				data.Drain.Pods[profileName] = convertPodsToReferences(stillPending)
			} else {
				// All pods are gone, clean up the drain state
				delete(data.Drain.Pods, profileName)
				delete(data.Drain.InitiatedAt, profileName)
			}
		} else {
			// All tracked pods are gone
			delete(data.Drain.Pods, profileName)
			delete(data.Drain.InitiatedAt, profileName)
		}
	}

	return nil
}

func convertPodsToReferences(pods []corev1.Pod) []state.PodReference {
	refs := make([]state.PodReference, len(pods))
	for i, pod := range pods {
		refs[i] = state.PodReference{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		}
	}
	return refs
}

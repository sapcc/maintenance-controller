// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"fmt"

	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
)

type nodeStateMap = map[string]*state.ProfileData

// Affinity does not pass if a node has at least one pod, which should be scheduled on operational nodes
// and other nodes are in maintenance-required, which do not have such a pod.
type Affinity struct {
	MinOperational int
}

// New creates a new Affinity instance with the given config.
func (a *Affinity) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	if config == nil {
		return &Affinity{}, nil
	}
	conf := struct {
		MinOperational int `config:"minOperational"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &Affinity{MinOperational: conf.MinOperational}, nil
}

func (a *Affinity) ID() string {
	return "affinity"
}

func (a *Affinity) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	if params.State != string(state.Required) {
		err := fmt.Errorf("affinity check plugin failed, node %v is not in maintenance-required but %v state",
			params.Node.Name, params.State)
		return plugin.Failed(nil), err
	}
	nodeStates, err := buildNodeStates(&params)
	if err != nil {
		return plugin.Failed(nil), err
	}
	if a.MinOperational > 0 {
		operationalCount, err := countOperational(&params, nodeStates)
		if err != nil {
			return plugin.Failed(nil), err
		}
		if operationalCount >= a.MinOperational {
			return plugin.PassedWithReason("minOperational exceeded"), nil
		}
	}
	currentAffinity, err := hasAffinityPod(params.Node.Name, &params)
	if err != nil {
		return plugin.Failed(nil), fmt.Errorf("failed to check if node %v has affinity pods: %w", params.Node.Name, err)
	}
	// current node does not have any relevant pods, so pass
	if !currentAffinity {
		return plugin.PassedWithReason("no pods with affinity"), nil
	}
	return checkOther(&params, nodeStates)
}

func buildNodeStates(params *plugin.Parameters) (nodeStateMap, error) {
	var nodes v1.NodeList
	if err := params.Client.List(params.Ctx, &nodes); err != nil {
		return nil, err
	}
	nodeStates := make(nodeStateMap)
	for i := range nodes.Items {
		node := &nodes.Items[i]
		dataStr := node.Annotations[constants.DataAnnotationKey]
		nodeData, err := state.ParseMigrateDataV2(dataStr, params.Log)
		if err != nil {
			params.Log.Error(err, "failed to parse node data")
			continue
		}
		// skip nodes, which don't have the profile
		otherState, ok := nodeData.Profiles[params.Profile]
		if !ok || otherState == nil {
			continue
		}
		nodeStates[node.Name] = otherState
	}
	return nodeStates, nil
}

func checkOther(params *plugin.Parameters, nodeStates nodeStateMap) (plugin.CheckResult, error) {
	var nodeList v1.NodeList
	err := params.Client.List(params.Ctx, &nodeList)
	if err != nil {
		return plugin.Failed(nil), fmt.Errorf("failed to list nodes in the cluster: %w", err)
	}
	for i := range nodeList.Items {
		node := &nodeList.Items[i]
		// skip self
		if node.Name == params.Node.Name {
			continue
		}
		// only consider nodes, when the transition into maintenance-required has been caused
		// by the same profile being checked right now.
		// Doing otherwise could cause unnecessary block due to nodes being in maintenance-required
		// caused by other profiles without affinity pods.
		// Accessing nodeStates implicitly skips nodes, which don't have the profile.
		// Also skip nodes, that are not in maintenance-required.
		otherState, ok := nodeStates[node.Name]
		if !ok || otherState.Current != state.Required {
			continue
		}
		// some other node in the cluster does not have any relevant pods, so block
		nodeAffinity, err := hasAffinityPod(node.Name, params)
		if err != nil {
			return plugin.Failed(nil), fmt.Errorf("failed to check if node %v has affinity pods: %w", params.Node.Name, err)
		}
		if !nodeAffinity {
			return plugin.FailedWithReason("pods without affinity on: " + node.Name), nil
		}
	}
	// all other nodes have relevant pods as well, so pass
	return plugin.Passed(nil), nil
}

func hasAffinityPod(nodeName string, params *plugin.Parameters) (bool, error) {
	var podList v1.PodList
	err := params.Client.List(params.Ctx, &podList, client.MatchingFields{"spec.nodeName": nodeName})
	if err != nil {
		return false, err
	}
	for i := range podList.Items {
		if hasOperationalAffinity(&podList.Items[i]) {
			return true, nil
		}
	}
	return false, nil
}

func hasOperationalAffinity(pod *v1.Pod) bool {
	if pod.Spec.Affinity == nil || pod.Spec.Affinity.NodeAffinity == nil {
		return false
	}
	nodeAffinity := pod.Spec.Affinity.NodeAffinity
	for _, preferred := range nodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
		for _, expr := range preferred.Preference.MatchExpressions {
			affinityPod := expr.Key == constants.StateLabelKey &&
				expr.Operator == v1.NodeSelectorOpIn &&
				len(expr.Values) == 1 &&
				expr.Values[0] == "operational"
			if affinityPod {
				return true
			}
		}
	}
	return false
}

func countOperational(params *plugin.Parameters, nodeStates nodeStateMap) (int, error) {
	var nodes v1.NodeList
	if err := params.Client.List(params.Ctx, &nodes); err != nil {
		return 0, err
	}
	var count int
	for _, node := range nodes.Items {
		// accessing nodeStates skips nodes, which don't have the profile
		otherState, ok := nodeStates[node.Name]
		// count the nodes, that have the same profile and are operational
		if ok && otherState.Current == state.Operational {
			count++
		}
	}
	return count, nil
}

func (a *Affinity) OnTransition(params plugin.Parameters) error {
	return nil
}

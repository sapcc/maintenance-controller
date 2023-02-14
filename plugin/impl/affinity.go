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

package impl

import (
	"fmt"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Affinity does not pass if a node has at least one pod, which should be scheduled on operational nodes
// and other nodes are in maintenance-required, which do not have such a pod.
type Affinity struct{}

// New creates a new Slack instance with the given config.
func (a *Affinity) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	return &Affinity{}, nil
}

func (a *Affinity) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	if params.State != string(state.Required) {
		err := fmt.Errorf("affinity check plugin failed, node %v is not in maintenance-required but %v state",
			params.Node.Name, params.State)
		return plugin.Failed(nil), err
	}
	currentAffinity, err := hasAffinityPod(params.Node.Name, &params)
	if err != nil {
		return plugin.Failed(nil), fmt.Errorf("failed to check if node %v has affinity pods: %w", params.Node.Name, err)
	}
	// current node does not have any relevant pods, so pass
	if !currentAffinity {
		return plugin.Passed(nil), nil
	}
	return checkOther(&params)
}

func checkOther(params *plugin.Parameters) (plugin.CheckResult, error) {
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
		// caused by other profiles without affinity pods
		nodeData, err := state.ParseData(node)
		if err != nil {
			return plugin.Failed(nil), err
		}
		// skip nodes, which don't have the profile
		otherState, ok := nodeData.ProfileStates[params.Profile]
		if !ok {
			continue
		}
		// skip nodes, that are not in maintenance-required
		if otherState != state.Required {
			continue
		}
		// some other node in the cluster does not have any relevant pods, so block
		nodeAffinity, err := hasAffinityPod(node.Name, params)
		if err != nil {
			return plugin.Failed(nil), fmt.Errorf("failed to check if node %v has affinity pods: %w", params.Node.Name, err)
		}
		if !nodeAffinity {
			return plugin.Failed(nil), nil
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

func (a *Affinity) OnTransition(params plugin.Parameters) error {
	return nil
}

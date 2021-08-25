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

package esx

import (
	"context"
	"fmt"

	"github.com/elastic/go-ucfg/yaml"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

// ConfigFilePath is the path to the configuration file.
const ConfigFilePath = "config/esx.yaml"

// Label key that holds whether a nodes esx host in maintenance or not.
const MaintenanceLabelKey string = "cloud.sap/esx-in-maintenance"

// Label key that holds the physical ESX host.
const HostLabelKey string = "kubernetes.cloud.sap/host"

// Label key that holds the region and availability zone.
const FailureDomainLabelKey string = "failure-domain.beta.kubernetes.io/zone"

type Runnable struct {
	client.Client
	Log logr.Logger
}

func (r *Runnable) NeedLeaderElection() bool {
	return true
}

func (r *Runnable) Start(ctx context.Context) error {
	// load the configuration
	conf, err := yaml.NewConfigWithFile(ConfigFilePath)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (syntax error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return err
	}
	var configuration Config
	err = conf.Unpack(&configuration)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (semantic error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return err
	}
	wait.JitterUntilWithContext(
		ctx,
		func(ctx context.Context) {
			r.Reconcile(ctx, configuration)
		},
		configuration.Intervals.Period,
		configuration.Intervals.Jitter,
		false,
	)
	return nil
}

func (r *Runnable) Reconcile(ctx context.Context, conf Config) {
	var nodes v1.NodeList
	err := r.Client.List(ctx, &nodes, client.HasLabels{HostLabelKey, FailureDomainLabelKey})
	if err != nil {
		r.Log.Error(err, "Failed to retrieve list of cluster nodes.")
		return
	}
	esxList, err := ParseHostList(nodes.Items)
	if err != nil {
		r.Log.Error(err, "Failed to assign nodes to ESX hosts.")
	}
	for i := range esxList {
		esx := &esxList[i]
		status, err := CheckForMaintenance(ctx, CheckParameters{
			VCenters: &conf.VCenters,
			Host:     esx.HostInfo,
			Log:      r.Log,
		})
		if err != nil {
			r.Log.Error(err, "Failed to check for ESX maintenance.", "host", esx.Name, "availabilityZone", esx.AvailabilityZone)
		}
		err = r.updateNodes(ctx, esx, status)
		if err != nil {
			r.Log.Error(err, "Failed to patch nodes on ESX with its maintenance status",
				"host", esx.Name, "availabilityZone", esx.AvailabilityZone)
		}
	}
}

func (r *Runnable) Check() {

}

type HostInfo struct {
	Name             string
	AvailabilityZone string
}

type Host struct {
	HostInfo
	Nodes []v1.Node
}

func ParseHostList(nodes []v1.Node) ([]Host, error) {
	nodesOnHost := make(map[HostInfo][]v1.Node)
	for i := range nodes {
		node := &nodes[i]
		info, err := parseHostInfo(node)
		if err != nil {
			return nil, err
		}
		if nodesOnHost[info] == nil {
			nodesOnHost[info] = make([]v1.Node, 0)
		}
		nodesOnHost[info] = append(nodesOnHost[info], *node)
	}
	result := make([]Host, 0)
	for info, nodes := range nodesOnHost {
		result = append(result, Host{
			HostInfo: info,
			Nodes:    nodes,
		})
	}
	return result, nil
}

func parseHostInfo(node *v1.Node) (HostInfo, error) {
	name, ok := node.Labels[HostLabelKey]
	if !ok {
		return HostInfo{}, fmt.Errorf("Node %v is missing label %v.", node.Name, HostLabelKey)
	}
	failureDomain, ok := node.Labels[FailureDomainLabelKey]
	if !ok {
		return HostInfo{}, fmt.Errorf("Node %v is missing label %v.", node.Name, FailureDomainLabelKey)
	}
	return HostInfo{
		Name: name,
		// assume the character of the failure domain is the availability zone
		AvailabilityZone: failureDomain[len(failureDomain)-1:],
	}, nil
}

// Updates the ESX maintenance on all nodes belonging to the given ESX host.
func (r *Runnable) updateNodes(ctx context.Context, esx *Host, maintenance Maintenance) error {
	for i := range esx.Nodes {
		oneNode := &esx.Nodes[i]
		if oneNode.Labels == nil {
			oneNode.Labels = make(map[string]string)
		}
		value, ok := oneNode.Labels[MaintenanceLabelKey]
		// If a nodes somehow already has the correct maintenance status skip patching
		if !ok || value != string(maintenance) {
			patchedNode := oneNode.DeepCopy()
			patchedNode.Labels[MaintenanceLabelKey] = string(maintenance)
			err := r.Patch(ctx, patchedNode, client.MergeFrom(oneNode))
			if err != nil {
				return fmt.Errorf("Failed to patch ESX maintenance status for host %v: %w", esx.Name, err)
			}
		}
	}
	return nil
}

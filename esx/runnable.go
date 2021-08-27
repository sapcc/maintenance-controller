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

// Label key that holds whether a node can rebootet if the hosting ESX is set into maintenance.
const RebootOkLabelKey string = "cloud.sap/esx-reboot-ok"

// Annotation key that holds whether this controller started rebooting the node.
const RebootInitiatedAnnotationKey string = "cloud.sap/esx-reboot-initiated"

const TrueString string = "true"

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
		configuration.Intervals.Check.Period,
		configuration.Intervals.Check.Jitter,
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
		err = r.CheckMaintenance(ctx, &conf.VCenters, esx)
		if err != nil {
			r.Log.Error(err, "Failed to update ESX maintenance labels.",
				"esx", esx.Name, "availablityZone", esx.AvailabilityZone)
			continue
		}
		err = r.ShutDown(ctx, &conf.VCenters, esx, &conf)
		if err != nil {
			r.Log.Error(err, "Failed to shutdown nodes on ESX.", "esx", esx.Name, "availablityZone", esx.AvailabilityZone)
			continue
		}
	}
}

func (r *Runnable) CheckMaintenance(ctx context.Context, vCenters *VCenters, esx *Host) error {
	status, err := CheckForMaintenance(ctx, CheckParameters{
		VCenters: vCenters,
		Host:     esx.HostInfo,
		Log:      r.Log,
	})
	if err != nil {
		return err
	}
	err = r.updateLabels(ctx, esx, MaintenanceLabelKey, string(status))
	if err != nil {
		return err
	}
	return nil
}

func (r *Runnable) ShutDown(ctx context.Context, vCenters *VCenters, esx *Host, conf *Config) error {
	// Manage labels and annotations
	if ShouldReboot(esx) {
		err := r.updateAnnotations(ctx, esx, RebootInitiatedAnnotationKey, TrueString)
		if err != nil {
			return err
		}
	}
	// Should reboot checks that all nodes on a certain host can be shut down.
	// So, operate on node level now.
	for j := range esx.Nodes {
		node := &esx.Nodes[j]
		// Cordon
		if !ShouldCordon(node) {
			continue
		}
		err := r.updateSchedulable(ctx, node, false)
		if err != nil {
			r.Log.Error(err, "Failed to cordon node", "node", node.Name)
			continue
		}
		// Drain
		if !ShouldDrain(node) {
			continue
		}
		deletable, err := GetPodsForDeletion(ctx, r.Client, node.Name)
		if err != nil {
			r.Log.Error(err, "Failed to fetch deletable pods.", "node", node.Name)
		}
		r.Log.Info("Going to delete pods from node.", "count", len(deletable), "node", node.Name)
		deleteFailed := false
		for i := range deletable {
			pod := deletable[i]
			err = r.Client.Delete(ctx, &pod, &client.DeleteOptions{})
			if err != nil {
				r.Log.Error(err, "Failed to delete pod from node.", "node", node.Name, "pod", pod.Name)
				deleteFailed = true
			}
		}
		if deleteFailed {
			continue
		}
		r.Log.Info("Awaiting pod deletion.")
		err = WaitForPodDeletions(ctx, deletable, WaitParameters{
			Client:  r.Client,
			Period:  conf.Intervals.PodDeletion.Period,
			Timeout: conf.Intervals.PodDeletion.Timeout,
		})
		if err != nil {
			r.Log.Error(err, "Failed to await pod deletions.", "node", node.Name)
			continue
		}
		// Shutdown VM
		err = ShutdownVM(ctx, vCenters, esx.HostInfo, node.Name)
		if err != nil {
			r.Log.Error(err, "Failed to shutdown node.", "node", node.Name)
		}
	}
	return nil
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

// Updates the given label on all nodes belonging to the given ESX host.
func (r *Runnable) updateLabels(ctx context.Context, esx *Host, key string, value string) error {
	for i := range esx.Nodes {
		oneNode := &esx.Nodes[i]
		if oneNode.Labels == nil {
			oneNode.Labels = make(map[string]string)
		}
		current, ok := oneNode.Labels[key]
		// If a nodes somehow already has the correct label skip patching
		if !ok || value != current {
			patchedNode := oneNode.DeepCopy()
			patchedNode.Labels[key] = value
			err := r.Patch(ctx, patchedNode, client.MergeFrom(oneNode))
			if err != nil {
				return fmt.Errorf("Failed to patch Label for node %v status on host %v: %w", oneNode.Name, esx.Name, err)
			}
		}
	}
	return nil
}

// Updates the given annotation on all nodes belonging to the given ESX host.
func (r *Runnable) updateAnnotations(ctx context.Context, esx *Host, key string, value string) error {
	for i := range esx.Nodes {
		oneNode := &esx.Nodes[i]
		if oneNode.Annotations == nil {
			oneNode.Annotations = make(map[string]string)
		}
		current, ok := oneNode.Annotations[key]
		// If a nodes somehow already has the correct annotation skip patching
		if !ok || value != current {
			patchedNode := oneNode.DeepCopy()
			patchedNode.Annotations[key] = value
			err := r.Patch(ctx, patchedNode, client.MergeFrom(oneNode))
			if err != nil {
				return fmt.Errorf("Failed to patch Annotation for node %v status on host %v: %w", oneNode.Name, esx.Name, err)
			}
		}
	}
	return nil
}

// Updates the Node.Spec.Unscheduable of the given Node.
func (r *Runnable) updateSchedulable(ctx context.Context, node *v1.Node, schedulable bool) error {
	// If node already has the correct value
	if node.Spec.Unschedulable != schedulable {
		return nil
	}
	cloned := node.DeepCopy()
	node.Spec.Unschedulable = !schedulable
	err := r.Patch(ctx, node, client.MergeFrom(cloned))
	if err != nil {
		return fmt.Errorf("Failed to set node %v as (un-)schedulable: %w", node.Name, err)
	}
	return nil
}

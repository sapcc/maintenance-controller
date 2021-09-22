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
	"time"

	"github.com/elastic/go-ucfg/yaml"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
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

func (r *Runnable) loadConfig() (Config, error) {
	yamlConf, err := yaml.NewConfigWithFile(ConfigFilePath)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (syntax error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return Config{}, err
	}
	var conf Config
	err = yamlConf.Unpack(&conf)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (semantic error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return Config{}, err
	}
	return conf, nil
}

func (r *Runnable) Start(ctx context.Context) error {
	conf, err := r.loadConfig()
	if err != nil {
		r.Log.Error(err, "Failed to load configuration")
		return err
	}
	wait.JitterUntilWithContext(
		ctx,
		func(ctx context.Context) {
			r.Reconcile(ctx)
		},
		conf.Intervals.Check.Period,
		conf.Intervals.Check.Jitter,
		false,
	)
	return nil
}

func (r *Runnable) Reconcile(ctx context.Context) {
	conf, err := r.loadConfig()
	if err != nil {
		r.Log.Error(err, "Failed to load configuration")
		return
	}
	var nodes v1.NodeList
	err = r.Client.List(ctx, &nodes, client.HasLabels{HostLabelKey, FailureDomainLabelKey})
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
		r.StartNodes(ctx, &conf.VCenters, esx, &conf)
		err = r.ShutdownNodes(ctx, &conf.VCenters, esx, &conf)
		if err != nil {
			r.Log.Error(err, "Failed to shutdown nodes on ESX.", "esx", esx.Name, "availablityZone", esx.AvailabilityZone)
			continue
		}
	}
	conf.VCenters.ClearCache(ctx)
}

// Checks the maintenance mode of the given ESX and attaches the according Maintenance label.
func (r *Runnable) CheckMaintenance(ctx context.Context, vCenters *VCenters, esx *Host) error {
	status, err := CheckForMaintenance(ctx, CheckParameters{
		VCenters: vCenters,
		Host:     esx.HostInfo,
		Log:      r.Log,
	})
	if err != nil {
		return err
	}
	err = r.ensureLabel(ctx, esx, MaintenanceLabelKey, string(status))
	if err != nil {
		return err
	}
	return nil
}

// Shuts down the nodes on the given ESX, if all nodes are with the RebootAllowed="true" label.
func (r *Runnable) ShutdownNodes(ctx context.Context, vCenters *VCenters, esx *Host, conf *Config) error {
	if ShouldShutdown(esx) {
		err := r.ensureAnnotation(ctx, esx, RebootInitiatedAnnotationKey, TrueString)
		if err != nil {
			return err
		}
	}
	for i := range esx.Nodes {
		node := &esx.Nodes[i]
		init, ok := node.Annotations[RebootInitiatedAnnotationKey]
		if !ok || init != TrueString {
			continue
		}
		err := r.ensureSchedulable(ctx, node, false)
		if err != nil {
			r.Log.Error(err, "Failed to cordon node.", "node", node.Name)
			continue
		}
		err = r.ensureDrain(ctx, node, conf)
		if err != nil {
			r.Log.Error(err, "Failed to drain node.", "node", node.Name)
			continue
		}
		time.Sleep(conf.Intervals.Stagger)
		r.Log.Info("Ensuring VM is shut off. Will shutdown if necessary.", "node", node.Name)
		err = ensureVmOff(ctx, vCenters, esx.HostInfo, node.Name)
		if err != nil {
			r.Log.Error(err, "Failed to shutdown node.", "node", node.Name)
		}
	}
	return nil
}

// Drains Pods from the given node, if required.
func (r *Runnable) ensureDrain(ctx context.Context, node *v1.Node, conf *Config) error {
	deletable, err := GetPodsForDeletion(ctx, r.Client, node.Name)
	if err != nil {
		return fmt.Errorf("Failed to fetch deletable pods: %w", err)
	}
	if len(deletable) == 0 {
		return nil
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
		return fmt.Errorf("Failed to delete at least one pod.")
	}
	r.Log.Info("Awaiting pod deletion.", "period", conf.Intervals.PodDeletion.Period,
		"timeout", conf.Intervals.PodDeletion.Timeout)
	err = WaitForPodDeletions(ctx, deletable, WaitParameters{
		Client:  r.Client,
		Period:  conf.Intervals.PodDeletion.Period,
		Timeout: conf.Intervals.PodDeletion.Timeout,
	})
	if err != nil {
		return fmt.Errorf("Failed to await pod deletions: %w", err)
	}
	return nil
}

// Starts the nodes on the given ESX, if this controller shut them down
// and the underlying ESX is no longer in maintenance.
func (r *Runnable) StartNodes(ctx context.Context, vCenters *VCenters, esx *Host, conf *Config) {
	for i := range esx.Nodes {
		node := &esx.Nodes[i]
		if !ShouldStart(node) {
			continue
		}
		r.Log.Info("Going to start VM", "node", node.Name)
		err := ensureVmOn(ctx, vCenters, esx.HostInfo, node.Name)
		if err != nil {
			r.Log.Error(err, "Failed to start VM.", "node", node.Name)
			continue
		}
		err = r.ensureSchedulable(ctx, node, true)
		if err != nil {
			r.Log.Error(err, "Failed to uncordon node.", "node", node.Name)
			continue
		}
		// ESX Maintenance is finished => delete annotation
		err = r.deleteAnnotation(ctx, node, RebootInitiatedAnnotationKey)
		if err != nil {
			r.Log.Error(err, "Failed to delete annotation.", "node", node.Name, "annotation", RebootInitiatedAnnotationKey)
		}
	}
}

type HostInfo struct {
	Name             string
	AvailabilityZone string
}

type Host struct {
	HostInfo
	Nodes []v1.Node
}

// Assigns nodes to their underlying ESX.
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
func (r *Runnable) ensureLabel(ctx context.Context, esx *Host, key string, value string) error {
	for i := range esx.Nodes {
		oneNode := &esx.Nodes[i]
		if oneNode.Labels == nil {
			oneNode.Labels = make(map[string]string)
		}
		current, ok := oneNode.Labels[key]
		// If the node misses the label or has the wrong value it needs patching.
		if !ok || value != current {
			cloned := oneNode.DeepCopy()
			oneNode.Labels[key] = value
			err := r.Patch(ctx, oneNode, client.MergeFrom(cloned))
			if err != nil {
				return fmt.Errorf("Failed to patch Label for node %v status on host %v: %w", oneNode.Name, esx.Name, err)
			}
		}
	}
	return nil
}

// Updates the given annotation on all nodes belonging to the given ESX host.
func (r *Runnable) ensureAnnotation(ctx context.Context, esx *Host, key string, value string) error {
	for i := range esx.Nodes {
		oneNode := &esx.Nodes[i]
		if oneNode.Annotations == nil {
			oneNode.Annotations = make(map[string]string)
		}
		current, ok := oneNode.Annotations[key]
		// If a nodes somehow already has the correct annotation skip patching
		if !ok || value != current {
			cloned := oneNode.DeepCopy()
			oneNode.Annotations[key] = value
			err := r.Patch(ctx, oneNode, client.MergeFrom(cloned))
			if err != nil {
				return fmt.Errorf("Failed to patch Annotation for node %v status on host %v: %w", oneNode.Name, esx.Name, err)
			}
		}
	}
	return nil
}

// Deletes the given annotation from a node.
func (r *Runnable) deleteAnnotation(ctx context.Context, node *v1.Node, key string) error {
	if node.Annotations == nil {
		return nil
	}
	_, ok := node.Annotations[key]
	if !ok {
		return nil
	}
	cloned := node.DeepCopy()
	delete(node.Annotations, key)
	return r.Patch(ctx, node, client.MergeFrom(cloned))
}

// Updates the Node.Spec.Unschedulable of the given Node.
func (r *Runnable) ensureSchedulable(ctx context.Context, node *v1.Node, schedulable bool) error {
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

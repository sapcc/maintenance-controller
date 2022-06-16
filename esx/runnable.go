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
	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch

type Runnable struct {
	client.Client
	Log  logr.Logger
	Conf *rest.Config
}

func (r *Runnable) NeedLeaderElection() bool {
	return true
}

func (r *Runnable) loadConfig() (Config, error) {
	yamlConf, err := yaml.NewConfigWithFile(constants.EsxConfigFilePath)
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
		r.Reconcile,
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
	err = r.Client.List(ctx, &nodes, client.HasLabels{constants.HostLabelKey, constants.FailureDomainLabelKey})
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
		err = r.FetchVersion(ctx, &conf.VCenters, esx)
		if err != nil {
			r.Log.Error(err, "Failed to update ESX version labels.",
				"esx", esx.Name, "availablityZone", esx.AvailabilityZone)
		}
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
	return r.ensureLabel(ctx, esx, constants.EsxMaintenanceLabelKey, string(status))
}

func (r *Runnable) FetchVersion(ctx context.Context, vCenters *VCenters, esx *Host) error {
	version, err := FetchVersion(ctx, CheckParameters{
		VCenters: vCenters,
		Host:     esx.HostInfo,
		Log:      r.Log,
	})
	if err != nil {
		return err
	}
	return r.ensureLabel(ctx, esx, constants.EsxVersionLabelKey, version)
}

// Shuts down the nodes on the given ESX, if all nodes are with the RebootAllowed="true" label.
func (r *Runnable) ShutdownNodes(ctx context.Context, vCenters *VCenters, esx *Host, conf *Config) error {
	if ShouldShutdown(esx) {
		err := r.ensureAnnotation(ctx, esx, constants.EsxRebootInitiatedAnnotationKey, constants.TrueStr)
		if err != nil {
			return err
		}
	}
	for i := range esx.Nodes {
		node := &esx.Nodes[i]
		init, ok := node.Annotations[constants.EsxRebootInitiatedAnnotationKey]
		if !ok || init != constants.TrueStr {
			continue
		}
		err := common.EnsureSchedulable(ctx, r.Client, node, false)
		if err != nil {
			r.Log.Error(err, "Failed to cordon node.", "node", node.Name)
			continue
		}
		err = common.EnsureDrain(ctx, node, r.Log,
			common.DrainParameters{
				Client:    r.Client,
				Clientset: kubernetes.NewForConfigOrDie(r.Conf),
				AwaitDeletion: common.WaitParameters{
					Period:  conf.Intervals.PodDeletion.Period,
					Timeout: conf.Intervals.PodDeletion.Timeout,
				},
				Eviction: common.WaitParameters{
					Period:  conf.Intervals.PodDeletion.Period,
					Timeout: conf.Intervals.PodDeletion.Timeout,
				},
			},
		)
		if err != nil {
			r.Log.Error(err, "Failed to drain node.", "node", node.Name)
			continue
		}
		time.Sleep(conf.Intervals.Stagger)
		r.Log.Info("Ensuring VM is shut off. Will shutdown if necessary.", "node", node.Name)
		err = ensureVMOff(ctx, vCenters, esx.HostInfo, node.Name)
		if err != nil {
			r.Log.Error(err, "Failed to shutdown node.", "node", node.Name)
		}
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
		err := ensureVMOn(ctx, vCenters, esx.HostInfo, node.Name)
		if err != nil {
			r.Log.Error(err, "Failed to start VM.", "node", node.Name)
			continue
		}
		err = common.EnsureSchedulable(ctx, r.Client, node, true)
		if err != nil {
			r.Log.Error(err, "Failed to uncordon node.", "node", node.Name)
			continue
		}
		// ESX Maintenance is finished => delete annotation
		err = r.deleteAnnotation(ctx, node, constants.EsxRebootInitiatedAnnotationKey)
		if err != nil {
			r.Log.Error(err, "Failed to delete annotation.", "node", node.Name,
				"annotation", constants.EsxRebootInitiatedAnnotationKey)
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
	name, ok := node.Labels[constants.HostLabelKey]
	if !ok {
		return HostInfo{}, fmt.Errorf("node %v is missing label %v", node.Name, constants.HostLabelKey)
	}
	failureDomain, ok := node.Labels[constants.FailureDomainLabelKey]
	if !ok {
		return HostInfo{}, fmt.Errorf("node %v is missing label %v", node.Name, constants.FailureDomainLabelKey)
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
	if _, ok := node.Annotations[key]; !ok {
		return nil
	}
	cloned := node.DeepCopy()
	delete(node.Annotations, key)
	return r.Patch(ctx, node, client.MergeFrom(cloned))
}

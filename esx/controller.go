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
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
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

type NodeReconciler struct {
	client.Client
	Log        logr.Logger
	Scheme     *runtime.Scheme
	Recorder   record.EventRecorder
	Timestamps Timestamps
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// load the configuration
	conf, err := yaml.NewConfigWithFile(ConfigFilePath)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (syntax error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return ctrl.Result{}, nil
	}
	var configuration Config
	err = conf.Unpack(&configuration)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (semantic error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return ctrl.Result{}, nil
	}
	r.Timestamps.Interval = configuration.Intervals.ESX

	// fetch the current node from the api server
	var theNode corev1.Node
	err = r.Get(ctx, req.NamespacedName, &theNode)
	if err != nil {
		r.Log.Error(err, "Failed to retrieve node information from the API server", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: configuration.Intervals.Node}, nil
	}

	// do stuff with the node
	esxHost, err := parseHost(&theNode)
	if err != nil {
		r.Log.Error(err, "Failed to determine ESX host", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: configuration.Intervals.Node}, nil
	}
	result, err := CheckForMaintenance(ctx, CheckParameters{&configuration.VCenters, &r.Timestamps, esxHost, r.Log})
	if err != nil {
		r.Log.Error(err, "Failed to check for ESX host maintenance", "node", req.NamespacedName)
	}
	if result != NotRequired {
		err = r.updateNodesOnESX(ctx, esxHost.Name, result)
		if err != nil {
			r.Log.Error(err, "Failed to update a node with a new ESX maintenance status", "node", req.NamespacedName)
			return ctrl.Result{RequeueAfter: configuration.Intervals.Node}, nil
		}
	}

	// ensure the controller reconciles again as an ESX host in maintenance may kill kubelets
	// which in turn cause most node reconciliation
	return ctrl.Result{RequeueAfter: configuration.Intervals.Node}, nil
}

// Parses HostLabelKey and FailureDomainLabelKey to create a check.Host object.
func parseHost(node *v1.Node) (Host, error) {
	name, ok := node.Labels[HostLabelKey]
	if !ok {
		return Host{}, fmt.Errorf("Node %v is missing label %v.", node.Name, HostLabelKey)
	}
	failureDomain, ok := node.Labels[FailureDomainLabelKey]
	if !ok {
		return Host{}, fmt.Errorf("Node %v is missing label %v.", node.Name, FailureDomainLabelKey)
	}
	return Host{
		Name: name,
		// assume the character of the failure domain is the availability zone
		AvailabilityZone: failureDomain[len(failureDomain)-1:],
	}, nil
}

// Updates the ESX maintenance on all nodes belonging to the given ESX host.
func (r *NodeReconciler) updateNodesOnESX(ctx context.Context, esxHost string, maintenance ESXMaintenance) error {
	// Get relevant nodes
	var nodeList v1.NodeList
	err := r.Client.List(ctx, &nodeList, client.MatchingLabels{HostLabelKey: esxHost})
	if err != nil {
		return fmt.Errorf("Failed to fetch on ESX host %v for updating its maintenance status: %w", esxHost, err)
	}

	for i := range nodeList.Items {
		oneNode := &nodeList.Items[i]
		if oneNode.Labels == nil {
			oneNode.Labels = make(map[string]string)
		}
		value, ok := oneNode.Labels[MaintenanceLabelKey]
		// If a nodes somehow already has the correct maintenance status skip patching
		if !ok || value != string(maintenance) {
			patchedNode := oneNode.DeepCopy()
			patchedNode.Labels[MaintenanceLabelKey] = string(maintenance)
			err = r.Patch(ctx, patchedNode, client.MergeFrom(oneNode))
			if err != nil {
				return fmt.Errorf("Failed to patch ESX maintenance status for host %v: %w", esxHost, err)
			}
		}
	}
	return nil
}

// SetupWithManager attaches the controller to the given manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}

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
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/elastic/go-ucfg/yaml"
	"github.com/go-logr/logr"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/metrics"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NodeReconciler reconciles a Node object.
type NodeReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

type reconcileParameters struct {
	client   client.Client
	config   *Config
	ctx      context.Context
	log      logr.Logger
	recorder record.EventRecorder
	node     *corev1.Node
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles the given request.
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// load the configuration
	conf, err := yaml.NewConfigWithFile(constants.MaintenanceConfigFilePath)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (syntax error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return ctrl.Result{}, nil
	}
	config, err := LoadConfig(conf)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (semantic error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return ctrl.Result{}, nil
	}

	// fetch the current node from the api server
	var theNode corev1.Node
	err = r.Get(ctx, req.NamespacedName, &theNode)
	if errors.IsNotFound(err) {
		r.Log.Info("Could not find node on the API server, maybe it has been deleted?", "node", req.NamespacedName)
		return ctrl.Result{}, nil
	} else if err != nil {
		r.Log.Error(err, "Failed to retrieve node information from the API server", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
	}
	unmodifiedNode := theNode.DeepCopy()

	// perform the reconciliation
	err = reconcileInternal(reconcileParameters{
		client:   r.Client,
		config:   config,
		ctx:      ctx,
		log:      r.Log.WithValues("node", req.NamespacedName),
		node:     &theNode,
		recorder: r.Recorder,
	})
	if err != nil {
		r.Log.Error(err, "Failed to reconcile. Skipping node patching.", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
	}

	// if the controller did not change anything, there is no need to patch
	if equality.Semantic.DeepEqual(&theNode, unmodifiedNode) {
		return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
	}

	// patch node
	err = r.Patch(ctx, &theNode, client.MergeFrom(unmodifiedNode))
	if err != nil {
		r.Log.Error(err, "Failed to patch node on the API server", "node", req.NamespacedName)
	}

	// await cache update
	err = pollCacheUpdate(ctx, r.Client, types.NamespacedName{
		Name:      theNode.Name,
		Namespace: theNode.Namespace,
	}, theNode.ResourceVersion)
	if err != nil {
		r.Log.Error(err, "Failed to poll for cache update")
	}
	return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
}

func pollCacheUpdate(ctx context.Context, client client.Client, ref types.NamespacedName, targetVersion string) error {
	return wait.PollImmediate(20*time.Millisecond, 1*time.Second, func() (done bool, err error) { //nolint:gomnd
		var nextNode corev1.Node
		if err = client.Get(ctx, ref, &nextNode); err != nil {
			return false, err
		}
		nextVersion, err := strconv.Atoi(nextNode.ResourceVersion)
		if err != nil {
			return false, err
		}
		currentVersion, err := strconv.Atoi(targetVersion)
		if err != nil {
			return false, err
		}
		return nextVersion >= currentVersion, nil
	})
}

// Implements the reconciliation logic.
func reconcileInternal(params reconcileParameters) error {
	node := params.node
	log := params.log

	// fetch the current node state
	data, err := state.ParseData(node)
	if err != nil {
		return err
	}
	profilesStr, ok := node.Labels[constants.ProfileLabelKey]
	if !ok {
		profilesStr = constants.DefaultProfileName
	}

	profileStates := data.GetProfilesWithState(profilesStr, params.config.Profiles)
	for _, ps := range profileStates {
		err = metrics.TouchShuffles(params.ctx, params.client, params.node, ps.Profile.Name)
		if err != nil {
			params.log.Info("failed to touch shuffle metrics", "profile", ps.Profile.Name, "error", err)
		}
		// construct state
		stateObj, err := state.FromLabel(ps.State, ps.Profile.Chains[ps.State])
		if err != nil {
			return fmt.Errorf("failed to create internal state from unknown label value: %w", err)
		}

		// build plugin arguments
		pluginParams := plugin.Parameters{Client: params.client, Ctx: params.ctx, Log: log,
			Profile: ps.Profile.Name, Node: node, InMaintenance: anyInMaintenance(profileStates),
			State: string(ps.State), LastTransition: data.LastTransition, Recorder: params.recorder}

		next, err := state.Apply(stateObj, node, &data, pluginParams)
		if err != nil {
			return fmt.Errorf("Failed to apply current state: %w", err)
		}
		// check if a transition happened
		if ps.State != next {
			node.Labels[constants.StateLabelKey] = string(next)
			data.LastTransition = time.Now().UTC()
			data.ProfileStates[ps.Profile.Name] = next
		}
	}

	updateMaintenanceStateLabel(params.node, profileStates)
	// update data annotation
	return writeData(node, data)
}

func anyInMaintenance(profileStates []state.ProfileState) bool {
	for _, ps := range profileStates {
		if ps.State == state.InMaintenance {
			return true
		}
	}
	return false
}

func updateMaintenanceStateLabel(node *corev1.Node, profileStates []state.ProfileState) {
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	for _, ps := range profileStates {
		if ps.State == state.InMaintenance {
			node.Labels[constants.StateLabelKey] = string(ps.State)
			return
		}
	}
	for _, ps := range profileStates {
		if ps.State == state.Required {
			node.Labels[constants.StateLabelKey] = string(ps.State)
			return
		}
	}
	node.Labels[constants.StateLabelKey] = string(state.Operational)
}

func writeData(node *corev1.Node, data state.Data) error {
	dataBytes, err := json.Marshal(&data)
	if err != nil {
		return fmt.Errorf("failed to marshal internal data: %w", err)
	}
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[constants.DataAnnotationKey] = string(dataBytes)
	return nil
}

// SetupWithManager attaches the controller to the given manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}

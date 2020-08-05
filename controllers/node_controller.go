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
	"time"

	"github.com/elastic/go-ucfg/yaml"
	"github.com/go-logr/logr"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultProfileName is the name of the default maintenance profile.
const DefaultProfileName = "default"

// ConfigFilePath is the path to the configuration file.
const ConfigFilePath = "./config/maintenance.yaml"

// StateLabelKey is the full label key, which the controller attaches the node state information to.
const StateLabelKey = "cloud.sap/maintenance-state"

// ProfileLabelKey is the full label key, where the user can attach profile information to a node.
const ProfileLabelKey = "cloud.sap/maintenance-profile"

// DataAnnotationKey is the full annotation key, to which the controller serializes internal data.
const DataAnnotationKey = "cloud.sap/maintenance-data"

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

// Reconcile reconciles the given request.
func (r *NodeReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	globalContext := context.Background()

	// load the configuration
	conf, err := yaml.NewConfigWithFile(ConfigFilePath)
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
	err = r.Get(globalContext, req.NamespacedName, &theNode)
	if err != nil {
		r.Log.Error(err, "Failed to retrieve node information from the API server", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
	}
	unmodifiedNode := theNode.DeepCopy()

	// perform the reconciliation
	err = reconcileInternal(reconcileParameters{
		client:   r.Client,
		config:   config,
		ctx:      globalContext,
		log:      r.Log.WithValues("node", req.NamespacedName),
		node:     &theNode,
		recorder: r.Recorder,
	})
	if err != nil {
		r.Log.Error(err, "Failed to reconcile. Skipping node patching.", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
	}

	// patch node
	err = r.Patch(globalContext, &theNode, client.MergeFrom(unmodifiedNode))
	if err != nil {
		r.Log.Error(err, "Failed to patch node on the API server", "node", req.NamespacedName)
	}
	return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
}

func reconcileInternal(params reconcileParameters) error {
	node := params.node
	log := params.log

	// fetch the current node state
	stateLabel := parseNodeState(node, StateLabelKey)
	stateStr := string(stateLabel)

	// get the current profile
	profile, err := getProfile(node, params.config)
	if err != nil {
		return err
	}

	// construct state
	stateObj, err := state.FromLabel(stateLabel, profile.Chains[stateLabel], params.config.NotificationInterval)
	if err != nil {
		return fmt.Errorf("failed to create internal state from unknown label value: %w", err)
	}

	// build plugin arguments
	data, err := parseData(node)
	if err != nil {
		return err
	}
	pluginParams := plugin.Parameters{Client: params.client, Ctx: params.ctx, Log: log, Node: node,
		State: stateStr, StateKey: StateLabelKey, LastTransition: data.LastTransition}

	// invoke notifications and check for transition
	err = stateObj.Notify(pluginParams, &data)
	if err != nil {
		params.recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
			"At least one notification plugin failed: Will stay in %v state", stateStr)
		return fmt.Errorf("failed to notify: %w", err)
	}
	next, err := stateObj.Transition(pluginParams, &data)
	if err != nil {
		params.recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
			"At least one check plugin failed: Will stay in %v state", stateStr)
		log.Error(err, "Failed to check for state transition", "state", stateStr)
	}

	// check if a transition should happen
	if next != stateLabel {
		err = stateObj.Trigger(pluginParams, &data)
		if err != nil {
			log.Error(err, "Failed to execute triggers", "state", stateStr)
			params.recorder.Eventf(node, "Normal", "ChangeMaintenanceStateFailed",
				"At least one trigger plugin failed: Will stay in %v state", stateStr)
		} else {
			node.Labels[StateLabelKey] = string(next)
			data.LastTransition = time.Now().UTC()
			log.Info("Moved node to next state", "state", string(next))
			params.recorder.Eventf(node, "Normal", "ChangedMaintenanceState", "The node is now in the %v state", string(next))
		}
	}

	// update data annotation
	return writeData(node, data)
}

func parseData(node *corev1.Node) (state.Data, error) {
	dataStr := node.Annotations[DataAnnotationKey]
	var data state.Data
	if dataStr != "" {
		err := json.Unmarshal([]byte(dataStr), &data)
		if err != nil {
			return state.Data{}, fmt.Errorf("failed to parse json value in data annotation: %w", err)
		}
	}
	return data, nil
}

func writeData(node *corev1.Node, data state.Data) error {
	dataBytes, err := json.Marshal(&data)
	if err != nil {
		return fmt.Errorf("failed to marshal internal data: %w", err)
	}
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[DataAnnotationKey] = string(dataBytes)
	return nil
}

func getProfile(node *corev1.Node, config *Config) (state.Profile, error) {
	profileStr, ok := node.Labels[ProfileLabelKey]
	if !ok {
		profileStr = DefaultProfileName
	}
	profile, ok := config.Profiles[profileStr]
	if !ok {
		return state.Profile{}, fmt.Errorf("cannot find the requested maintenance profile %v", profileStr)
	}
	return profile, nil
}

func parseNodeState(node *corev1.Node, key string) state.NodeStateLabel {
	// get the current node state
	stateStr, ok := node.Labels[key]
	if ok {
		return state.NodeStateLabel(stateStr)
	}
	// if not found attach operational state
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels[key] = string(state.Operational)
	return state.Operational
}

// SetupWithManager attaches the controller to the given manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}

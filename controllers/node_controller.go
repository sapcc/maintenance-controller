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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CheckPluginSuffix is the check plugin configuration suffix for node annotations
const CheckPluginSuffix = "check"

// NotificationPluginSuffix is the notification plugin configuration suffix for node annotations
const NotificationPluginSuffix = "notify"

// TriggerPluginSuffix is the notification plugin configuration suffix for node annotations
const TriggerPluginSuffix = "trigger"

// ConfigFilePath is the path to the configuration file
const ConfigFilePath = "./maintenance_config.yaml"

// ReconcileError signals if a reconciliation failed
type ReconcileError struct {
	Message string
	Err     error
}

func (e ReconcileError) Unwrap() error {
	return e.Err
}

func (e ReconcileError) Error() string {
	return fmt.Sprintf("reconciliation failed: %v", e.Message)
}

// NewReconcileError creates a new ReconcileError from the given root Error and a custom message
func NewReconcileError(err error, message string) ReconcileError {
	return ReconcileError{Message: message, Err: err}
}

// NodeReconciler reconciles a Node object
type NodeReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch

// Reconcile reconciles the given request
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
		r.Log.Error(err, "Failed to retrive node information from the API server", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: config.RequeueInterval}, nil
	}
	unmodifiedNode := theNode.DeepCopy()

	// perform the reconciliation
	err = r.reconcileInternal(globalContext, &theNode, config)
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

func (r *NodeReconciler) reconcileInternal(ctx context.Context, node *corev1.Node, config *Config) error {
	// get the current node state
	stateStr, ok := node.Labels[config.StateKey]
	// if not found attach operational state
	if !ok {
		if node.Labels == nil {
			node.Labels = make(map[string]string)
		}
		node.Labels[config.StateKey] = string(state.Operational)
		stateStr = string(state.Operational)
	}
	stateLabel := state.NodeStateLabel(stateStr)

	// get the plugin configurations for the current state
	currentStateKey := config.AnnotationBaseKey + "-" + stateStr
	checkStr := node.Annotations[currentStateKey+"-"+CheckPluginSuffix]
	notificationStr := node.Annotations[currentStateKey+"-"+NotificationPluginSuffix]
	triggerStr := node.Annotations[currentStateKey+"-"+TriggerPluginSuffix]
	registry := &config.Registry

	checkChain, err := registry.NewCheckChain(checkStr)
	if err != nil {
		return NewReconcileError(err, "failed to parse check chain config")
	}
	notificationChain, err := registry.NewNotificationChain(notificationStr)
	if err != nil {
		return NewReconcileError(err, "failed to parse notification chain config")
	}
	triggerChain, err := registry.NewTriggerChain(triggerStr)
	if err != nil {
		return NewReconcileError(err, "failed to parse trigger chain config")
	}
	chains := state.PluginChains{
		Check:        checkChain,
		Notification: notificationChain,
		Trigger:      triggerChain,
	}

	// construct state
	stateObj, err := state.FromLabel(stateLabel, chains, config.NotificationInterval)
	if err != nil {
		return NewReconcileError(err, "failed to create internal state from unkown label value")
	}

	// build plugin arguments
	dataStr := node.Annotations[config.AnnotationBaseKey+"-data"]
	var data state.Data
	json.Unmarshal([]byte(dataStr), &data)
	params := plugin.Parameters{
		Node:   node,
		State:  stateStr,
		Client: r.Client,
		Ctx:    ctx,
		Log:    r.Log,
	}

	err = stateObj.Notify(params, &data)
	if err != nil {
		return NewReconcileError(err, "failed to notify")
	}
	next, err := stateObj.Transition(params, &data)
	if err != nil {
		// return NewReconcileError(err, "failed to check for state transition")
		r.Log.Error(err, "Failed to check for state transition", "state", stateStr)
	}

	// check if a transition should happen
	if next != stateLabel {
		err = stateObj.Trigger(params, &data)
		if err != nil {
			r.Log.Error(err, "Failed to execute triggers", "state", stateStr)
		} else {
			node.Labels[config.StateKey] = string(next)
			data.LastTransition = time.Now()
		}
	}

	// update data annotation
	dataBytes, err := json.Marshal(&data)
	if err != nil {
		return NewReconcileError(err, "failed to marshal internal data")
	}
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[config.AnnotationBaseKey+"-data"] = string(dataBytes)

	return nil
}

// SetupWithManager attaches the controller to the given manager
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}

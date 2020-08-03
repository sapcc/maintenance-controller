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

// CheckPluginSuffix is the check plugin configuration suffix for node annotations.
const CheckPluginSuffix = "check"

// NotificationPluginSuffix is the notification plugin configuration suffix for node annotations.
const NotificationPluginSuffix = "notify"

// TriggerPluginSuffix is the notification plugin configuration suffix for node annotations.
const TriggerPluginSuffix = "trigger"

// ConfigFilePath is the path to the configuration file.
const ConfigFilePath = "./config/maintenance.yaml"

// NodeReconciler reconciles a Node object.
type NodeReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
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
	// fetch the current node state
	stateLabel := parseNodeState(node, config.StateKey)
	stateStr := string(stateLabel)

	// get the plugin configurations for the current state
	chains, err := parsePluginChains(node, config, stateStr)
	if err != nil {
		return err
	}

	// construct state
	stateObj, err := state.FromLabel(stateLabel, chains, config.NotificationInterval)
	if err != nil {
		return fmt.Errorf("failed to create internal state from unknown label value: %w", err)
	}

	// build plugin arguments
	dataStr := node.Annotations[config.AnnotationBaseKey+"-data"]
	var data state.Data
	if dataStr != "" {
		err = json.Unmarshal([]byte(dataStr), &data)
		if err != nil {
			return fmt.Errorf("failed to parse json value in data annotation: %w", err)
		}
	}
	params := plugin.Parameters{Client: r.Client, Ctx: ctx, Log: r.Log, Node: node,
		State: stateStr, StateKey: config.StateKey, LastTransition: data.LastTransition}

	// invoke notifications and check for transition
	err = stateObj.Notify(params, &data)
	if err != nil {
		return fmt.Errorf("failed to notify: %w", err)
	}
	next, err := stateObj.Transition(params, &data)
	if err != nil {
		r.Log.Error(err, "Failed to check for state transition", "state", stateStr)
	}

	// check if a transition should happen
	if next != stateLabel {
		err = stateObj.Trigger(params, &data)
		if err != nil {
			r.Log.Error(err, "Failed to execute triggers", "state", stateStr)
		} else {
			node.Labels[config.StateKey] = string(next)
			data.LastTransition = time.Now().UTC()
		}
	}

	// update data annotation
	dataBytes, err := json.Marshal(&data)
	if err != nil {
		return fmt.Errorf("failed to marshal internal data: %w", err)
	}
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[config.AnnotationBaseKey+"-data"] = string(dataBytes)

	return nil
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

func parsePluginChains(node *corev1.Node, config *Config, stateStr string) (state.PluginChains, error) {
	// construct annotation keys
	currentStateKey := config.AnnotationBaseKey + "-" + stateStr
	checkStr := node.Annotations[currentStateKey+"-"+CheckPluginSuffix]
	notificationStr := node.Annotations[currentStateKey+"-"+NotificationPluginSuffix]
	triggerStr := node.Annotations[currentStateKey+"-"+TriggerPluginSuffix]

	// invoke parsers
	var chains state.PluginChains
	checkChain, err := config.Registry.NewCheckChain(checkStr)
	if err != nil {
		return chains, fmt.Errorf("failed to parse check chain config: %w", err)
	}
	notificationChain, err := config.Registry.NewNotificationChain(notificationStr)
	if err != nil {
		return chains, fmt.Errorf("failed to parse notification chain config: %w", err)
	}
	triggerChain, err := config.Registry.NewTriggerChain(triggerStr)
	if err != nil {
		return chains, fmt.Errorf("failed to parse trigger chain config: %w", err)
	}
	chains.Check = checkChain
	chains.Notification = notificationChain
	chains.Trigger = triggerChain
	return chains, nil
}

// SetupWithManager attaches the controller to the given manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}).
		Complete(r)
}

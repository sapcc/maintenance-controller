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

	"github.com/elastic/go-ucfg"
	"github.com/go-logr/logr"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/cache"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/state"
)

// NodeReconciler reconciles a Node object.
type NodeReconciler struct {
	client.Client
	Log           logr.Logger
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	NodeInfoCache cache.NodeInfoCache
}

type reconcileParameters struct {
	client        client.Client
	config        *Config
	log           logr.Logger
	recorder      record.EventRecorder
	node          *corev1.Node
	nodeInfoCache cache.NodeInfoCache
}

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;update;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile reconciles the given request.
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// load the configuration
	conf, err := ucfgwrap.FromYAMLFile(constants.MaintenanceConfigFilePath, ucfg.VarExp, ucfg.ResolveEnv)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (syntax error)")
		// the controller is missconfigured, no need to requeue before the configuration is fixed
		return ctrl.Result{}, nil
	}
	config, err := LoadConfig(&conf)
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
	err = reconcileInternal(ctx, r.makeParams(config, &theNode))
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

func (r *NodeReconciler) makeParams(config *Config, node *corev1.Node) reconcileParameters {
	return reconcileParameters{
		client:        r.Client,
		config:        config,
		log:           r.Log.WithValues("node", types.NamespacedName{Name: node.Name, Namespace: node.Namespace}),
		node:          node,
		recorder:      r.Recorder,
		nodeInfoCache: r.NodeInfoCache,
	}
}

// Ensures a new version of the specified resources arrives in the cache made by controller-runtime.
func pollCacheUpdate(ctx context.Context, k8sClient client.Client, ref types.NamespacedName, targetVersion string) error {
	return wait.PollImmediate(20*time.Millisecond, 1*time.Second, func() (bool, error) { //nolint:gomnd,staticcheck
		var nextNode corev1.Node
		if err := k8sClient.Get(ctx, ref, &nextNode); err != nil {
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

func reconcileInternal(ctx context.Context, params reconcileParameters) error {
	dataStr := params.node.Annotations[constants.DataAnnotationKey]
	data, err := state.ParseMigrateDataV2(dataStr, params.log)
	if err != nil {
		return err
	}
	err = HandleNode(ctx, params, &data)
	if err != nil {
		return err
	}
	return writeData(params.node, data)
}

func writeData(node *corev1.Node, data state.DataV2) error {
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

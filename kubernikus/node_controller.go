// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package kubernikus

import (
	"context"
	"fmt"
	"time"

	"github.com/blang/semver/v4"
	"github.com/elastic/go-ucfg"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/compute/v2/servers"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
)

// According to https://pkg.go.dev/k8s.io/client-go/util/workqueue
// the same node is never reconciled more than once concurrently.
const ConcurrentReconciles = 5

type Config struct {
	Intervals struct {
		Requeue     time.Duration `config:"requeue" validate:"required"`
		PodDeletion struct {
			Period  time.Duration `config:"period" validate:"required"`
			Timeout time.Duration `config:"timeout" validate:"required"`
		} `config:"podDeletion" validate:"required"`
		PodEviction struct {
			Period  time.Duration `config:"period" validate:"required"`
			Timeout time.Duration `config:"timeout" validate:"required"`
			Force   bool          `config:"force"`
		} `config:"podEviction" validate:"required"`
	}
	CloudProviderSecret struct {
		Name      string `config:"name"`
		Namespace string `config:"namespace"`
	} `config:"cloudProviderSecret"`
}

func (r *NodeReconciler) loadConfig() (Config, error) {
	yamlConf, err := ucfgwrap.FromYAMLFile(constants.KubernikusConfigFilePath, ucfg.VarExp, ucfg.ResolveEnv)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (syntax error)")
		return Config{}, err
	}
	var conf Config
	err = yamlConf.Unpack(&conf)
	if err != nil {
		r.Log.Error(err, "Failed to parse configuration file (semantic error)")
		return Config{}, err
	}
	return conf, nil
}

// NodeReconciler reconciles a Node object.
type NodeReconciler struct {
	client.Client
	Conf   *rest.Config
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// Reconcile reconciles the given request.
func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	conf, err := r.loadConfig()
	if err != nil {
		r.Log.Error(err, "Failed to load Kubernikus configuration")
		return ctrl.Result{RequeueAfter: conf.Intervals.Requeue}, err
	}

	node := &v1.Node{}
	err = r.Get(ctx, req.NamespacedName, node)
	if errors.IsNotFound(err) {
		r.Log.Info("Could not find node on the API server, maybe it has been deleted?", "node", req.NamespacedName)
		return ctrl.Result{RequeueAfter: conf.Intervals.Requeue}, nil
	} else if err != nil {
		r.Log.Error(err, "Failed to retrieve node", "node", req.Name)
		return ctrl.Result{RequeueAfter: conf.Intervals.Requeue}, nil
	}

	// mark kubelet update
	err = r.markUpdate(ctx, node)
	if err != nil {
		r.Log.Error(err, "failed to mark node for kubelet upgrade", "node", node.Name)
		return ctrl.Result{RequeueAfter: conf.Intervals.Requeue}, nil
	}

	// delete if requested
	shouldDelete, ok := node.Labels[constants.DeleteNodeLabelKey]
	if ok && shouldDelete == constants.TrueStr {
		secretKey := client.ObjectKey{
			Name:      conf.CloudProviderSecret.Name,
			Namespace: conf.CloudProviderSecret.Namespace,
		}
		err = r.deleteNode(ctx, node, secretKey,
			common.DrainParameters{
				Client:    r.Client,
				Clientset: kubernetes.NewForConfigOrDie(r.Conf),
				AwaitDeletion: common.WaitParameters{
					Period:  conf.Intervals.PodDeletion.Period,
					Timeout: conf.Intervals.PodDeletion.Timeout,
				},
				Eviction: common.WaitParameters{
					Period:  conf.Intervals.PodEviction.Period,
					Timeout: conf.Intervals.PodEviction.Timeout,
				},
				ForceEviction: conf.Intervals.PodEviction.Force,
			},
		)
		if err != nil {
			r.Log.Error(err, "failed to remove Kubernikus node", "node", node.Name)
			return ctrl.Result{RequeueAfter: conf.Intervals.Requeue}, nil
		}
	}

	return ctrl.Result{RequeueAfter: conf.Intervals.Requeue}, nil
}

func (r *NodeReconciler) markUpdate(ctx context.Context, node *v1.Node) error {
	unmodified := node.DeepCopy()
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	update, err := r.needsKubeletUpdate(node)
	if err != nil {
		return err
	}
	if update {
		node.Labels[constants.KubeletUpdateLabelKey] = constants.TrueStr
	} else {
		node.Labels[constants.KubeletUpdateLabelKey] = "false"
	}
	err = r.Patch(ctx, node, client.MergeFrom(unmodified))
	if err != nil {
		return err
	}
	return nil
}

func (r *NodeReconciler) needsKubeletUpdate(node *v1.Node) (bool, error) {
	KubeletVersion, err := semver.Parse(node.Status.NodeInfo.KubeletVersion[1:])
	if err != nil {
		return false, err
	}

	APIVersion, err := getAPIServerVersion(r.Conf)
	if err != nil {
		return false, err
	}
	return APIVersion.NE(KubeletVersion), nil
}

func getAPIServerVersion(conf *rest.Config) (semver.Version, error) {
	clientset, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return semver.Version{}, fmt.Errorf("failed to create API Server client: %w", err)
	}
	return common.GetAPIServerVersion(clientset)
}

func (r *NodeReconciler) deleteNode(ctx context.Context, node *v1.Node, secretKey client.ObjectKey, params common.DrainParameters) error {
	r.Log.Info("Cordoning, draining and deleting node", "node", node.Name)
	err := common.EnsureSchedulable(ctx, r.Client, node, false)
	// In case of error just retry, cordoning is ensured again
	if err != nil {
		return fmt.Errorf("failed to cordon node %s: %w", node.Name, err)
	}
	// In case of error or node not empty just retry, draining is ensured again
	drained, err := common.EnsureDrain(ctx, node, r.Log, params)
	if err != nil {
		return fmt.Errorf("failed to drain node %s: %w", node.Name, err)
	}
	if !drained {
		r.Log.Info("Node drain still in progress; will continue in next reconcile", "node", node.Name)
		return nil
	}
	osConf, err := common.LoadOSConfig(ctx, r.Client, secretKey)
	if err != nil {
		return fmt.Errorf("failed to load OpenStack config: %w", err)
	}
	if err := deleteVM(ctx, node.Name, osConf); err != nil {
		return fmt.Errorf("failed to delete VM backing node %s: %w", node.Name, err)
	}
	return nil
}

func deleteVM(ctx context.Context, nodeName string, osConf common.OpenStackConfig) error {
	provider, endpointOpts, err := osConf.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed OpenStack authentication: %w", err)
	}
	compute, err := openstack.NewComputeV2(provider, endpointOpts)
	if err != nil {
		return fmt.Errorf("failed to create OS compute endpoint: %w", err)
	}
	list, err := servers.List(compute, servers.ListOpts{
		TenantID: osConf.ProjectID,
		Name:     nodeName,
	}).AllPages(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}
	serverList, err := servers.ExtractServers(list)
	if err != nil {
		return fmt.Errorf("failed to extract server list: %w", err)
	}
	if len(serverList) == 0 {
		// if 0 servers are returned the backing VM is already hopefully deleted
		return nil
	}
	if len(serverList) != 1 {
		return fmt.Errorf("expected to list 1 or 0 servers, but got %v", len(serverList))
	}
	result := servers.Delete(ctx, compute, serverList[0].ID)
	if result.ExtractErr() != nil {
		return fmt.Errorf("failed to delete VM: %w body: %v", result.ExtractErr(), result.Body)
	}
	return nil
}

// SetupWithManager attaches the controller to the given manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			// According to https://pkg.go.dev/k8s.io/client-go/util/workqueue
			// the same node is never reconciled more than once concurrently.
			MaxConcurrentReconciles: ConcurrentReconciles,
		}).
		For(&v1.Node{}, builder.WithPredicates()).
		Named("kubernikus").
		Complete(r)
}

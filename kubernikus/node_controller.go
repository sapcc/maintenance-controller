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

package kubernikus

import (
	"context"
	"fmt"
	"time"

	"github.com/blang/semver"
	"github.com/elastic/go-ucfg/yaml"
	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
	"gopkg.in/ini.v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Config struct {
	Intervals struct {
		Requeue     time.Duration `config:"requeue" validate:"required"`
		PodDeletion struct {
			Period  time.Duration `config:"period" validate:"required"`
			Timeout time.Duration `config:"timeout" validate:"required"`
		} `config:"podDeletion" validate:"required"`
	}
}

func (r *NodeReconciler) loadConfig() (Config, error) {
	yamlConf, err := yaml.NewConfigWithFile(constants.KubernikusConfigFilePath)
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
		return ctrl.Result{}, err
	}

	node := &v1.Node{}
	err = r.Get(ctx, req.NamespacedName, node)
	if err != nil {
		return ctrl.Result{}, err
	}
	unmodified := node.DeepCopy()
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}

	// mark kubelet update
	update, err := r.needsKubeletUpdate(node)
	if err != nil {
		return ctrl.Result{}, err
	}
	if update {
		node.Labels[constants.KubeletUpdateLabelKey] = constants.TrueStr
	} else {
		node.Labels[constants.KubeletUpdateLabelKey] = "false"
	}

	err = r.Patch(ctx, node, client.MergeFrom(unmodified))
	if err != nil {
		return ctrl.Result{}, err
	}

	// delete if requested
	shouldDelete, ok := node.Labels[constants.DeleteNodeLabelKey]
	if ok && shouldDelete == constants.TrueStr {
		err = r.deleteNode(ctx, node, common.WaitParameters{
			Client:  r.Client,
			Period:  conf.Intervals.PodDeletion.Period,
			Timeout: conf.Intervals.PodDeletion.Timeout,
		})
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: conf.Intervals.Requeue}, nil
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
	fmt.Printf("API: %s Kubelet: %s\n", APIVersion, KubeletVersion)
	return APIVersion.GT(KubeletVersion), nil
}

func getAPIServerVersion(conf *rest.Config) (semver.Version, error) {
	client, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return semver.Version{}, fmt.Errorf("failed to create API Server client: %w", err)
	}
	rsp, err := client.ServerVersion()
	if err != nil {
		return semver.Version{}, fmt.Errorf("failed to do request for API Server version: %w", err)
	}
	gitVersion := rsp.GitVersion[1:]
	version, err := semver.Parse(gitVersion)
	if err != nil {
		return semver.Version{}, fmt.Errorf("API Server version %s is not semver compatible: %w", gitVersion, err)
	}
	return version, nil
}

func (r *NodeReconciler) deleteNode(ctx context.Context, node *v1.Node, params common.WaitParameters) error {
	r.Log.Info("Cordoning, draining and deleting node", "node", node.Name)
	err := common.EnsureSchedulable(ctx, r.Client, node, false)
	// In case of error just retry, cordoning is ensured again
	if err != nil {
		return fmt.Errorf("failed to cordon node %s: %w", node.Name, err)
	}
	err = common.EnsureDrain(ctx, node, r.Log, params)
	// In case of error just retry, draining is ensured again
	if err != nil {
		return fmt.Errorf("failed to drain node %s: %w", node.Name, err)
	}
	err = deleteVM(ctx, node.Name)
	if err != nil {
		return fmt.Errorf("failed to delete VM backing node %s: %w", node.Name, err)
	}
	return nil
}

func deleteVM(ctx context.Context, nodeName string) error {
	osConf := struct {
		Global struct {
			AuthURL  string `ini:"auth-url"`
			Username string `ini:"username"`
			Password string `ini:"password"`
			Region   string `ini:"region"`
		} `ini:"global"`
	}{}
	err := ini.MapTo(&osConf, "config/cloudprovider.conf")
	if err != nil {
		return err
	}
	opts := &clientconfig.ClientOpts{
		AuthInfo: &clientconfig.AuthInfo{
			AuthURL:  osConf.Global.AuthURL,
			Username: osConf.Global.Username,
			Password: osConf.Global.Password,
		},
	}
	provider, err := clientconfig.AuthenticatedClient(opts)
	if err != nil {
		return err
	}
	provider.Context = ctx
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: osConf.Global.Region,
	})
	if err != nil {
		return err
	}
	result := servers.Delete(compute, nodeName)
	if result.Err != nil {
		return err
	}
	return nil
}

// SetupWithManager attaches the controller to the given manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Node{}).
		Complete(r)
}

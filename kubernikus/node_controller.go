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

	semver "github.com/blang/semver/v4"
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
	"k8s.io/apimachinery/pkg/api/errors"
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

type OpenStackConfig struct {
	Region     string
	AuthURL    string
	Username   string
	Password   string
	Domainname string
	ProjectID  string
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
	if errors.IsNotFound(err) {
		r.Log.Info("Could not find node on the API server, maybe it has been deleted?", "node", req.NamespacedName)
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// mark kubelet update
	err = r.markUpdate(ctx, node)
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
	osConf, err := loadOpenStackConfig()
	if err != nil {
		return fmt.Errorf("failed to parese cloudprovider.conf: %w", err)
	}
	opts := &clientconfig.ClientOpts{
		AuthInfo: &clientconfig.AuthInfo{
			AuthURL:        osConf.AuthURL,
			Username:       osConf.Username,
			Password:       osConf.Password,
			UserDomainName: osConf.Domainname,
			ProjectID:      osConf.ProjectID,
		},
	}
	provider, err := clientconfig.AuthenticatedClient(opts)
	if err != nil {
		return fmt.Errorf("failed OpenStack authentification: %w", err)
	}
	provider.Context = ctx
	compute, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Region: osConf.Region,
	})
	if err != nil {
		return fmt.Errorf("failed to create OS compute endpoint: %w", err)
	}
	list, err := servers.List(compute, servers.ListOpts{
		TenantID: osConf.ProjectID,
		Name:     nodeName,
	}).AllPages()
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
	result := servers.Delete(compute, serverList[0].ID)
	if result.ExtractErr() != nil {
		return fmt.Errorf("failed to delete VM: %w body: %v", result.ExtractErr(), result.Body)
	}
	return nil
}

func loadOpenStackConfig() (OpenStackConfig, error) {
	osConf := struct {
		Global struct {
			AuthURL    string `ini:"auth-url"`
			Username   string `ini:"username"`
			Password   string `ini:"password"`
			Region     string `ini:"region"`
			Domainname string `ini:"domain-name"`
			TenantID   string `ini:"tenant-id"`
		} `ini:"Global"`
	}{}
	err := ini.MapTo(&osConf, constants.CloudProviderConfigFilePath)
	if err != nil {
		return OpenStackConfig{}, fmt.Errorf("failed to parese cloudprovider.conf: %w", err)
	}
	return OpenStackConfig{
		Region:     osConf.Global.Region,
		AuthURL:    osConf.Global.AuthURL,
		Username:   osConf.Global.Username,
		Password:   osConf.Global.Password,
		Domainname: osConf.Global.Domainname,
		ProjectID:  osConf.Global.TenantID,
	}, nil
}

// SetupWithManager attaches the controller to the given manager.
func (r *NodeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Node{}).
		Complete(r)
}

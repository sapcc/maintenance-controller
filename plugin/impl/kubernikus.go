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

package impl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/plugin"
	corev1 "k8s.io/api/core/v1"
)

type KubernikusCount struct {
	Cluster string
}

type nodePool struct {
	Size int
}

type kluster struct {
	Spec struct {
		NodePools []nodePool
	}
}

func (kc *KubernikusCount) New(config *common.Config) (plugin.Checker, error) {
	conf := struct {
		Cluster string `config:"cluster" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &KubernikusCount{Cluster: conf.Cluster}, nil
}

func (kc *KubernikusCount) Check(params plugin.Parameters) (bool, error) {
	kluster, err := kc.fetchKluster(&params)
	if err != nil {
		return false, err
	}
	var nodeList corev1.NodeList
	err = params.Client.List(params.Ctx, &nodeList)
	if err != nil {
		return false, err
	}
	specCount := 0
	for _, nodePool := range kluster.Spec.NodePools {
		specCount += nodePool.Size
	}
	if len(nodeList.Items) >= specCount {
		return true, nil
	}
	return false, nil
}

func (kc *KubernikusCount) fetchKluster(params *plugin.Parameters) (kluster, error) {
	osConf, err := common.LoadOpenStackConfig()
	if err != nil {
		return kluster{}, err
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
		return kluster{}, fmt.Errorf("failed OpenStack authentification: %w", err)
	}
	kubernikusURL := fmt.Sprintf("https://kubernikus.%s.cloud.sap/api/v1/clusters/%s", osConf.Region, kc.Cluster)
	req, err := http.NewRequestWithContext(params.Ctx, http.MethodGet, kubernikusURL, strings.NewReader(""))
	req.Header.Add("x-auth-token", provider.Token())
	if err != nil {
		return kluster{}, err
	}
	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		return kluster{}, err
	}
	defer rsp.Body.Close()
	data, err := io.ReadAll(rsp.Body)
	if err != nil {
		return kluster{}, err
	}
	result := kluster{}
	err = json.Unmarshal(data, &result)
	if err != nil {
		return kluster{}, err
	}
	return result, nil
}

func (kc *KubernikusCount) AfterEval(chainResult bool, params plugin.Parameters) error {
	return nil
}

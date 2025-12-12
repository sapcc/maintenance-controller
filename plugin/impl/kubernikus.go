// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/plugin"
)

type KubernikusCount struct {
	Cluster             string
	CloudProviderSecret client.ObjectKey
}

type nodePool struct {
	Size int
}

type kluster struct {
	Spec struct {
		NodePools []nodePool
	}
}

func (kc *KubernikusCount) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Cluster             string `config:"cluster" validate:"required"`
		CloudProviderSecret struct {
			Name      string `config:"name"`
			Namespace string `config:"namespace"`
		} `config:"cloudProviderSecret"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	secretKey := client.ObjectKey{Name: conf.CloudProviderSecret.Name, Namespace: conf.CloudProviderSecret.Namespace}
	return &KubernikusCount{Cluster: conf.Cluster, CloudProviderSecret: secretKey}, nil
}

func (kc *KubernikusCount) ID() string {
	return "kubernikusCount"
}

func (kc *KubernikusCount) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	cluster, err := kc.fetchKluster(&params)
	if err != nil {
		return plugin.Failed(nil), err
	}
	var nodeList corev1.NodeList
	err = params.Client.List(params.Ctx, &nodeList)
	if err != nil {
		return plugin.Failed(nil), err
	}
	specCount := 0
	for _, nodePool := range cluster.Spec.NodePools {
		specCount += nodePool.Size
	}
	if len(nodeList.Items) >= specCount {
		return plugin.PassedWithReason("found equal or more nodes than specified by nodepool"), nil
	}
	return plugin.FailedWithReason("found less nodes than specified by nodepool"), nil
}

func (kc *KubernikusCount) fetchKluster(params *plugin.Parameters) (kluster, error) {
	osConf, err := common.LoadOSConfig(params.Ctx, params.Client, kc.CloudProviderSecret)
	if err != nil {
		return kluster{}, err
	}
	provider, _, err := osConf.Connect(params.Ctx)
	if err != nil {
		return kluster{}, fmt.Errorf("failed OpenStack authentication: %w", err)
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

func (kc *KubernikusCount) OnTransition(params plugin.Parameters) error {
	return nil
}

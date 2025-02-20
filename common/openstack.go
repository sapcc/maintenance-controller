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

package common

import (
	"context"
	"fmt"
	"os"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"gopkg.in/ini.v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/constants"
)

type OpenStackConfig struct {
	Region     string
	AuthURL    string
	Username   string
	Password   string
	Domainname string
	ProjectID  string
}

func parseOSConfig(data []byte) (OpenStackConfig, error) {
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
	if err := ini.MapTo(&osConf, data); err != nil {
		return OpenStackConfig{}, fmt.Errorf("failed to parse cloudprovider.conf: %w", err)
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

func LoadOSConfigFile() (OpenStackConfig, error) {
	data, err := os.ReadFile(constants.CloudProviderConfigFilePath)
	if err != nil {
		return OpenStackConfig{}, fmt.Errorf("failed to read cloudprovider.conf: %w", err)
	}
	return parseOSConfig(data)
}

func LoadOSConfig(ctx context.Context, k8sClient client.Client, secretKey client.ObjectKey) (OpenStackConfig, error) {
	if secretKey.Name == "" || secretKey.Namespace == "" {
		return LoadOSConfigFile()
	}
	secret := &corev1.Secret{}
	err := k8sClient.Get(ctx, secretKey, secret)
	if err != nil {
		return OpenStackConfig{}, fmt.Errorf("failed to retrieve secret %s: %w", secretKey, err)
	}
	cloudProviderConf, ok := secret.Data["cloudprovider.conf"]
	if !ok {
		return OpenStackConfig{}, fmt.Errorf("secret %s does not contain key 'cloudprovider.conf'", secretKey)
	}
	return parseOSConfig(cloudProviderConf)
}

func (osConf OpenStackConfig) Connect(ctx context.Context) (*gophercloud.ProviderClient, gophercloud.EndpointOpts, error) {
	ao := gophercloud.AuthOptions{
		IdentityEndpoint: osConf.AuthURL,
		Username:         osConf.Username,
		Password:         osConf.Password,
		DomainName:       osConf.Domainname, // domain name of user, not of project
		AllowReauth:      true,
		Scope: &gophercloud.AuthScope{
			ProjectID: osConf.ProjectID,
		},
	}
	provider, err := openstack.NewClient(ao.IdentityEndpoint)
	if err != nil {
		return nil, gophercloud.EndpointOpts{}, err
	}
	err = openstack.Authenticate(ctx, provider, ao)
	if err != nil {
		return nil, gophercloud.EndpointOpts{}, err
	}
	eo := gophercloud.EndpointOpts{
		Region: osConf.Region,
	}
	return provider, eo, nil
}

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
	"fmt"

	"github.com/sapcc/maintenance-controller/constants"
	"gopkg.in/ini.v1"
)

type OpenStackConfig struct {
	Region     string
	AuthURL    string
	Username   string
	Password   string
	Domainname string
	ProjectID  string
}

func LoadOpenStackConfig() (OpenStackConfig, error) {
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

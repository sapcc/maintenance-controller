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

package esx

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vmware/govmomi"
)

// Specifies the string in a vCenter URL, which is replaced by the availability zone.
const AvailabilityZoneReplacer string = "$AZ"

type Config struct {
	Intervals struct {
		Node time.Duration `config:"node" validate:"required"`
		ESX  time.Duration `config:"esx" validate:"required"`
	} `config:"intervals" validate:"required"`
	VCenters VCenters `config:"vCenters" validate:"required"`
}

type Credential struct {
	Username string `config:"username" validate:"required"`
	Password string `config:"password" validate:"required"`
}

// VCenters contains connection information to regional vCenters.
type VCenters struct {
	// URL to regional vCenters with the availability zone replaced by AvailabilityZoneReplacer.
	Template string `config:"templateUrl" validate:"required"`
	// Pair of credentials per availability zone.
	Credentials map[string]Credential `config:"credentials" validate:"required"`
}

// Gets an URL to connect to a vCenters in a specific availability zone.
func (vc *VCenters) URL(availabilityZone string) (*url.URL, error) {
	withAZ := strings.ReplaceAll(vc.Template+"/sdk", AvailabilityZoneReplacer, availabilityZone)
	vCenterURL, err := url.Parse(withAZ)
	if err != nil {
		return nil, err
	}
	cred, ok := vc.Credentials[availabilityZone]
	if !ok {
		return nil, fmt.Errorf("No vCenter credentials have been provided for availability zone %v", availabilityZone)
	}
	vCenterURL.User = url.UserPassword(cred.Username, cred.Password)
	return vCenterURL, nil
}

// Returns a ready to use vCenter client for the given availability zone.
func (vc *VCenters) Client(ctx context.Context, availabilityZone string) (*govmomi.Client, error) {
	url, err := vc.URL(availabilityZone)
	if err != nil {
		return nil, fmt.Errorf("Failed to render vCenter URL: %w", err)
	}
	client, err := govmomi.NewClient(ctx, url, false)
	if err != nil {
		return nil, fmt.Errorf("Failed to create vCenter client: %w", err)
	}
	return client, nil
}

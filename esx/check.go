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
	"time"

	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

type ESXMaintenance string

const NoMaintenance ESXMaintenance = "false"

const InMaintenance ESXMaintenance = "true"

const NotRequired ESXMaintenance = "not-required"

// Timestamps tracks the last time esx hosts haven been checked.
type Timestamps struct {
	// Specifies how often the vCenter is queried for a specific esx host.
	Interval time.Duration
	// Maps an ESX Hosts to the time it was checked
	lastChecks map[string]time.Time
}

func NewTimestamps() Timestamps {
	return Timestamps{
		Interval:   1 * time.Minute,
		lastChecks: make(map[string]time.Time),
	}
}

// Returns true if an esx host needs to be checked for maintenance.
func (t *Timestamps) CheckRequired(host string) bool {
	t.clean()
	lastCheck, ok := t.lastChecks[host]
	if !ok {
		return true
	}
	return time.Since(lastCheck) > t.Interval
}

// Sets the time the given esx host was checked to time.Now().
func (t *Timestamps) MarkChecked(host string) {
	t.lastChecks[host] = time.Now()
}

// Cleanup not recently checked esx hosts to avoid "leaking" memory, if esx hosts get removed.
func (t *Timestamps) clean() {
	for host, stamp := range t.lastChecks {
		if time.Since(stamp) > t.Interval {
			delete(t.lastChecks, host)
		}
	}
}

// Describes an ESX host within an availability zone.
type Host struct {
	Name             string
	AvailabilityZone string
}

type CheckParameters struct {
	VCenters   *VCenters
	Timestamps *Timestamps
	Host       Host
}

// Performs a check for the specified host if allowed by timestamps.
func CheckForMaintenance(ctx context.Context, params CheckParameters) (ESXMaintenance, error) {
	if !params.Timestamps.CheckRequired(params.Host.Name) {
		return NotRequired, nil
	}
	// Do the check
	client, err := params.VCenters.Client(ctx, params.Host.AvailabilityZone)
	if err != nil {
		return NoMaintenance, fmt.Errorf("Failed to check for esx host maintenance state: %w", err)
	}
	mgr := view.NewManager(client.Client)
	view, err := mgr.CreateContainerView(context.Background(), client.ServiceContent.RootFolder,
		[]string{"HostSystem"}, true)
	if err != nil {
		return NoMaintenance, fmt.Errorf("Failed to create container view: %w", err)
	}
	var hss []mo.HostSystem
	err = view.RetrieveWithFilter(context.Background(), []string{"HostSystem"}, []string{"runtime"},
		&hss, property.Filter{"name": params.Host.Name})
	if err != nil {
		return NoMaintenance, fmt.Errorf("Failed to fetch runtime information for esx host %v: %w", params.Host.Name, err)
	}
	if len(hss) != 1 {
		return NoMaintenance, fmt.Errorf("Expected to retrieve 1 esx host from vCenter, but got %v", len(hss))
	}
	params.Timestamps.MarkChecked(params.Host.Name)
	if hss[0].Runtime.InMaintenanceMode {
		return InMaintenance, nil
	}
	return NoMaintenance, nil
}

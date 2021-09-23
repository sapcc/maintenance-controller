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

	"github.com/go-logr/logr"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

type Maintenance string

const NoMaintenance Maintenance = "false"

const InMaintenance Maintenance = "true"

const UnknownMaintenance Maintenance = "unknown"

type CheckParameters struct {
	VCenters *VCenters
	Host     HostInfo
	Log      logr.Logger
}

// Performs a check for the specified host if allowed by timestamps.
func CheckForMaintenance(ctx context.Context, params CheckParameters) (Maintenance, error) {
	client, err := params.VCenters.Client(ctx, params.Host.AvailabilityZone)
	if err != nil {
		return UnknownMaintenance, fmt.Errorf("Failed to check for esx host maintenance state: %w", err)
	}
	host, err := fetchHost(ctx, client.Client, params.Host.Name)
	if err != nil {
		return UnknownMaintenance, err
	}
	if host.Runtime.InMaintenanceMode {
		return InMaintenance, nil
	}
	// The vSphere API models entering maintenance mode as a task.
	// Runtime.InMaintenanceMode is only true if that task completed.
	// The tasks completion requires that all virtual machines on a host are moved or powered off.
	// Since the labels, which are attached by this controller should allow automation on moves
	// or shutdowns, it is required to also consider an ESX host as in-maintenance if the
	// entering maintenance mode task is running.
	// This branch can't be tested currently as the govmomi simulator just sets a host into
	// maintenance if requested.
	// The simulator ignores the condition mentioned above.
	taskRefs := host.RecentTask
	// no recent tasks, so no maintenance
	if len(taskRefs) == 0 {
		return NoMaintenance, nil
	}
	var tasks []mo.Task
	err = client.Retrieve(ctx, taskRefs, []string{"info"}, &tasks)
	if err != nil {
		return UnknownMaintenance, fmt.Errorf("Failed to fetch recent task information for esx host %v: %w",
			params.Host.Name, err)
	}
	for _, task := range tasks {
		params.Log.Info("Got a recent task for ESX", "esx", params.Host.Name,
			"name", task.Info.Name, "state", task.Info.State)
		// do not care about status queued and error
		// success should already be handled by checking for Runtime.InMaintenanceMode
		// also recent tasks retains completed tasks for a while
		// so checking for success could result in returning in-maintenance while the ESX
		// is actually running again.
		if task.Info.Name == "EnterMaintenanceMode_Task" && task.Info.State == types.TaskInfoStateRunning {
			return InMaintenance, nil
		}
	}
	return NoMaintenance, nil
}

func fetchHost(ctx context.Context, client *vim25.Client, hostname string) (mo.HostSystem, error) {
	mgr := view.NewManager(client)
	view, err := mgr.CreateContainerView(ctx, client.ServiceContent.RootFolder,
		[]string{"HostSystem"}, true)
	if err != nil {
		return mo.HostSystem{}, fmt.Errorf("Failed to create container view: %w", err)
	}
	var hss []mo.HostSystem
	err = view.RetrieveWithFilter(ctx, []string{"HostSystem"}, []string{"runtime", "recentTask"},
		&hss, property.Filter{"name": hostname})
	if err != nil {
		return mo.HostSystem{}, fmt.Errorf("Failed to fetch runtime information for esx host %v: %w",
			hostname, err)
	}
	if len(hss) != 1 {
		return mo.HostSystem{}, fmt.Errorf("Expected to retrieve 1 esx host from vCenter, but got %v", len(hss))
	}
	return hss[0], nil
}

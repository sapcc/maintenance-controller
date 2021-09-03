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

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	vctypes "github.com/vmware/govmomi/vim25/types"
	v1 "k8s.io/api/core/v1"
)

// Checks if the controller initiated the maintenance and the underlying ESX is not in maintenance.
func ShouldStart(node *v1.Node) bool {
	initiated, ok := node.Annotations[RebootInitiatedAnnotationKey]
	if !ok || initiated != TrueString {
		return false
	}
	state, ok := node.Labels[MaintenanceLabelKey]
	if !ok || state != string(NoMaintenance) {
		return false
	}
	return true
}

// Starts the virtual machine specified by nodeName on the ESX specified by info.
func ensureVmOn(ctx context.Context, vCenters *VCenters, info HostInfo, nodeName string) error {
	client, err := vCenters.Client(ctx, info.AvailabilityZone)
	if err != nil {
		return fmt.Errorf("Failed to connect to vCenter: %w", err)
	}
	mgr := view.NewManager(client.Client)
	view, err := mgr.CreateContainerView(ctx, client.ServiceContent.RootFolder,
		[]string{"VirtualMachine"}, true)
	if err != nil {
		return fmt.Errorf("Failed to create container view: %w", err)
	}
	var vms []mo.VirtualMachine
	err = view.RetrieveWithFilter(ctx, []string{"VirtualMachine"}, []string{"summary.runtime"},
		&vms, property.Filter{"name": nodeName})
	if err != nil {
		return fmt.Errorf("Failed to retrieve VM %v", nodeName)
	}
	if len(vms) != 1 {
		return fmt.Errorf("Expected to retrieve 1 VM from vCenter, but got %v", len(vms))
	}
	if vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn {
		return nil
	}
	vm := object.NewVirtualMachine(client.Client, vms[0].Self)
	task, err := vm.PowerOn(ctx)
	if err != nil {
		return fmt.Errorf("Failed to create poweron task for VM %v", nodeName)
	}
	taskResult, err := task.WaitForResult(ctx)
	if err != nil {
		return fmt.Errorf("Failed to await poweron task for VM %v", nodeName)
	}
	if taskResult.State != vctypes.TaskInfoStateSuccess {
		return fmt.Errorf("VM %v poweron task was not successful", nodeName)
	}
	return nil
}

// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package esx

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi/object"
	vctypes "github.com/vmware/govmomi/vim25/types"
	v1 "k8s.io/api/core/v1"

	"github.com/sapcc/maintenance-controller/constants"
)

// Checks if the controller initiated the maintenance and the underlying ESX is not in maintenance.
func ShouldStart(node *v1.Node) bool {
	initiated, ok := node.Annotations[constants.EsxRebootInitiatedAnnotationKey]
	if !ok || initiated != constants.TrueStr {
		return false
	}
	state, ok := node.Labels[constants.EsxMaintenanceLabelKey]
	if !ok || state != string(NoMaintenance) {
		return false
	}
	return true
}

// Starts the virtual machine specified by nodeName on the ESX specified by info.
func ensureVMOn(ctx context.Context, vCenters *VCenters, info HostInfo, nodeName string) error {
	client, err := vCenters.Client(ctx, info.AvailabilityZone)
	if err != nil {
		return fmt.Errorf("failed to connect to vCenter: %w", err)
	}
	movm, err := RetrieveVM(ctx, client, nodeName)
	if err != nil {
		return fmt.Errorf("failed to retrieve vm %s: %w", nodeName, err)
	}
	if movm.Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn {
		return nil
	}
	vm := object.NewVirtualMachine(client.Client, movm.Self)
	task, err := vm.PowerOn(ctx)
	if err != nil {
		return fmt.Errorf("failed to create poweron task for VM %v", nodeName)
	}
	taskResult, err := task.WaitForResultEx(ctx)
	if err != nil {
		return fmt.Errorf("failed to await poweron task for VM %v", nodeName)
	}
	if taskResult.State != vctypes.TaskInfoStateSuccess {
		return fmt.Errorf("VM %v poweron task was not successful", nodeName)
	}
	return nil
}

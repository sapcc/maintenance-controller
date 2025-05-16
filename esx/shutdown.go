// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package esx

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/soap"
	vctypes "github.com/vmware/govmomi/vim25/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/sapcc/maintenance-controller/constants"
)

// Checks, if all Nodes on an ESX need maintenance and are allowed to be shutdown.
func ShouldShutdown(esx *Host) bool {
	var initCount int
	for i := range esx.Nodes {
		node := &esx.Nodes[i]
		if ShouldShutdownNode(node) {
			initCount++
		}
	}
	return initCount == len(esx.Nodes)
}

func ShouldShutdownNode(node *v1.Node) bool {
	state := Maintenance(node.Labels[constants.EsxMaintenanceLabelKey])
	return ShutdownAllowed(state) && node.Labels[constants.EsxRebootOkLabelKey] == constants.TrueStr
}

func ShutdownAllowed(state Maintenance) bool {
	return state == InMaintenance || state == AlarmMaintenance
}

type ShutdownParams struct {
	VCenters *VCenters
	Info     HostInfo
	NodeName string
	Period   time.Duration
	Timeout  time.Duration
	Log      logr.Logger
}

func EnsureVMOff(ctx context.Context, params ShutdownParams) error {
	client, err := params.VCenters.Client(ctx, params.Info.AvailabilityZone)
	if err != nil {
		return fmt.Errorf("failed to connect to vCenter: %w", err)
	}
	movm, err := RetrieveVM(ctx, client, params.NodeName)
	if err != nil {
		return fmt.Errorf("failed to retrieve vm %s: %w", params.NodeName, err)
	}
	if movm.Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOff {
		return nil
	}

	vm := object.NewVirtualMachine(client.Client, movm.Self)
	return shutdownVM(ctx, params.Log, vm, PollPowerOffParams{
		client:   client,
		nodeName: params.NodeName,
		period:   params.Period,
		timeout:  params.Timeout,
	})
}

func shutdownVM(ctx context.Context, log logr.Logger, vm *object.VirtualMachine, params PollPowerOffParams) error {
	log = log.WithValues("node", params.nodeName)
	err := vm.ShutdownGuest(ctx)
	if err == nil {
		err = pollPowerOff(ctx, params)
		if err == nil {
			// no error => so VM is turned off
			log.Info("graceful VM shutdown succeeded")
			return nil
		}
		if !wait.Interrupted(err) {
			// not a timeout error => bubble up
			return fmt.Errorf("failed to wait for guest OS shutdown: %w", err)
		}
		// timeout error force power off
		log.Info("graceful shutdown timed out, will unplug the VM")
	} else if !isToolsUnavailable(err) {
		return fmt.Errorf("failed to shutdown guest OS for vm %s: %w", params.nodeName, err)
	}
	log.Info("unplugging VM")
	// no guest tools, continue with force power off
	task, err := vm.PowerOff(ctx)
	if err != nil {
		return fmt.Errorf("failed to create poweroff task for VM %v", params.nodeName)
	}
	taskResult, err := task.WaitForResultEx(ctx)
	if err != nil {
		return fmt.Errorf("failed to await poweroff task for VM %v", params.nodeName)
	}
	if taskResult.State != vctypes.TaskInfoStateSuccess {
		return fmt.Errorf("VM %v poweroff task was not successful", params.nodeName)
	}
	return nil
}

type PollPowerOffParams struct {
	client   *govmomi.Client
	nodeName string
	period   time.Duration
	timeout  time.Duration
}

func pollPowerOff(ctx context.Context, params PollPowerOffParams) error {
	return wait.PollWithContext(ctx, params.period, params.timeout, func(ctx context.Context) (bool, error) { //nolint:staticcheck,lll
		vm, err := RetrieveVM(ctx, params.client, params.nodeName)
		if err != nil {
			return false, err
		}
		return vm.Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOff, nil
	})
}

func isToolsUnavailable(err error) bool {
	// kindly copied from govmomi
	if soap.IsSoapFault(err) {
		soapFault := soap.ToSoapFault(err)
		if _, ok := soapFault.VimFault().(vctypes.ToolsUnavailable); ok {
			return ok
		}
	}

	return false
}

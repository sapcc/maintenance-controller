// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package esx

import (
	"context"
	"fmt"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
)

func RetrieveVM(ctx context.Context, client *govmomi.Client, name string) (mo.VirtualMachine, error) {
	mgr := view.NewManager(client.Client)
	containerView, err := mgr.CreateContainerView(ctx, client.ServiceContent.RootFolder,
		[]string{"VirtualMachine"}, true)
	if err != nil {
		return mo.VirtualMachine{}, fmt.Errorf("failed to create container view: %w", err)
	}
	var vms []mo.VirtualMachine
	err = containerView.RetrieveWithFilter(ctx, []string{"VirtualMachine"}, []string{"summary.runtime"},
		&vms, property.Match{"name": name})
	if err != nil {
		return mo.VirtualMachine{}, fmt.Errorf("failed to retrieve VM %v", name)
	}
	err = containerView.Destroy(ctx)
	if err != nil {
		return mo.VirtualMachine{}, fmt.Errorf("failed to destroy ContainerView: %w", err)
	}
	if len(vms) != 1 {
		return mo.VirtualMachine{}, fmt.Errorf("expected to retrieve 1 VM from vCenter, but got %v", len(vms))
	}
	return vms[0], nil
}

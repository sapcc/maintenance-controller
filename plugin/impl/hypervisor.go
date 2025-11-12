// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"github.com/cobaltcore-dev/openstack-hypervisor-operator/api/v1"
	"github.com/sapcc/ucfgwrap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/plugin"
)

// IsHypervisorEvicted is a check plugin, which checks whether a hypervisor is evicted.
type IsHypervisorEvicted struct{}

func (i *IsHypervisorEvicted) OnTransition(params plugin.Parameters) error {
	return nil
}

// New creates a new IsHypervisorEvicted instance with the given config.
func (i *IsHypervisorEvicted) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	return &IsHypervisorEvicted{}, nil
}

func (i *IsHypervisorEvicted) ID() string {
	return "isHypervisorEvicted"
}

// Check checks whether the hypervisor is evicted.
func (i *IsHypervisorEvicted) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	var hypervisor v1.Hypervisor
	if err := params.Client.Get(params.Ctx, types.NamespacedName{Name: params.Node.Name}, &hypervisor); err != nil {
		return plugin.Failed(nil), err
	}

	if hypervisor.Status.Evicted {
		return plugin.Passed(nil), nil
	}
	return plugin.Failed(nil), nil
}

// AlterHypervisorMaintenance is a trigger plugin, which can enable Maintenance for a hypervisor.
type AlterHypervisorMaintenance struct {
	Value string
}

// New creates a new AlterHypervisorMaintenance instance with the given config.
func (a *AlterHypervisorMaintenance) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	conf := struct {
		Value string `config:"value"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &AlterHypervisorMaintenance{Value: conf.Value}, nil
}

func (a *AlterHypervisorMaintenance) ID() string {
	return "alterHypervisorMaintenance"
}

// Trigger Alters the Maintenance field of the hypervisor Spec.
func (a *AlterHypervisorMaintenance) Trigger(params plugin.Parameters) error {
	var hypervisor v1.Hypervisor
	// Use RetryOnConflict to handle potential update conflicts since Hypervisor object is managed by multiple
	// controllers
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := params.Client.Get(params.Ctx, types.NamespacedName{Name: params.Node.Name}, &hypervisor); err != nil {
			return err
		}

		orig := hypervisor.DeepCopy()
		hypervisor.Spec.Maintenance = a.Value

		return params.Client.Patch(params.Ctx, &hypervisor, client.MergeFrom(orig))
	})
}

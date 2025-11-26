// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"github.com/sapcc/ucfgwrap"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/sapcc/maintenance-controller/plugin"
)

// AlterFinalizer is a trigger plugin, which can alter properties of the Finalizer CRO of the node.
type AlterFinalizer struct {
	Key    string
	Remove bool
}

// New creates a new AlterFinalizer instance with the given config.
func (a *AlterFinalizer) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	conf := struct {
		Key    string `config:"key" validate:"required"`
		Remove bool   `config:"remove"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &AlterFinalizer{Key: conf.Key, Remove: conf.Remove}, nil
}

func (a *AlterFinalizer) ID() string {
	return "alterFinalizer"
}

// Trigger ensures the Finalizer with the provided key is removed if removes is set to true.
// Otherwise, it sets the Finalizer with the provided key if required.
func (a *AlterFinalizer) Trigger(params plugin.Parameters) error {
	if !a.Remove {
		controllerutil.AddFinalizer(params.Node, a.Key)
		return nil
	}

	controllerutil.RemoveFinalizer(params.Node, a.Key)
	return nil
}

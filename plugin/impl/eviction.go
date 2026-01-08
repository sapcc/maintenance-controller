// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"fmt"
	"slices"
	"time"

	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/plugin"
)

const (
	Drain    EvictionAction = "drain"
	Cordon   EvictionAction = "cordon"
	Uncordon EvictionAction = "uncordon"

	defaultPeriod time.Duration = 5 * time.Second
)

var validActions = []EvictionAction{Drain, Cordon, Uncordon}

type EvictionAction string

type Eviction struct {
	Action          EvictionAction
	DeletionTimeout time.Duration
	EvictionTimeout time.Duration
	ForceEviction   bool
}

func (e *Eviction) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	conf := struct {
		Action          string        `config:"action" validate:"required"`
		DeletionTimeout time.Duration `config:"deletionTimeout"`
		EvictionTimeout time.Duration `config:"evictionTimeout"`
		ForceEviction   bool          `config:"forceEviction"`
	}{
		DeletionTimeout: 10 * time.Minute,
		EvictionTimeout: 10 * time.Minute,
	}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	if !slices.Contains(validActions, EvictionAction(conf.Action)) {
		return nil, fmt.Errorf("got invalid eviction action: %s", conf.Action)
	}
	return &Eviction{
		Action:          EvictionAction(conf.Action),
		DeletionTimeout: conf.DeletionTimeout,
		EvictionTimeout: conf.EvictionTimeout,
		ForceEviction:   conf.ForceEviction,
	}, nil
}

func (e *Eviction) ID() string {
	return "eviction"
}

func (e *Eviction) Trigger(params plugin.Parameters) error {
	switch e.Action {
	case Cordon:
		params.Node.Spec.Unschedulable = true
		return nil
	case Uncordon:
		params.Node.Spec.Unschedulable = false
		return nil
	case Drain:
		// original comment:
		// The implementation below is technically wrong.
		// Just assigning true to Unschedulable does not patch the node object on the api server.
		// This done at the end of the reconciliation loop.
		// Actively patching within this plugin increases the resource version to increase.
		// The increment causes the patch at then end of the reconciliation loop to fail,
		// which consistently drops state.
		// Assigning true to unschedulable here is a sanity action.
		// The user should configure to run the cordon action before running the drain action.
		params.Node.Spec.Unschedulable = true
		drained, err := common.EnsureDrainNonBlocking(params.Ctx, params.Node, params.Log, common.DrainParameters{
			AwaitDeletion: common.WaitParameters{
				Period:  defaultPeriod,
				Timeout: e.DeletionTimeout,
			},
			Eviction: common.WaitParameters{
				Period:  defaultPeriod,
				Timeout: e.EvictionTimeout,
			},
			Client:        params.Client,
			Clientset:     params.Clientset,
			ForceEviction: e.ForceEviction,
		})
		if err != nil {
			return err
		}
		if !drained {
			params.Log.Info("Drain still in progress; will continue in next reconcile", "node", params.Node.Name)
		}
		return nil
	}
	return fmt.Errorf("invalid eviction action: %s", e.Action)
}

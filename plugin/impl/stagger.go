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

package impl

import (
	"time"

	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/plugin"
	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Stagger is a check plugin that checks that only one node
// can pass every configurable period.
type Stagger struct {
	Duration  time.Duration
	LeaseName types.NamespacedName
}

// New creates a new Stagger instance with the given config.
func (s *Stagger) New(config *ucfg.Config) (plugin.Checker, error) {
	conf := struct {
		Duration       time.Duration `config:"duration" validate:"required"`
		LeaseName      string        `config:"leaseName" validate:"required"`
		LeaseNamespace string        `config:"leaseNamespace" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &Stagger{LeaseName: types.NamespacedName{
		Namespace: conf.LeaseNamespace,
		Name:      conf.LeaseName},
		Duration: conf.Duration}, nil
}

// Check asserts that since the last successful check is a certain time has passed.
func (s *Stagger) Check(params plugin.Parameters) (bool, error) {
	lease, err := s.getOrCreateLease(&params)
	if err != nil {
		return false, err
	}
	if time.Since(lease.Spec.RenewTime.Time) <= time.Duration(*lease.Spec.LeaseDurationSeconds)*time.Second {
		return false, nil
	}
	return true, nil
}

func (s *Stagger) getOrCreateLease(params *plugin.Parameters) (coordinationv1.Lease, error) {
	var lease coordinationv1.Lease
	err := params.Client.Get(params.Ctx, s.LeaseName, &lease)
	if err == nil {
		return lease, nil
	}
	if !errors.IsNotFound(err) {
		return coordinationv1.Lease{}, err
	}
	lease.Name = s.LeaseName.Name
	lease.Namespace = s.LeaseName.Namespace
	lease.Spec.HolderIdentity = &params.Node.Name
	// Create the lease in the past, so it can immediately pass the timeout check.
	// In AfterEval() the lease will then also receive sensible values.
	past := v1.MicroTime{
		Time: time.Now().Add(-2 * s.Duration),
	}
	lease.Spec.AcquireTime = &past
	lease.Spec.RenewTime = &past
	secs := int32(s.Duration.Seconds())
	lease.Spec.LeaseDurationSeconds = &secs
	err = params.Client.Create(params.Ctx, &lease)
	if err != nil {
		return lease, err
	}
	return lease, nil
}

// If the whole check chain passed, the lease needs to be grabed, so other nodes are blocked from progressing.
func (s *Stagger) AfterEval(chainResult bool, params plugin.Parameters) error {
	if !chainResult {
		return nil
	}
	lease := &coordinationv1.Lease{}
	err := params.Client.Get(params.Ctx, s.LeaseName, lease)
	if err != nil {
		return err
	}
	return s.grabLease(&params, lease)
}

func (s *Stagger) grabLease(params *plugin.Parameters, lease *coordinationv1.Lease) error {
	unmodified := lease.DeepCopy()
	lease.Spec.HolderIdentity = &params.Node.Name
	now := v1.MicroTime{
		Time: time.Now(),
	}
	lease.Spec.AcquireTime = &now
	lease.Spec.RenewTime = &now
	transitions := int32(0)
	if lease.Spec.LeaseTransitions != nil {
		transitions = *lease.Spec.LeaseTransitions + 1
	}
	lease.Spec.LeaseTransitions = &transitions
	return params.Client.Patch(params.Ctx, lease, client.MergeFrom(unmodified))
}

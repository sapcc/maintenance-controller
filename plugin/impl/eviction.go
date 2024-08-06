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
		return common.EnsureSchedulable(params.Ctx, params.Client, params.Node, false)
	case Uncordon:
		return common.EnsureSchedulable(params.Ctx, params.Client, params.Node, true)
	case Drain:
		if err := common.EnsureSchedulable(params.Ctx, params.Client, params.Node, false); err != nil {
			return err
		}
		return common.EnsureDrain(params.Ctx, params.Node, params.Log, common.DrainParameters{
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
	}
	return fmt.Errorf("invalid eviction action: %s", e.Action)
}

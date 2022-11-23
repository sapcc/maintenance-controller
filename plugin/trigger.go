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

package plugin

import (
	"fmt"

	"github.com/sapcc/ucfgwrap"
)

// Trigger is the interface that trigger plugins need to implement.
// It is recommend to make trigger plugins idempotent, as the same trigger might be invoked multiple times.
// A zero-initialized trigger plugin should not actually work as it is used to create
// the actual usable configured instances.
type Trigger interface {
	Trigger(params Parameters) error
	New(config *ucfgwrap.Config) (Trigger, error)
}

// TriggerInstance represents a configured and named instance of a trigger plugin.
type TriggerInstance struct {
	Plugin Trigger
	Name   string
}

// TriggerChain represents a collection of multiple TriggerInstance that can be executed one after another.
type TriggerChain struct {
	Plugins []TriggerInstance
}

// Execute invokes Trigger on each TriggerInstance in the chain and aborts when a plugin returns an error.
func (chain *TriggerChain) Execute(params Parameters) error {
	for _, trigger := range chain.Plugins {
		err := trigger.Plugin.Trigger(params)
		if err != nil {
			return &ChainError{
				Message: fmt.Sprintf("Trigger instance %v failed", trigger.Name),
				Err:     err,
			}
		}
		params.Log.Info("Executed trigger instance", "instance", trigger.Name)
	}
	return nil
}

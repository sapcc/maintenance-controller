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

	"github.com/PaesslerAG/gval"
	"github.com/elastic/go-ucfg"
)

// Checker is the interface that check plugins need to implement.
// Check plugins have to be idempotent, as they are invoked multiple times.
// A zero-initialized check plugin should not actually work as it is used
// to create the actual usable configured instances.
type Checker interface {
	Check(params Parameters) (bool, error)
	New(config *ucfg.Config) (Checker, error)
}

// CheckInstance represents a configured and named instance of a check plugin.
type CheckInstance struct {
	Plugin Checker
	Name   string
}

// CheckChain represents a collection of multiple TriggerInstance that can be executed one after another.
type CheckChain struct {
	Plugins   []CheckInstance
	Evaluable gval.Evaluable
}

// Execute invokes Trigger on each TriggerInstance in the chain and aborts when a plugin returns an error.
// It returns true if all checks passed.
func (chain *CheckChain) Execute(params Parameters) (bool, error) {
	// no checks configured
	if chain.Evaluable == nil && len(chain.Plugins) == 0 {
		return true, nil
	}
	// execute all plugins and build gval parameter map
	evalParams := make(map[string]interface{})
	for _, check := range chain.Plugins {
		result, err := check.Plugin.Check(params)
		if err != nil {
			return false, &ChainError{
				Message: fmt.Sprintf("Check instance %v failed", check.Name),
				Err:     err,
			}
		}
		evalParams[check.Name] = result
	}
	// evaluate boolean expression
	result, err := chain.Evaluable.EvalBool(params.Ctx, evalParams)
	if err != nil {
		return false, err
	}
	return result, nil
}

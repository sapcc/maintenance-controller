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
	"strings"

	"github.com/PaesslerAG/gval"
	"github.com/sapcc/ucfgwrap"
)

type CheckResult struct {
	Passed bool
	Info   map[string]any
}

func Passed(info map[string]any) CheckResult {
	return CheckResult{Passed: true, Info: info}
}

func Failed(info map[string]any) CheckResult {
	return CheckResult{Passed: false, Info: info}
}

// Checker is the interface that check plugins need to implement.
// Check plugins have to be idempotent, as they are invoked multiple times.
// A zero-initialized check plugin should not actually work as it is used
// to create the actual usable configured instances.
type Checker interface {
	Check(params Parameters) (CheckResult, error)
	New(config *ucfgwrap.Config) (Checker, error)
	// OnTransition is invoked once evaluation the CheckChain this instance is the cause for a transition.
	OnTransition(params Parameters) error
}

// CheckInstance represents a configured and named instance of a check plugin.
type CheckInstance struct {
	Plugin Checker
	Name   string
}

type CheckChainResult struct {
	Passed bool
	Info   map[string]CheckResult
}

// CheckChain represents a collection of multiple TriggerInstance that can be executed one after another.
type CheckChain struct {
	Plugins   []CheckInstance
	Evaluable gval.Evaluable
}

// Execute invokes Trigger on each TriggerInstance in the chain and aborts when a plugin returns an error.
// It returns true if all checks passed.
func (chain *CheckChain) Execute(params Parameters) (CheckChainResult, error) {
	// no checks configured
	if chain.Evaluable == nil && len(chain.Plugins) == 0 {
		return CheckChainResult{Passed: true, Info: make(map[string]CheckResult)}, nil
	}
	// execute all plugins and build gval parameter map
	evalParams := make(map[string]interface{})
	infos := make(map[string]CheckResult)
	failedInstances := make([]string, 0)
	for _, check := range chain.Plugins {
		result, err := check.Plugin.Check(params)
		if result.Info == nil {
			result.Info = make(map[string]any)
		}
		if err != nil {
			evalParams[check.Name] = false
			failedInstances = append(failedInstances, check.Name)
			result.Info["error"] = fmt.Sprintf("%s", err)
		} else {
			evalParams[check.Name] = result.Passed
		}
		infos[check.Name] = result
	}
	if params.LogDetails {
		params.Log.Info("results of check plugins", "node", params.Node.Name, "checks", evalParams)
	}
	if len(failedInstances) > 0 {
		return CheckChainResult{Passed: false, Info: infos},
			fmt.Errorf("failed check instances: %s", strings.Join(failedInstances, ", "))
	}
	// evaluate boolean expression
	result, err := chain.Evaluable.EvalBool(params.Ctx, evalParams)
	if err != nil {
		return CheckChainResult{Passed: false, Info: infos}, err
	}
	return CheckChainResult{Passed: result, Info: infos}, nil
}

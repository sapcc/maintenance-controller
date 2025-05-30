// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"strings"

	"github.com/PaesslerAG/gval"
	"github.com/sapcc/ucfgwrap"
)

type CheckResult struct {
	ID     string         `json:"id"`
	Passed bool           `json:"passed"`
	Info   map[string]any `json:"info"`
}

func Passed(info map[string]any) CheckResult {
	return CheckResult{Passed: true, Info: info}
}

func PassedWithReason(reason string) CheckResult {
	return CheckResult{Passed: true, Info: map[string]any{"reason": reason}}
}

func Failed(info map[string]any) CheckResult {
	return CheckResult{Passed: false, Info: info}
}

func FailedWithReason(reason string) CheckResult {
	return CheckResult{Passed: false, Info: map[string]any{"reason": reason}}
}

// Checker is the interface that check plugins need to implement.
// Check plugins have to be idempotent, as they are invoked multiple times.
// A zero-initialized check plugin should not actually work as it is used
// to create the actual usable configured instances.
type Checker interface {
	Check(params Parameters) (CheckResult, error)
	New(config *ucfgwrap.Config) (Checker, error)
	ID() string
	// OnTransition is invoked once evaluation the CheckChain this instance is the cause for a transition.
	OnTransition(params Parameters) error
}

// CheckInstance represents a configured and named instance of a check plugin.
type CheckInstance struct {
	Plugin Checker
	Name   string
}

type CheckChainResult struct {
	Passed     bool                   `json:"passed"`
	Info       map[string]CheckResult `json:"info"`
	Expression string                 `json:"expression"`
}

// CheckChain represents a collection of multiple TriggerInstance that can be executed one after another.
type CheckChain struct {
	Plugins    []CheckInstance
	Evaluable  gval.Evaluable
	Expression string
}

// Execute invokes Trigger on each TriggerInstance in the chain and aborts when a plugin returns an error.
// It returns true if all checks passed.
func (chain *CheckChain) Execute(params Parameters) (CheckChainResult, error) {
	result := CheckChainResult{
		Passed:     false,
		Info:       make(map[string]CheckResult),
		Expression: chain.Expression,
	}
	// no checks configured
	if chain.Evaluable == nil && len(chain.Plugins) == 0 {
		result.Passed = true
		return result, nil
	}
	// execute all plugins and build gval parameter map
	evalParams := make(map[string]interface{})
	infos := make(map[string]CheckResult)
	failedInstances := make([]string, 0)
	for _, check := range chain.Plugins {
		result, err := check.Plugin.Check(params)
		result.ID = check.Plugin.ID()
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
	result.Info = infos
	if len(failedInstances) > 0 {
		return result,
			fmt.Errorf("failed check instances: %s", strings.Join(failedInstances, ", "))
	}
	// evaluate boolean expression
	eval, err := chain.Evaluable.EvalBool(params.Ctx, evalParams)
	if err != nil {
		return result, err
	}
	result.Passed = eval
	return result, nil
}

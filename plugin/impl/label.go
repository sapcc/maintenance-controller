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
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
)

// HasLabel is a check plugin that checks whether a node has a label or a label with a certain value.
type HasLabel struct {
	Key   string
	Value string
}

// New creates a new HasLabel instance with the given config.
func (h *HasLabel) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Key   string `config:"key" validate:"required"`
		Value string `config:"value"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &HasLabel{Key: conf.Key, Value: conf.Value}, nil
}

// Check checks whether a node has a label (if h.Value == "") or a label with a certain value (if h.Value != "").
func (h *HasLabel) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	val, ok := params.Node.Labels[h.Key]
	if !ok {
		return plugin.Failed(nil), nil
	}
	if h.Value == "" {
		return plugin.Passed(nil), nil
	}
	return plugin.CheckResult{Passed: val == h.Value}, nil
}

func (h *HasLabel) OnTransition(params plugin.Parameters) error {
	return nil
}

type AnyLabel struct {
	Key   string
	Value string
}

func (a *AnyLabel) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Key   string `config:"key" validate:"required"`
		Value string `config:"value"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &AnyLabel{Key: conf.Key, Value: conf.Value}, nil
}

func (a *AnyLabel) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	var nodes v1.NodeList
	err := params.Client.List(params.Ctx, &nodes)
	if err != nil {
		return plugin.Failed(nil), err
	}
	for _, node := range nodes.Items {
		val, ok := node.Labels[a.Key]
		if !ok {
			continue
		}
		if a.Value == "" || a.Value == val {
			return plugin.Passed(nil), nil
		}
	}
	return plugin.Failed(nil), nil
}

func (a *AnyLabel) OnTransition(params plugin.Parameters) error {
	return nil
}

// AlterLabel is a trigger plugin, which can add, change or remove a label.
type AlterLabel struct {
	Key    string
	Value  string
	Remove bool
}

// New creates a new AlterLabel instance with the given config.
func (a *AlterLabel) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	conf := struct {
		Key    string `config:"key" validate:"required"`
		Value  string `config:"value"`
		Remove bool   `config:"remove"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &AlterLabel{Key: conf.Key, Remove: conf.Remove, Value: conf.Value}, nil
}

// Trigger ensures the label with the provided key is removed if removes is set to true.
// Otherwise it sets the label with the provided key to the provided value adding the label if required.
func (a *AlterLabel) Trigger(params plugin.Parameters) error {
	_, ok := params.Node.Labels[a.Key]
	if !a.Remove {
		params.Node.Labels[a.Key] = a.Value
		return nil
	}
	if ok {
		delete(params.Node.Labels, a.Key)
	}
	return nil
}

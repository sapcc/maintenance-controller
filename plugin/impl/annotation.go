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
	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/plugin"
)

// HasAnnotation is a check plugin that checks whether a node has an annotation or an annotation with a certain value.
type HasAnnotation struct {
	Key   string
	Value string
}

// New creates a new HasAnnotation instance with the given config.
func (h *HasAnnotation) New(config *ucfg.Config) (plugin.Checker, error) {
	conf := struct {
		Key   string `config:"key" validate:"required"`
		Value string `config:"value"`
	}{}
	err := config.Unpack(&conf)
	if err != nil {
		return nil, err
	}
	return &HasAnnotation{Key: conf.Key, Value: conf.Value}, nil
}

// Check checks whether a node has an annotation (if h.Value == "")
// or an annotation with a certain value (if h.Value != "").
func (h *HasAnnotation) Check(params plugin.Parameters) (bool, error) {
	val, ok := params.Node.Annotations[h.Key]
	if !ok {
		return false, nil
	}
	if h.Value == "" {
		return true, nil
	}
	return val == h.Value, nil
}

func (h *HasAnnotation) AfterEval(chainResult bool, params plugin.Parameters) error {
	return nil
}

// AlterAnnotation is a trigger plugin, which can add, change or remove an annotation.
type AlterAnnotation struct {
	Key    string
	Value  string
	Remove bool
}

// New creates a new AlterAnnotation instance with the given config.
func (a *AlterAnnotation) New(config *ucfg.Config) (plugin.Trigger, error) {
	conf := struct {
		Key    string `config:"key" validate:"required"`
		Value  string `config:"value"`
		Remove bool   `config:"remove"`
	}{}
	err := config.Unpack(&conf)
	if err != nil {
		return nil, err
	}
	return &AlterAnnotation{Key: conf.Key, Remove: conf.Remove, Value: conf.Value}, nil
}

// Trigger ensures the annotation with the provided key is removed if removes is set to true.
// Otherwise it sets the annotation with the provided key to the provided value adding the annotation if required.
func (a *AlterAnnotation) Trigger(params plugin.Parameters) error {
	_, ok := params.Node.Annotations[a.Key]
	if !a.Remove {
		params.Node.Annotations[a.Key] = a.Value
		return nil
	}
	if ok {
		delete(params.Node.Annotations, a.Key)
	}
	return nil
}

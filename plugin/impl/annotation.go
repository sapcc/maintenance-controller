// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"fmt"

	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

// HasAnnotation is a check plugin that checks whether a node has an annotation or an annotation with a certain value.
type HasAnnotation struct {
	Key   string
	Value string
}

// New creates a new HasAnnotation instance with the given config.
func (h *HasAnnotation) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Key   string `config:"key" validate:"required"`
		Value string `config:"value"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &HasAnnotation{Key: conf.Key, Value: conf.Value}, nil
}

func (h *HasAnnotation) ID() string {
	return "hasAnnotation"
}

// Check checks whether a node has an annotation (if h.Value == "")
// or an annotation with a certain value (if h.Value != "").
func (h *HasAnnotation) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	val, ok := params.Node.Annotations[h.Key]
	if !ok {
		return plugin.FailedWithReason(fmt.Sprintf("annotation %s not present", h.Key)), nil
	}
	if h.Value == "" {
		return plugin.Passed(nil), nil
	}
	return plugin.CheckResult{Passed: val == h.Value}, nil
}

func (h *HasAnnotation) OnTransition(params plugin.Parameters) error {
	return nil
}

// AlterAnnotation is a trigger plugin, which can add, change or remove an annotation.
type AlterAnnotation struct {
	Key    string
	Value  string
	Remove bool
}

// New creates a new AlterAnnotation instance with the given config.
func (a *AlterAnnotation) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	conf := struct {
		Key    string `config:"key" validate:"required"`
		Value  string `config:"value"`
		Remove bool   `config:"remove"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &AlterAnnotation{Key: conf.Key, Remove: conf.Remove, Value: conf.Value}, nil
}

func (a *AlterAnnotation) ID() string {
	return "alterAnnotation"
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

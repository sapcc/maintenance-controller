// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"fmt"
	"reflect"
	"strings"

	v1 "github.com/cobaltcore-dev/openstack-hypervisor-operator/api/v1"
	"github.com/sapcc/ucfgwrap"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/plugin"
)

// CheckHypervisor is a check plugin, which checks properties of the hypervisor status CRO of the node.
type CheckHypervisor struct {
	Fields *map[string]any
}

func (i *CheckHypervisor) OnTransition(params plugin.Parameters) error {
	return nil
}

// New creates a new CheckHypervisor instance with the given config.
func (i *CheckHypervisor) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Fields map[string]any `config:",inline"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}

	mappedFields, err := mapToStructFields(conf.Fields, reflect.TypeFor[v1.HypervisorStatus]())
	if err != nil {
		return nil, err
	}
	return &CheckHypervisor{&mappedFields}, nil
}

func (i *CheckHypervisor) ID() string {
	return "checkHypervisor"
}

// Check checks whether the hypervisor is evicted.
func (i *CheckHypervisor) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	var hypervisor v1.Hypervisor
	if err := params.Client.Get(params.Ctx, types.NamespacedName{Name: params.Node.Name}, &hypervisor); err != nil {
		return plugin.Failed(nil), err
	}

	for key, value := range *i.Fields {
		s := reflect.ValueOf(&hypervisor.Status).Elem()
		f := s.FieldByName(key)
		if !f.IsValid() {
			return plugin.Failed(nil), fmt.Errorf("field %s not found in Hypervisor status", key)
		}

		switch v := value.(type) {
		case bool:
			if f.Bool() != v {
				return plugin.Failed(nil), nil
			}
		case string:
			if f.String() != v {
				return plugin.Failed(nil), nil
			}
		case []string:
			fieldSlice, ok := f.Interface().([]string)
			if !ok {
				return plugin.Failed(nil), fmt.Errorf("field %s is not of type []string", key)
			}
			if !reflect.DeepEqual(fieldSlice, v) {
				return plugin.Failed(nil), nil
			}
		default:
			return plugin.Failed(nil), fmt.Errorf("unsupported field type %T for field %s", value, key)
		}
	}

	return plugin.Passed(nil), nil
}

// AlterHypervisor is a trigger plugin, which can alter properties of the hypervisor CRO of the node.
type AlterHypervisor struct {
	Fields *map[string]any
}

// New creates a new AlterHypervisor instance with the given config.
func (a *AlterHypervisor) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	// Config is based on a string map because we need to know exactly which fields to alter at runtime.
	conf := struct {
		Fields map[string]any `config:",inline"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}

	mappedFields, err := mapToStructFields(conf.Fields, reflect.TypeFor[v1.HypervisorSpec]())
	if err != nil {
		return nil, err
	}
	return &AlterHypervisor{&mappedFields}, nil
}

func (a *AlterHypervisor) ID() string {
	return "alterHypervisor"
}

// Trigger Alters the Maintenance field of the hypervisor Spec.
func (a *AlterHypervisor) Trigger(params plugin.Parameters) error {
	var hypervisor v1.Hypervisor
	// Use RetryOnConflict to handle potential update conflicts since Hypervisor object is managed by multiple
	// controllers
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := params.Client.Get(params.Ctx, types.NamespacedName{Name: params.Node.Name}, &hypervisor); err != nil {
			return err
		}

		orig := hypervisor.DeepCopy()
		s := reflect.ValueOf(&hypervisor.Spec).Elem()
		for key, value := range *a.Fields {
			f := s.FieldByName(key)
			if !f.IsValid() {
				return fmt.Errorf("field %s not found in Hypervisor spec", key)
			}
			if !f.CanSet() {
				return fmt.Errorf("cannot set field %s in Hypervisor spec", key)
			}

			switch v := value.(type) {
			case bool:
				f.SetBool(v)
			case string:
				f.SetString(v)
			case []string:
				f.Set(reflect.ValueOf(v))
			default:
				return fmt.Errorf("unsupported field type %T for field %s", value, key)
			}
		}

		return params.Client.Patch(params.Ctx, &hypervisor, client.MergeFrom(orig))
	})
}

// mapToStructFields maps the provided fields to the struct field names based on the json tags.
func mapToStructFields(fields map[string]any, t reflect.Type) (map[string]any, error) {
	var structFields = make(map[string]any, len(fields))

	// Validate that all provided fields exist in HypervisorSpec and map json tag names to struct field names
	for key := range fields {
		found := false
		for i := range t.NumField() {
			f := t.Field(i)
			jsonName := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
			if key == jsonName {
				structFields[f.Name] = fields[key]
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("field %s not found in Hypervisor spec", key)
		}
	}
	return structFields, nil
}

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/plugin"
)

// celExprPrefix and celExprSuffix delimit a CEL expression in config fields.
const (
	celExprPrefix = "{{="
	celExprSuffix = "}}"
)

// reflectMapType is reflect.Type for map[string]any, used by evalMap's ConvertToNative.
var reflectMapType = reflect.TypeFor[map[string]any]()

// celField holds either a plain string value or a compiled CEL program.
type celField struct {
	raw     string
	plain   string
	program cel.Program
}

// parseCELField parses a config field value. If the value is wrapped in {{= }},
// it is compiled as a CEL expression against the given environment. Otherwise it
// is treated as a literal string.
func parseCELField(raw string, env *cel.Env) (celField, error) {
	if strings.HasPrefix(raw, celExprPrefix) && strings.HasSuffix(raw, celExprSuffix) {
		expr := strings.TrimSpace(raw[len(celExprPrefix) : len(raw)-len(celExprSuffix)])
		ast, issues := env.Compile(expr)
		if issues != nil && issues.Err() != nil {
			return celField{}, fmt.Errorf("CEL compilation error: %w", issues.Err())
		}
		prog, err := env.Program(ast)
		if err != nil {
			return celField{}, fmt.Errorf("CEL program error: %w", err)
		}
		return celField{raw: raw, program: prog}, nil
	}
	return celField{raw: raw, plain: raw}, nil
}

// evalString evaluates the field as a string. For plain fields the literal value
// is returned. For CEL fields the expression is evaluated and the result must be
// a string.
func (f *celField) evalString(input map[string]any) (string, error) {
	if f.program == nil {
		return f.plain, nil
	}
	out, _, err := f.program.Eval(input)
	if err != nil {
		return "", fmt.Errorf("CEL evaluation error: %w", err)
	}
	s, ok := out.Value().(string)
	if !ok {
		return "", fmt.Errorf("CEL expression did not return a string, got %T", out.Value())
	}
	return s, nil
}

// evalBool evaluates the field as a boolean. The field must be a CEL expression.
func (f *celField) evalBool(input map[string]any) (bool, error) {
	if f.program == nil {
		return false, errors.New("field is not a CEL expression")
	}
	out, _, err := f.program.Eval(input)
	if err != nil {
		return false, fmt.Errorf("CEL evaluation error: %w", err)
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("CEL expression did not return a bool, got %T", out.Value())
	}
	return b, nil
}

// evalMap evaluates the field as a map[string]any. The field must be a CEL expression.
// CEL's ConvertToNative only converts the top-level map; nested values may remain
// as CEL ref.Val types, so we recursively convert all values to native Go types.
func (f *celField) evalMap(input map[string]any) (map[string]any, error) {
	if f.program == nil {
		return nil, errors.New("field is not a CEL expression")
	}
	out, _, err := f.program.Eval(input)
	if err != nil {
		return nil, fmt.Errorf("CEL evaluation error: %w", err)
	}
	v, err := out.ConvertToNative(reflectMapType)
	if err != nil {
		return nil, fmt.Errorf("CEL expression result cannot be converted to map: %w", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("CEL expression did not return a map, got %T", v)
	}
	return convertCELMap(m), nil
}

// convertCELMap recursively converts a map that may contain CEL ref.Val types
// into plain Go types (map[string]any, []any, string, bool, int64, etc.).
func convertCELMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = convertCELValue(v)
	}
	return result
}

// convertCELValue converts a single value that may be a CEL ref.Val into a native Go value.
// CEL's ConvertToNative only converts the outermost map to map[string]any — nested maps
// may remain as map[ref.Val]ref.Val (or other reflect.Map types). We use reflection to
// detect and convert arbitrary map and slice types recursively.
func convertCELValue(v any) any {
	if v == nil {
		return nil
	}

	// Fast path: already-native types
	switch val := v.(type) {
	case map[string]any:
		return convertCELMap(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = convertCELValue(item)
		}
		return result
	case string, bool, int64, float64, uint64:
		return v
	}

	// Try the ref.Val interface for CEL wrapper types (e.g. types.String, types.Bool)
	if refVal, ok := v.(interface{ Value() any }); ok {
		return convertCELValue(refVal.Value())
	}

	// Reflection path: handle map[ref.Val]ref.Val and similar exotic map types
	rv := reflect.ValueOf(v)
	switch rv.Kind() { //nolint:exhaustive
	case reflect.Map:
		result := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := convertCELValue(iter.Key().Interface())
			keyStr, ok := key.(string)
			if !ok {
				keyStr = fmt.Sprintf("%v", key)
			}
			result[keyStr] = convertCELValue(iter.Value().Interface())
		}
		return result
	case reflect.Slice:
		result := make([]any, rv.Len())
		for i := range rv.Len() {
			result[i] = convertCELValue(rv.Index(i).Interface())
		}
		return result
	}

	return v
}

// nodeToMap converts a Kubernetes Node to an unstructured map.
func nodeToMap(node *corev1.Node) (map[string]any, error) {
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(node)
	if err != nil {
		return nil, fmt.Errorf("failed to convert node to unstructured: %w", err)
	}
	return m, nil
}

// deepMerge recursively merges src into dst. For overlapping keys where both
// values are maps, the merge recurses. Otherwise src values overwrite dst values.
// Non-overlapping keys are preserved from both sides.
func deepMerge(dst, src map[string]any) map[string]any {
	for k, srcVal := range src {
		dstVal, exists := dst[k]
		if !exists {
			dst[k] = srcVal
			continue
		}
		dstMap, dstIsMap := dstVal.(map[string]any)
		srcMap, srcIsMap := srcVal.(map[string]any)
		if dstIsMap && srcIsMap {
			dst[k] = deepMerge(dstMap, srcMap)
		} else {
			dst[k] = srcVal
		}
	}
	return dst
}

// newRefEnv creates a CEL environment with only a "node" variable (dyn type).
// Used for resolving name/namespace fields.
func newRefEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("node", cel.DynType),
		ext.Strings(),
		ext.Lists(),
		ext.Sets(),
		ext.Math(),
		ext.Encoders(),
	)
}

// newEvalEnv creates a CEL environment with "node" and "object" variables (dyn type).
// Used for check/modify expressions.
func newEvalEnv() (*cel.Env, error) {
	return cel.NewEnv(
		cel.Variable("node", cel.DynType),
		cel.Variable("object", cel.DynType),
		ext.Strings(),
		ext.Lists(),
		ext.Sets(),
		ext.Math(),
		ext.Encoders(),
	)
}

// CheckDynamicResource is a check plugin that fetches an arbitrary Kubernetes
// object using GVK and evaluates a CEL expression against it.
type CheckDynamicResource struct {
	group     string
	version   string
	kind      string
	namespace celField
	name      celField
	check     celField
}

// New creates a new CheckDynamicResource instance with the given config.
func (c *CheckDynamicResource) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Group     string `config:"group"`
		Version   string `config:"version" validate:"required"`
		Kind      string `config:"kind" validate:"required"`
		Namespace string `config:"namespace"`
		Name      string `config:"name" validate:"required"`
		Check     string `config:"check" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}

	// Validate that check uses CEL syntax
	if !strings.HasPrefix(conf.Check, celExprPrefix) || !strings.HasSuffix(conf.Check, celExprSuffix) {
		return nil, errors.New("check field must be a CEL expression wrapped in {{= }}")
	}

	refEnv, err := newRefEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL ref environment: %w", err)
	}
	evalEnv, err := newEvalEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL eval environment: %w", err)
	}

	nameField, err := parseCELField(conf.Name, refEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse name field: %w", err)
	}
	var nsField celField
	if conf.Namespace != "" {
		nsField, err = parseCELField(conf.Namespace, refEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to parse namespace field: %w", err)
		}
	}
	checkField, err := parseCELField(conf.Check, evalEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse check field: %w", err)
	}

	return &CheckDynamicResource{
		group:     conf.Group,
		version:   conf.Version,
		kind:      conf.Kind,
		namespace: nsField,
		name:      nameField,
		check:     checkField,
	}, nil
}

// ID returns the plugin identifier.
func (c *CheckDynamicResource) ID() string {
	return "checkDynamicResource"
}

// Check fetches the target Kubernetes object and evaluates the CEL check expression.
func (c *CheckDynamicResource) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	nodeMap, err := nodeToMap(params.Node)
	if err != nil {
		return plugin.Failed(nil), err
	}
	refInput := map[string]any{"node": nodeMap}

	name, err := c.name.evalString(refInput)
	if err != nil {
		return plugin.Failed(nil), fmt.Errorf("failed to resolve name: %w", err)
	}
	namespace, err := c.namespace.evalString(refInput)
	if err != nil {
		return plugin.Failed(nil), fmt.Errorf("failed to resolve namespace: %w", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   c.group,
		Version: c.version,
		Kind:    c.kind,
	})

	if err := params.Client.Get(params.Ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}, obj); err != nil {
		return plugin.Failed(nil), err
	}

	evalInput := map[string]any{
		"node":   nodeMap,
		"object": obj.UnstructuredContent(),
	}
	passed, err := c.check.evalBool(evalInput)
	if err != nil {
		return plugin.Failed(nil), err
	}
	if passed {
		return plugin.Passed(nil), nil
	}
	return plugin.Failed(nil), nil
}

// OnTransition is a no-op for this plugin.
func (c *CheckDynamicResource) OnTransition(params plugin.Parameters) error {
	return nil
}

// AlterDynamicResource is a trigger plugin that fetches an arbitrary Kubernetes
// object using GVK, evaluates a CEL expression to produce a merge fragment,
// and patches the object.
type AlterDynamicResource struct {
	group     string
	version   string
	kind      string
	namespace celField
	name      celField
	modify    celField
}

// New creates a new AlterDynamicResource instance with the given config.
func (a *AlterDynamicResource) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	conf := struct {
		Group     string `config:"group"`
		Version   string `config:"version" validate:"required"`
		Kind      string `config:"kind" validate:"required"`
		Namespace string `config:"namespace"`
		Name      string `config:"name" validate:"required"`
		Modify    string `config:"modify" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}

	// Validate that modify uses CEL syntax
	if !strings.HasPrefix(conf.Modify, celExprPrefix) || !strings.HasSuffix(conf.Modify, celExprSuffix) {
		return nil, errors.New("modify field must be a CEL expression wrapped in {{= }}")
	}

	refEnv, err := newRefEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL ref environment: %w", err)
	}
	evalEnv, err := newEvalEnv()
	if err != nil {
		return nil, fmt.Errorf("failed to create CEL eval environment: %w", err)
	}

	nameField, err := parseCELField(conf.Name, refEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse name field: %w", err)
	}
	var nsField celField
	if conf.Namespace != "" {
		nsField, err = parseCELField(conf.Namespace, refEnv)
		if err != nil {
			return nil, fmt.Errorf("failed to parse namespace field: %w", err)
		}
	}
	modifyField, err := parseCELField(conf.Modify, evalEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to parse modify field: %w", err)
	}

	return &AlterDynamicResource{
		group:     conf.Group,
		version:   conf.Version,
		kind:      conf.Kind,
		namespace: nsField,
		name:      nameField,
		modify:    modifyField,
	}, nil
}

// ID returns the plugin identifier.
func (a *AlterDynamicResource) ID() string {
	return "alterDynamicResource"
}

// Trigger fetches the target Kubernetes object, evaluates the CEL modify
// expression to produce a merge fragment, deep-merges it into the object,
// and patches with optimistic locking.
func (a *AlterDynamicResource) Trigger(params plugin.Parameters) error {
	nodeMap, err := nodeToMap(params.Node)
	if err != nil {
		return err
	}
	refInput := map[string]any{"node": nodeMap}

	name, err := a.name.evalString(refInput)
	if err != nil {
		return fmt.Errorf("failed to resolve name: %w", err)
	}
	namespace, err := a.namespace.evalString(refInput)
	if err != nil {
		return fmt.Errorf("failed to resolve namespace: %w", err)
	}

	key := types.NamespacedName{Namespace: namespace, Name: name}
	gvk := schema.GroupVersionKind{Group: a.group, Version: a.version, Kind: a.kind}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk)
		if err := params.Client.Get(params.Ctx, key, obj); err != nil {
			return err
		}

		orig := obj.DeepCopy()

		evalInput := map[string]any{
			"node":   nodeMap,
			"object": obj.UnstructuredContent(),
		}
		fragment, err := a.modify.evalMap(evalInput)
		if err != nil {
			return err
		}

		deepMerge(obj.UnstructuredContent(), fragment)

		return params.Client.Patch(params.Ctx, obj,
			client.MergeFromWithOptions(orig, client.MergeFromWithOptimisticLock{}))
	})
}

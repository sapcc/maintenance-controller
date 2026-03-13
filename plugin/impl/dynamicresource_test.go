// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The dynamic resource plugins", func() {

	Describe("deepMerge", func() {
		It("merges non-overlapping keys", func() {
			dst := map[string]any{"a": "1"}
			src := map[string]any{"b": "2"}
			result := deepMerge(dst, src)
			Expect(result).To(Equal(map[string]any{"a": "1", "b": "2"}))
		})

		It("overwrites leaves on conflict", func() {
			dst := map[string]any{"a": "old"}
			src := map[string]any{"a": "new"}
			result := deepMerge(dst, src)
			Expect(result).To(Equal(map[string]any{"a": "new"}))
		})

		It("merges nested maps recursively", func() {
			dst := map[string]any{
				"data": map[string]any{"x": "1", "y": "2"},
			}
			src := map[string]any{
				"data": map[string]any{"y": "changed", "z": "3"},
			}
			result := deepMerge(dst, src)
			Expect(result).To(Equal(map[string]any{
				"data": map[string]any{"x": "1", "y": "changed", "z": "3"},
			}))
		})

		It("overwrites non-map with map", func() {
			dst := map[string]any{"data": "string-value"}
			src := map[string]any{"data": map[string]any{"key": "val"}}
			result := deepMerge(dst, src)
			Expect(result).To(Equal(map[string]any{
				"data": map[string]any{"key": "val"},
			}))
		})
	})

	Describe("celField parsing", func() {
		It("returns a plain field for non-CEL values", func() {
			env, err := newRefEnv()
			Expect(err).To(Succeed())

			field, err := parseCELField("plain-string", env)
			Expect(err).To(Succeed())
			Expect(field.program).To(BeNil())
			Expect(field.plain).To(Equal("plain-string"))
		})

		It("compiles a CEL expression wrapped in {{= }}", func() {
			env, err := newRefEnv()
			Expect(err).To(Succeed())

			field, err := parseCELField("{{= node.metadata.name }}", env)
			Expect(err).To(Succeed())
			Expect(field.program).ToNot(BeNil())
			Expect(field.plain).To(BeEmpty())
		})

		It("rejects invalid CEL expressions", func() {
			env, err := newRefEnv()
			Expect(err).To(Succeed())

			_, err = parseCELField("{{= !!!invalid!! }}", env)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CEL compilation error"))
		})

		It("evaluates a CEL expression to a string", func() {
			env, err := newRefEnv()
			Expect(err).To(Succeed())

			field, err := parseCELField("{{= node.metadata.name }}", env)
			Expect(err).To(Succeed())

			input := map[string]any{
				"node": map[string]any{
					"metadata": map[string]any{
						"name": "test-node",
					},
				},
			}
			result, err := field.evalString(input)
			Expect(err).To(Succeed())
			Expect(result).To(Equal("test-node"))
		})

		It("returns plain value for evalString on non-CEL field", func() {
			env, err := newRefEnv()
			Expect(err).To(Succeed())

			field, err := parseCELField("literal-value", env)
			Expect(err).To(Succeed())

			result, err := field.evalString(nil)
			Expect(err).To(Succeed())
			Expect(result).To(Equal("literal-value"))
		})

		It("evaluates a CEL expression to a bool", func() {
			env, err := newEvalEnv()
			Expect(err).To(Succeed())

			field, err := parseCELField("{{= object.data.ready == \"true\" }}", env)
			Expect(err).To(Succeed())

			input := map[string]any{
				"node":   map[string]any{},
				"object": map[string]any{"data": map[string]any{"ready": "true"}},
			}
			result, err := field.evalBool(input)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
		})

		It("returns error for evalBool on non-bool CEL result", func() {
			env, err := newEvalEnv()
			Expect(err).To(Succeed())

			field, err := parseCELField("{{= \"not-a-bool\" }}", env)
			Expect(err).To(Succeed())

			_, err = field.evalBool(map[string]any{"node": map[string]any{}, "object": map[string]any{}})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("did not return a bool"))
		})

		It("evaluates a CEL expression to a map", func() {
			env, err := newEvalEnv()
			Expect(err).To(Succeed())

			field, err := parseCELField(`{{= {"data": {"key": "value"}} }}`, env)
			Expect(err).To(Succeed())

			input := map[string]any{
				"node":   map[string]any{},
				"object": map[string]any{},
			}
			result, err := field.evalMap(input)
			Expect(err).To(Succeed())
			Expect(result).To(Equal(map[string]any{
				"data": map[string]any{"key": "value"},
			}))
		})
	})

	Describe("CheckDynamicResource", func() {
		var k8sClient client.Client
		var testNode *corev1.Node

		BeforeEach(func() {
			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			testNode = &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
		})

		It("parses valid config with plain name and namespace", func() {
			configStr := `version: v1
kind: ConfigMap
namespace: kube-system
name: my-config
check: "{{= object.data.ready == \"true\" }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			_, err = base.New(&config)
			Expect(err).To(Succeed())
		})

		It("parses valid config with CEL name", func() {
			configStr := `version: v1
kind: ConfigMap
namespace: kube-system
name: "{{= node.metadata.name }}"
check: "{{= object.data.ready == \"true\" }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			_, err = base.New(&config)
			Expect(err).To(Succeed())
		})

		It("rejects config without required fields", func() {
			configStr := `version: v1`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			_, err = base.New(&config)
			Expect(err).To(HaveOccurred())
		})

		It("rejects check field without CEL delimiters", func() {
			configStr := `version: v1
kind: ConfigMap
name: my-config
check: not-a-cel-expression
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			_, err = base.New(&config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CEL expression"))
		})

		It("rejects invalid CEL in check field", func() {
			configStr := `version: v1
kind: ConfigMap
name: my-config
check: "{{= !!!invalid }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			_, err = base.New(&config)
			Expect(err).To(HaveOccurred())
		})

		It("passes when check expression evaluates to true", func(ctx SpecContext) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"ready": "true",
				},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			configStr := `version: v1
kind: ConfigMap
namespace: default
name: my-config
check: "{{= object.data.ready == \"true\" }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			checker, err := base.New(&config)
			Expect(err).To(Succeed())

			result, err := checker.Check(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("fails when check expression evaluates to false", func(ctx SpecContext) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"ready": "false",
				},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			configStr := `version: v1
kind: ConfigMap
namespace: default
name: my-config
check: "{{= object.data.ready == \"true\" }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			checker, err := base.New(&config)
			Expect(err).To(Succeed())

			result, err := checker.Check(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("returns error when object is not found", func(ctx SpecContext) {
			configStr := `version: v1
kind: ConfigMap
namespace: default
name: nonexistent
check: "{{= true }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			checker, err := base.New(&config)
			Expect(err).To(Succeed())

			result, err := checker.Check(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(HaveOccurred())
			Expect(result.Passed).To(BeFalse())
		})

		It("resolves name from node via CEL", func(ctx SpecContext) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node",
					Namespace: "default",
				},
				Data: map[string]string{
					"status": "ok",
				},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			configStr := `version: v1
kind: ConfigMap
namespace: default
name: "{{= node.metadata.name }}"
check: "{{= object.data.status == \"ok\" }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			checker, err := base.New(&config)
			Expect(err).To(Succeed())

			result, err := checker.Check(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("works with group/version/kind for non-core resources", func(ctx SpecContext) {
			// Create an unstructured object with a custom GVK
			obj := &unstructured.Unstructured{
				Object: map[string]any{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]any{
						"name":      "coredns",
						"namespace": "kube-system",
					},
					"status": map[string]any{
						"availableReplicas": int64(2),
					},
				},
			}
			// Register the GVK with the scheme so the fake client can handle it
			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			scheme.AddKnownTypeWithName(
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				&unstructured.Unstructured{},
			)
			scheme.AddKnownTypeWithName(
				schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DeploymentList"},
				&unstructured.UnstructuredList{},
			)
			k8sClient = fake.NewClientBuilder().WithScheme(scheme).WithObjects(obj).Build()

			configStr := `group: apps
version: v1
kind: Deployment
namespace: kube-system
name: coredns
check: "{{= int(object.status.availableReplicas) >= 2 }}"
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base CheckDynamicResource
			checker, err := base.New(&config)
			Expect(err).To(Succeed())

			result, err := checker.Check(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("has correct ID", func() {
			c := &CheckDynamicResource{}
			Expect(c.ID()).To(Equal("checkDynamicResource"))
		})

		It("OnTransition is a no-op", func() {
			c := &CheckDynamicResource{}
			err := c.OnTransition(plugin.Parameters{})
			Expect(err).To(Succeed())
		})
	})

	Describe("AlterDynamicResource", func() {
		var k8sClient client.Client
		var testNode *corev1.Node

		BeforeEach(func() {
			scheme := runtime.NewScheme()
			Expect(corev1.AddToScheme(scheme)).To(Succeed())
			k8sClient = fake.NewClientBuilder().WithScheme(scheme).Build()
			testNode = &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
		})

		It("parses valid config", func() {
			configStr := `version: v1
kind: ConfigMap
namespace: default
name: my-config
modify: '{{= {"data": {"key": "value"}} }}'
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base AlterDynamicResource
			_, err = base.New(&config)
			Expect(err).To(Succeed())
		})

		It("rejects config without required fields", func() {
			configStr := `version: v1`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base AlterDynamicResource
			_, err = base.New(&config)
			Expect(err).To(HaveOccurred())
		})

		It("rejects modify field without CEL delimiters", func() {
			configStr := `version: v1
kind: ConfigMap
name: my-config
modify: not-a-cel-expression
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base AlterDynamicResource
			_, err = base.New(&config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("CEL expression"))
		})

		It("applies merge fragment to object", func(ctx SpecContext) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-config",
					Namespace: "default",
				},
				Data: map[string]string{
					"existing": "value",
				},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			configStr := `version: v1
kind: ConfigMap
namespace: default
name: my-config
modify: '{{= {"data": {"node": node.metadata.name}} }}'
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base AlterDynamicResource
			trigger, err := base.New(&config)
			Expect(err).To(Succeed())

			err = trigger.Trigger(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(Succeed())

			// Verify the object was updated
			updated := &unstructured.Unstructured{}
			updated.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: "default",
				Name:      "my-config",
			}, updated)).To(Succeed())

			data, found, err := unstructured.NestedStringMap(updated.Object, "data")
			Expect(err).To(Succeed())
			Expect(found).To(BeTrue())
			Expect(data["node"]).To(Equal("test-node"))
		})

		It("returns error when object is not found", func(ctx SpecContext) {
			configStr := `version: v1
kind: ConfigMap
namespace: default
name: nonexistent
modify: '{{= {"data": {"key": "value"}} }}'
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base AlterDynamicResource
			trigger, err := base.New(&config)
			Expect(err).To(Succeed())

			err = trigger.Trigger(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(HaveOccurred())
		})

		It("resolves name from node via CEL", func(ctx SpecContext) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-node",
					Namespace: "default",
				},
				Data: map[string]string{},
			}
			Expect(k8sClient.Create(ctx, cm)).To(Succeed())

			configStr := `version: v1
kind: ConfigMap
namespace: default
name: "{{= node.metadata.name }}"
modify: '{{= {"data": {"modified": "yes"}} }}'
`
			config, err := ucfgwrap.FromYAML([]byte(configStr))
			Expect(err).To(Succeed())

			var base AlterDynamicResource
			trigger, err := base.New(&config)
			Expect(err).To(Succeed())

			err = trigger.Trigger(plugin.Parameters{
				Client: k8sClient,
				Ctx:    ctx,
				Node:   testNode,
			})
			Expect(err).To(Succeed())

			// Verify the object was updated
			updated := &unstructured.Unstructured{}
			updated.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"})
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: "default",
				Name:      "test-node",
			}, updated)).To(Succeed())

			data, found, err := unstructured.NestedStringMap(updated.Object, "data")
			Expect(err).To(Succeed())
			Expect(found).To(BeTrue())
			Expect(data["modified"]).To(Equal("yes"))
		})

		It("has correct ID", func() {
			a := &AlterDynamicResource{}
			Expect(a.ID()).To(Equal("alterDynamicResource"))
		})
	})
})

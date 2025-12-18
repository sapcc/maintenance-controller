// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"reflect"

	kvmv1 "github.com/cobaltcore-dev/openstack-hypervisor-operator/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("The hypervisor plugin", func() {
	var k8sclient client.Client
	var testNode = &v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}

	BeforeEach(func() {
		By("Initializing fake k8s client")
		scheme := runtime.NewScheme()
		Expect(kvmv1.AddToScheme(scheme)).To(Succeed())
		k8sclient = fake.NewClientBuilder().WithScheme(scheme).Build()
	})

	Describe("CheckHypervisor", func() {
		It("parses with correct config", func() {
			config, err := ucfgwrap.FromYAML([]byte("evicted: true"))
			Expect(err).To(Succeed())

			var base CheckHypervisor
			plugin, err := base.New(&config)

			Expect(err).To(Succeed())
			Expect(plugin).To(Equal(&CheckHypervisor{
				Fields: map[string]any{"Evicted": true},
			}))
		})

		It("fails parsing incorrect config", func() {
			_, err := ucfgwrap.FromYAML([]byte("invalid_yaml"))
			errMsg := "type 'string' is not supported on top level of config, only dictionary or list"
			Expect(err).To(MatchError(errMsg))

			config, err := ucfgwrap.FromYAML([]byte("value: test"))
			Expect(err).To(Succeed())

			var base CheckHypervisor
			_, err = base.New(&config)
			Expect(err).To(MatchError("field value not found in HypervisorStatus"))
		})

		It("fails without a matching hypervisor", func(ctx SpecContext) {
			checker := &CheckHypervisor{
				Fields: map[string]any{"Evicted": true},
			}
			result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("hypervisors.kvm.cloud.sap \"test-node\" not found"))
			Expect(result.Passed).To(BeFalse())
		})

		It("succeeds if the hypervisor matches the expected fields", func(ctx SpecContext) {
			By("Running the check with a matching hypervisor")
			hypervisor := &kvmv1.Hypervisor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Status:     kvmv1.HypervisorStatus{Evicted: true},
			}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			checker := &CheckHypervisor{
				Fields: map[string]any{"Evicted": true},
			}
			result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})

			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})

		It("fails if the field doesn't exist", func(ctx SpecContext) {
			By("Running the check with a non-existing field")
			hypervisor := &kvmv1.Hypervisor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			checker := &CheckHypervisor{
				Fields: map[string]any{"NonExistingField": true},
			}
			result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field NonExistingField not found in Hypervisor status"))
			Expect(result.Passed).To(BeFalse())
			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})

		It("doesn't panic if the type of f is not equal to the type of v", func(ctx SpecContext) {
			By("Running the check with a field type mismatch")
			hypervisor := &kvmv1.Hypervisor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			checker := &CheckHypervisor{
				Fields: map[string]any{"Instances": []string{"test-instance"}},
			}
			result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field Instances is not of type []string"))
			Expect(result.Passed).To(BeFalse())

			checker = &CheckHypervisor{
				Fields: map[string]any{"Evicted": 123},
			}
			result, err = checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field Evicted is not of type int"))
			Expect(result.Passed).To(BeFalse())

			checker = &CheckHypervisor{
				Fields: map[string]any{"NumInstances": []int{1, 2, 3}},
			}
			result, err = checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("unsupported field type []int for field NumInstances"))
			Expect(result.Passed).To(BeFalse())
			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})

		It("fails if the field value doesn't match", func(ctx SpecContext) {
			By("Running the check with a non-matching hypervisor")
			hypervisor := &kvmv1.Hypervisor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Status: kvmv1.HypervisorStatus{
					Evicted:      false,
					Aggregates:   []string{"agg1", "agg2"},
					NumInstances: 42,
				},
			}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			fields := map[string]any{
				"Evicted":      true,
				"Aggregates":   []string{"agg1", "agg2", "agg3"},
				"NumInstances": 10,
				"HypervisorID": "some-id",
			}
			for key, value := range fields {
				checker := &CheckHypervisor{
					Fields: map[string]any{key: value},
				}
				result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.Passed).To(BeFalse())
			}
			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})
	})

	Describe("HypervisorCondition", func() {
		It("parses with correct config", func() {
			config, err := ucfgwrap.FromYAML([]byte("type: Ready\nstatus: \"True\""))
			Expect(err).To(Succeed())

			var base HypervisorCondition
			plugin, err := base.New(&config)
			Expect(err).To(Succeed())
			Expect(plugin).To(Equal(&HypervisorCondition{
				Type:   "Ready",
				Status: "True",
			}))
		})

		It("fails parsing incorrect config", func() {
			_, err := ucfgwrap.FromYAML([]byte("invalid_yaml"))
			errMsg := "type 'string' is not supported on top level of config, only dictionary or list"
			Expect(err).To(MatchError(errMsg))

			config, err := ucfgwrap.FromYAML([]byte("testest: test"))
			Expect(err).To(Succeed())

			var base HypervisorCondition
			_, err = base.New(&config)
			Expect(err).To(MatchError("string value is not set accessing 'type'"))
		})

		It("fails without a matching hypervisor", func(ctx SpecContext) {
			checker := &HypervisorCondition{
				Type:   "Ready",
				Status: "True",
			}
			result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("hypervisors.kvm.cloud.sap \"test-node\" not found"))
			Expect(result.Passed).To(BeFalse())
		})

		It("succeeds if the hypervisor matches the expected fields", func(ctx SpecContext) {
			By("Running the check with a matching hypervisor")
			hypervisor := &kvmv1.Hypervisor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Status: kvmv1.HypervisorStatus{
					Conditions: []metav1.Condition{
						{Type: "Ready", Status: metav1.ConditionTrue},
					},
				},
			}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			checker := &HypervisorCondition{
				Type:   "Ready",
				Status: "True",
			}
			result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})

			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})

		It("not passes if the field doesn't exist", func(ctx SpecContext) {
			By("Running the check with a non-existing field")
			hypervisor := &kvmv1.Hypervisor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			checker := &HypervisorCondition{
				Type:   "NonExistingField",
				Status: "True",
			}
			result, err := checker.Check(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
			Expect(result.Info["reason"]).To(Equal("condition NonExistingField not present"))
			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})
	})

	Describe("AlterHypervisor", func() {
		It("fails parsing incorrect config", func() {
			_, err := ucfgwrap.FromYAML([]byte("invalid_yaml"))
			errMsg := "type 'string' is not supported on top level of config, only dictionary or list"
			Expect(err).To(MatchError(errMsg))

			config, err := ucfgwrap.FromYAML([]byte("value: test"))
			Expect(err).To(Succeed())

			var base AlterHypervisor
			_, err = base.New(&config)
			Expect(err).To(MatchError("field value not found in HypervisorSpec"))
		})

		It("has valid configuration", func() {
			config, err := ucfgwrap.FromYAML([]byte("maintenance: \"true\""))
			Expect(err).To(Succeed())

			var base AlterHypervisor
			plugin, err := base.New(&config)
			Expect(err).To(Succeed())
			Expect(plugin).To(Equal(&AlterHypervisor{
				Fields: &map[string]any{"Maintenance": "true"},
			}))
		})

		It("fails without a matching hypervisor", func(ctx SpecContext) {
			trigger := &AlterHypervisor{Fields: &map[string]any{"Maintenance": "true"}}
			err := trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("hypervisors.kvm.cloud.sap \"test-node\" not found"))
		})

		It("Fails when given an unsupported field type", func(ctx SpecContext) {
			By("Running the trigger with an unsupported field type")
			hypervisor := &kvmv1.Hypervisor{ObjectMeta: metav1.ObjectMeta{Name: "test-node"}}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			trigger := &AlterHypervisor{Fields: &map[string]any{"UnsupportedField": 3.14}}
			err := trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field UnsupportedField not found in Hypervisor spec"))

			trigger = &AlterHypervisor{Fields: &map[string]any{"InstallCertificate": "not-a-bool"}}
			err = trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field InstallCertificate is not of type string, expected bool"))

			trigger = &AlterHypervisor{Fields: &map[string]any{"Aggregates": "not-a-list"}}
			err = trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field Aggregates is not of type string, expected []string"))

			trigger = &AlterHypervisor{Fields: &map[string]any{"Maintenance": []string{"not", "a", "string"}}}
			err = trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field Maintenance is not of type []string, expected string"))

			trigger = &AlterHypervisor{Fields: &map[string]any{"OperatingSystemVersion": true}}
			err = trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("field OperatingSystemVersion is not of type bool, expected string"))

			trigger = &AlterHypervisor{Fields: &map[string]any{"SkipTests": 3.14}}
			err = trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(MatchError("unsupported field type float64 for field SkipTests"))

			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})

		It("alters the hypervisor fields as expected", func(ctx SpecContext) {
			By("Running the trigger to alter hypervisor fields")
			hypervisor := &kvmv1.Hypervisor{
				ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
				Spec:       kvmv1.HypervisorSpec{Maintenance: "false"},
			}
			Expect(k8sclient.Create(ctx, hypervisor)).To(Succeed())

			trigger := &AlterHypervisor{
				Fields: &map[string]any{
					"Maintenance":        "true",
					"InstallCertificate": true,
					"Aggregates":         []string{"agg1", "agg2"},
				},
			}
			err := trigger.Trigger(plugin.Parameters{Client: k8sclient, Ctx: ctx, Node: testNode})
			Expect(err).To(Succeed())

			updatedHypervisor := &kvmv1.Hypervisor{}
			Expect(k8sclient.Get(ctx, client.ObjectKey{Name: "test-node"}, updatedHypervisor)).To(Succeed())
			Expect(updatedHypervisor.Spec.Maintenance).To(Equal("true"))
			Expect(updatedHypervisor.Spec.Aggregates).To(Equal([]string{"agg1", "agg2"}))
			Expect(updatedHypervisor.Spec.InstallCertificate).To(BeTrue())
			Expect(k8sclient.Delete(ctx, hypervisor)).To(Succeed())
		})
	})

	Describe("mapToStructFields", func() {
		It("returns an error for unsupported field types", func() {
			_, err := mapToStructFields(
				map[string]any{"UnsupportedField": 3.14},
				reflect.TypeFor[kvmv1.HypervisorSpec](),
			)
			Expect(err).To(MatchError("field UnsupportedField not found in HypervisorSpec"))
		})

		It("returns an error for type mismatches", func() {
			_, err := mapToStructFields(
				map[string]any{"installCertificate": "not-a-bool"},
				reflect.TypeFor[kvmv1.HypervisorSpec](),
			)
			Expect(err).To(MatchError(
				"field installCertificate has incorrect type: expected bool, got string"),
			)
		})

		It("maps fields correctly", func() {
			fields, err := mapToStructFields(map[string]any{
				"maintenance":        "auto",
				"installCertificate": false,
				"aggregates":         []string{"agg1", "agg2"},
			}, reflect.TypeFor[kvmv1.HypervisorSpec]())
			Expect(err).To(Succeed())
			Expect(fields).To(Equal(map[string]any{
				"Maintenance":        "auto",
				"InstallCertificate": false,
				"Aggregates":         []string{"agg1", "agg2"},
			}))
		})
	})
})

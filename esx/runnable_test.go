// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package esx

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	vctypes "github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/constants"
)

const DefaultNamespace string = "default"

var _ = Describe("The ESX controller", func() {

	var firstNode *corev1.Node
	var secondNode *corev1.Node
	var thirdNode *corev1.Node
	var fourthNode *corev1.Node

	makeNode := func(ctx context.Context, name, esx string, schedulable, withPods bool) (*corev1.Node, error) {
		node := &corev1.Node{}
		node.Name = name
		node.Namespace = DefaultNamespace
		node.Spec.Unschedulable = !schedulable
		node.Labels = make(map[string]string)
		node.Labels[constants.HostLabelKey] = esx
		node.Labels[constants.FailureDomainLabelKey] = "eu-nl-2a"
		err := k8sClient.Create(ctx, node)
		if err != nil {
			return nil, err
		}

		if !withPods {
			return node, nil
		}

		pod := &corev1.Pod{}
		pod.Namespace = DefaultNamespace
		pod.Name = name + "-container"
		pod.Spec.NodeName = name
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		var gracePeriod int64
		pod.Spec.TerminationGracePeriodSeconds = &gracePeriod
		err = k8sClient.Create(ctx, pod)
		if err != nil {
			return nil, err
		}
		return node, nil
	}

	BeforeEach(func(ctx SpecContext) {
		var err error
		firstNode, err = makeNode(ctx, "firstvm", ESXName, true, true)
		Expect(err).To(Succeed())
		secondNode, err = makeNode(ctx, "secondvm", ESXName, true, true)
		Expect(err).To(Succeed())
		thirdNode, err = makeNode(ctx, "thirdvm", "DC0_H1", true, false)
		Expect(err).To(Succeed())
		fourthNode, err = makeNode(ctx, "fourthvm", "DC0_H1", false, false)
		Expect(err).To(Succeed())
	})

	AfterEach(func(ctx SpecContext) {
		err := k8sClient.Delete(ctx, firstNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(ctx, secondNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(ctx, thirdNode)
		Expect(err).To(Succeed())
		err = k8sClient.Delete(ctx, fourthNode)
		Expect(err).To(Succeed())

		var podList corev1.PodList
		err = k8sClient.List(ctx, &podList)
		Expect(err).To(Succeed())
		var gracePeriod int64
		for i := range podList.Items {
			err = k8sClient.Delete(ctx, &podList.Items[i],
				&client.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			Expect(err).To(Succeed())
		}

		vcClient, err := govmomi.NewClient(ctx, vcServer.URL, true)
		Expect(err).To(Succeed())
		// set host out of maintenance
		host := object.NewHostSystem(vcClient.Client, vctypes.ManagedObjectReference{
			Type:  "HostSystem",
			Value: esxRef,
		})
		task, err := host.ExitMaintenanceMode(ctx, 1000)
		Expect(err).To(Succeed())
		err = task.WaitEx(ctx)
		Expect(err).To(Succeed())
	})

	It("labels previously unlabeled nodes with maintenance state", func(ctx SpecContext) {
		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "firstvm"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(NoMaintenance)))
		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "secondvm"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(NoMaintenance)))
	})

	It("labels previously unlabeled nodes with esx version", func(ctx SpecContext) {
		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "firstvm"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.EsxVersionLabelKey]
			return val
		}).Should(Equal("8.0.2"))
		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "secondvm"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.EsxVersionLabelKey]
			return val
		}).Should(Equal("8.0.2"))
	})

	It("labels all nodes on a single EXS host in case of changes to the maintenance state", func(ctx SpecContext) {
		vcClient, err := govmomi.NewClient(ctx, vcServer.URL, true)
		Expect(err).To(Succeed())

		// set host in maintenance
		host := object.NewHostSystem(vcClient.Client, vctypes.ManagedObjectReference{
			Type:  "HostSystem",
			Value: esxRef,
		})
		task, err := host.EnterMaintenanceMode(ctx, 1000, false, &vctypes.HostMaintenanceSpec{})
		Expect(err).To(Succeed())
		err = task.WaitEx(ctx)
		Expect(err).To(Succeed())

		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "firstvm"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(InMaintenance)))
		Eventually(func(g Gomega) string {
			var node corev1.Node
			err := k8sClient.Get(ctx, client.ObjectKey{Name: "secondvm"}, &node)
			g.Expect(err).To(Succeed())

			val := node.Labels[constants.EsxMaintenanceLabelKey]
			return val
		}).Should(Equal(string(InMaintenance)))
	})

	fetchPowerState := func(ctx context.Context, vcClient *govmomi.Client) (vctypes.VirtualMachinePowerState, error) {
		mgr := view.NewManager(vcClient.Client)
		view, err := mgr.CreateContainerView(ctx,
			vcClient.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		if err != nil {
			return "", err
		}
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(ctx, []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Match{"name": "firstvm"})
		if err != nil {
			if err = view.Destroy(ctx); err != nil {
				return "", err
			}
			return "", nil
		}
		if err = view.Destroy(ctx); err != nil {
			return "", err
		}
		return vms[0].Summary.Runtime.PowerState, nil
	}

	It("shuts down nodes on an ESX host if it is in-maintenance and reboots are allowed", func(ctx SpecContext) {
		vcClient, err := govmomi.NewClient(ctx, vcServer.URL, true)
		Expect(err).To(Succeed())

		// set host in maintenance
		host := object.NewHostSystem(vcClient.Client, vctypes.ManagedObjectReference{
			Type:  "HostSystem",
			Value: esxRef,
		})
		task, err := host.EnterMaintenanceMode(ctx, 1000, false, &vctypes.HostMaintenanceSpec{})
		Expect(err).To(Succeed())
		err = task.WaitEx(ctx)
		Expect(err).To(Succeed())

		allowMaintenance := func(node *corev1.Node) error {
			cloned := node.DeepCopy()
			node.Labels[constants.EsxRebootOkLabelKey] = constants.TrueStr
			return k8sClient.Patch(ctx, node, client.MergeFrom(cloned))
		}
		Expect(allowMaintenance(firstNode)).To(Succeed())
		Expect(allowMaintenance(secondNode)).To(Succeed())

		Eventually(func(g Gomega) map[string]string {
			node := &corev1.Node{}
			err = k8sClient.Get(ctx, types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			g.Expect(err).To(Succeed())
			return node.Annotations
		}).Should(HaveKey(constants.EsxRebootInitiatedAnnotationKey))
		Eventually(func(g Gomega) bool {
			node := &corev1.Node{}
			err = k8sClient.Get(ctx, types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			g.Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}).Should(BeTrue())
		Eventually(func(g Gomega) []corev1.Pod {
			var podList corev1.PodList
			err = k8sClient.List(ctx, &podList)
			g.Expect(err).To(Succeed())
			return podList.Items
		}, 10*time.Second).Should(BeEmpty())
		Eventually(func(g Gomega) bool {
			powerState, err := fetchPowerState(ctx, vcClient)
			g.Expect(err).To(Succeed())
			return powerState == vctypes.VirtualMachinePowerStatePoweredOff
		}).Should(BeTrue())

		// ensure VM's on different host are not affected
		mgr := view.NewManager(vcClient.Client)
		Expect(err).To(Succeed())
		view, err := mgr.CreateContainerView(ctx,
			vcClient.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		Expect(err).To(Succeed())
		defer func() {
			Expect(view.Destroy(ctx)).To(Succeed())
		}()
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(ctx, []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Match{"name": "thirdvm"})
		Expect(err).To(Succeed())
		result := vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn
		Expect(result).To(BeTrue())
		err = view.RetrieveWithFilter(ctx, []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Match{"name": "fourthvm"})
		Expect(err).To(Succeed())
		result = vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn
		Expect(result).To(BeTrue())
	})

	It("starts nodes on an ESX host if it is out of maintenance and the controller initiated the shutdown", func(ctx SpecContext) {
		vcClient, err := govmomi.NewClient(ctx, vcServer.URL, true)
		Expect(err).To(Succeed())

		markInitiated := func(node *corev1.Node) error {
			cloned := node.DeepCopy()
			node.Spec.Unschedulable = true
			node.Annotations = map[string]string{constants.EsxRebootInitiatedAnnotationKey: constants.TrueStr}
			return k8sClient.Patch(ctx, node, client.MergeFrom(cloned))
		}
		Expect(markInitiated(firstNode)).To(Succeed())
		Expect(markInitiated(secondNode)).To(Succeed())

		Eventually(func(g Gomega) bool {
			node := &corev1.Node{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			g.Expect(err).To(Succeed())
			return node.Spec.Unschedulable
		}, 10*time.Second).Should(BeFalse())
		Eventually(func(g Gomega) map[string]string {
			node := &corev1.Node{}
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: DefaultNamespace, Name: "firstvm"}, node)
			g.Expect(err).To(Succeed())
			return node.Annotations
		}).ShouldNot(HaveKey(constants.EsxRebootInitiatedAnnotationKey))
		Eventually(func(g Gomega) bool {
			powerState, err := fetchPowerState(ctx, vcClient)
			g.Expect(err).To(Succeed())
			return powerState == vctypes.VirtualMachinePowerStatePoweredOn
		}).Should(BeTrue())
	})

})

var _ = Describe("nodeGracePeriod", func() {

	It("returns 0 if node is in alarm maintenance", func() {
		node := &corev1.Node{}
		node.Labels = map[string]string{constants.EsxMaintenanceLabelKey: string(AlarmMaintenance)}
		gracePeriod := nodeGracePeriod(node)
		Expect(gracePeriod).To(HaveValue(BeEquivalentTo(0)))
	})

	It("returns nil if node is in normal maintenance", func() {
		node := &corev1.Node{}
		node.Labels = map[string]string{constants.EsxMaintenanceLabelKey: string(InMaintenance)}
		gracePeriod := nodeGracePeriod(node)
		Expect(gracePeriod).To(BeNil())
	})

	It("returns nil if node maintenance is unknown", func() {
		node := &corev1.Node{}
		gracePeriod := nodeGracePeriod(node)
		Expect(gracePeriod).To(BeNil())
	})

})

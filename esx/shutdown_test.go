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

package esx

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	vctypes "github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/common"
	"github.com/sapcc/maintenance-controller/constants"
)

var _ = Describe("ShouldShutdown", func() {

	makeEsx := func(m1 Maintenance, v1 string, m2 Maintenance, v2 string) Host { //nolint:unparam
		nodes := make([]corev1.Node, 2)
		nodes[0].Labels = make(map[string]string)
		nodes[0].Labels[constants.EsxMaintenanceLabelKey] = string(m1)
		nodes[0].Labels[constants.EsxRebootOkLabelKey] = v1
		nodes[1].Labels = make(map[string]string)
		nodes[1].Labels[constants.EsxMaintenanceLabelKey] = string(m2)
		nodes[1].Labels[constants.EsxRebootOkLabelKey] = v2
		return Host{
			Nodes: nodes,
		}
	}

	It("should pass if all nodes require ESX maintenance and are allowed to reboot", func() {
		esx := makeEsx(InMaintenance, constants.TrueStr, InMaintenance, constants.TrueStr)
		result := ShouldShutdown(&esx)
		Expect(result).To(BeTrue())
	})

	It("should pass if all nodes have an ESX alarm and are allowed to reboot", func() {
		esx := makeEsx(AlarmMaintenance, constants.TrueStr, AlarmMaintenance, constants.TrueStr)
		result := ShouldShutdown(&esx)
		Expect(result).To(BeTrue())
	})

	It("should not pass if at least one node does not require maintenance", func() {
		esx := makeEsx(InMaintenance, constants.TrueStr, NoMaintenance, constants.TrueStr)
		result := ShouldShutdown(&esx)
		Expect(result).To(BeFalse())
	})

	It("should not pass if at least one node does not have approval", func() {
		esx := makeEsx(InMaintenance, constants.TrueStr, InMaintenance, "thisisfine")
		result := ShouldShutdown(&esx)
		Expect(result).To(BeFalse())
	})
})

var _ = Describe("GetPodsForDeletion", func() {

	makePod := func(ctx context.Context, podName, nodeName string, custom ...func(*corev1.Pod)) error {
		var pod corev1.Pod
		pod.Namespace = "default"
		pod.Name = podName
		pod.Spec.NodeName = nodeName
		pod.Spec.Containers = []corev1.Container{
			{
				Name:  "nginx",
				Image: "nginx",
			},
		}
		for _, cust := range custom {
			cust(&pod)
		}
		return k8sClient.Create(ctx, &pod)
	}

	AfterEach(func(ctx context.Context) {
		var podList corev1.PodList
		Expect(k8sClient.List(ctx, &podList)).To(Succeed())
		var gracePeriod int64
		for i := range podList.Items {
			Expect(k8sClient.Delete(ctx, &podList.Items[i],
				&client.DeleteOptions{GracePeriodSeconds: &gracePeriod})).To(Succeed())
		}
		err := common.WaitForPodDeletions(ctx, k8sClient, podList.Items,
			common.WaitParameters{
				Period:  1 * time.Second,
				Timeout: 4 * time.Second,
			})
		Expect(err).To(Succeed())
	})

	It("filters for the correct node", func(ctx SpecContext) {
		Expect(makePod(ctx, "firstpod", "firstnode")).To(Succeed())
		Expect(makePod(ctx, "secondpod", "secondnode")).To(Succeed())
		deletable, err := common.GetPodsForDrain(ctx, k8sClient, "firstnode")
		Expect(err).To(Succeed())
		Expect(deletable).To(HaveLen(1))
	})

	It("filters DaemonSets", func(ctx SpecContext) {
		Expect(makePod(ctx, "firstpod", "node")).To(Succeed())
		Expect(makePod(ctx, "secondpod", "node")).To(Succeed())
		Expect(makePod(ctx, "ds", "node", func(p *corev1.Pod) {
			p.OwnerReferences = []v1.OwnerReference{{Kind: "DaemonSet", APIVersion: "apps/v1", Name: "ds", UID: types.UID("ds")}}
		})).To(Succeed())
		deletable, err := common.GetPodsForDrain(ctx, k8sClient, "node")
		Expect(err).To(Succeed())
		Expect(deletable).To(HaveLen(2))
	})

	It("filters MirrorPods", func(ctx SpecContext) {
		Expect(makePod(ctx, "firstpod", "node")).To(Succeed())
		Expect(makePod(ctx, "secondpod", "node")).To(Succeed())
		Expect(makePod(ctx, "mirror", "node", func(p *corev1.Pod) {
			p.Annotations = map[string]string{corev1.MirrorPodAnnotationKey: constants.TrueStr}
		})).To(Succeed())
		deletable, err := common.GetPodsForDrain(ctx, k8sClient, "node")
		Expect(err).To(Succeed())
		Expect(deletable).To(HaveLen(2))
	})
})

var _ = Describe("ensureVmOff", func() {
	var vCenters *VCenters

	BeforeEach(func() {
		vCenters = &VCenters{
			Template: TemplateURL,
			Credentials: map[string]Credential{
				vcServer.URL.Host: {
					Username: "user",
					Password: "pass",
				},
			},
		}
	})

	AfterEach(func(ctx SpecContext) {
		// power on VM
		client, err := vCenters.Client(ctx, vcServer.URL.Host)
		Expect(err).To(Succeed())
		mgr := view.NewManager(client.Client)
		Expect(err).To(Succeed())
		view, err := mgr.CreateContainerView(ctx,
			client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		Expect(err).To(Succeed())
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(ctx, []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Match{"name": "firstvm"})
		Expect(err).To(Succeed())
		vm := object.NewVirtualMachine(client.Client, vms[0].Self)
		task, err := vm.PowerOn(ctx)
		Expect(err).To(Succeed())
		err = task.WaitEx(ctx)
		Expect(err).To(Succeed())
	})

	It("should shutdown a VM", func(ctx SpecContext) {
		err := EnsureVMOff(ctx, ShutdownParams{
			VCenters: vCenters,
			Info: HostInfo{
				AvailabilityZone: vcServer.URL.Host,
				Name:             HostSystemName,
			},
			NodeName: "firstvm",
			Period:   1 * time.Second,
			Timeout:  1 * time.Minute,
			Log:      GinkgoLogr,
		})
		Expect(err).To(Succeed())

		client, err := vCenters.Client(ctx, vcServer.URL.Host)
		Expect(err).To(Succeed())
		mgr := view.NewManager(client.Client)
		Expect(err).To(Succeed())
		view, err := mgr.CreateContainerView(ctx,
			client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		Expect(err).To(Succeed())
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(ctx, []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Match{"name": "firstvm"})
		Expect(err).To(Succeed())
		result := vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOff
		Expect(result).To(BeTrue())
	})
})

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
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	vctypes "github.com/vmware/govmomi/vim25/types"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("ShouldStart", func() {

	makeNode := func(initiated string, maintenance Maintenance) *v1.Node {
		node := &v1.Node{}
		node.Name = "somenode"
		node.Labels = map[string]string{constants.EsxMaintenanceLabelKey: string(maintenance)}
		node.Annotations = map[string]string{constants.EsxRebootInitiatedAnnotationKey: initiated}
		return node
	}

	It("passes if the controller initiated the maintenance and ESX is not in maintenance", func() {
		Expect(ShouldStart(makeNode(constants.TrueStr, NoMaintenance))).To(BeTrue())
	})

	It("does not pass if the controller did not initiate the maintenance", func() {
		Expect(ShouldStart(makeNode("garbage", NoMaintenance))).To(BeFalse())
	})

	It("does not pass if the ESX is not out of maintenance", func() {
		Expect(ShouldStart(makeNode(constants.TrueStr, InMaintenance))).To(BeFalse())
	})

})

var _ = Describe("ensureVmOn", func() {

	It("starts a VM", func() {
		vCenters := &VCenters{
			Template: "http://" + AvailabilityZoneReplacer,
			Credentials: map[string]Credential{
				vcServer.URL.Host: {
					Username: "user",
					Password: "pass",
				},
			},
		}
		hostInfo := HostInfo{
			AvailabilityZone: vcServer.URL.Host,
			Name:             HostSystemName,
		}
		err := EnsureVMOff(context.Background(), ShutdownParams{
			VCenters: vCenters,
			Info:     hostInfo,
			NodeName: "firstvm",
			Period:   1 * time.Second,
			Timeout:  1 * time.Minute,
			Log:      GinkgoLogr,
		})
		Expect(err).To(Succeed())
		err = ensureVMOn(context.Background(), vCenters, hostInfo, "firstvm")
		Expect(err).To(Succeed())

		client, err := vCenters.Client(context.Background(), vcServer.URL.Host)
		Expect(err).To(Succeed())
		mgr := view.NewManager(client.Client)
		Expect(err).To(Succeed())
		view, err := mgr.CreateContainerView(context.Background(),
			client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
		Expect(err).To(Succeed())
		var vms []mo.VirtualMachine
		err = view.RetrieveWithFilter(context.Background(), []string{"VirtualMachine"},
			[]string{"summary.runtime"}, &vms, property.Filter{"name": "firstvm"})
		Expect(err).To(Succeed())
		result := vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOn
		Expect(result).To(BeTrue())
	})

})

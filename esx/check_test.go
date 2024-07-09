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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

const HostSystemName string = "DC0_H0"

var _ = Describe("CheckForMaintenance", func() {
	var vCenters *VCenters

	BeforeEach(func(ctx SpecContext) {
		vCenters = &VCenters{
			Template: TemplateURL,
			Credentials: map[string]Credential{
				vcServer.URL.Host: {
					Username: "user",
					Password: "pass",
				},
			},
		}

		// set host out of maintenance
		client, err := vCenters.Client(ctx, vcServer.URL.Host)
		Expect(err).To(Succeed())
		host := object.NewHostSystem(client.Client, types.ManagedObjectReference{
			Type:  "HostSystem",
			Value: esxRef,
		})
		task, err := host.ExitMaintenanceMode(ctx, 1000)
		Expect(err).To(Succeed())
		err = task.WaitEx(ctx)
		Expect(err).To(Succeed())
	})

	AfterEach(func(ctx SpecContext) {
		// set host out of maintenance
		client, err := vCenters.Client(ctx, vcServer.URL.Host)
		Expect(err).To(Succeed())
		host := object.NewHostSystem(client.Client, types.ManagedObjectReference{
			Type:  "HostSystem",
			Value: esxRef,
		})
		task, err := host.ExitMaintenanceMode(ctx, 1000)
		Expect(err).To(Succeed())
		err = task.WaitEx(ctx)
		Expect(err).To(Succeed())
	})

	It("should return NoMaintenance if the host is not in maintenance", func(ctx SpecContext) {
		result, err := CheckForMaintenance(ctx, CheckParameters{vCenters, HostInfo{
			AvailabilityZone: vcServer.URL.Host,
			Name:             HostSystemName,
		}, GinkgoLogr})
		Expect(err).To(Succeed())
		Expect(result).To(Equal(NoMaintenance))
	})

	It("should return InMaintenance if the host is in maintenance", func(ctx SpecContext) {
		client, err := vCenters.Client(ctx, vcServer.URL.Host)
		Expect(err).To(Succeed())

		// set host in maintenance
		host := object.NewHostSystem(client.Client, types.ManagedObjectReference{
			Type:  "HostSystem",
			Value: esxRef,
		})
		task, err := host.EnterMaintenanceMode(ctx, 1000, false, &types.HostMaintenanceSpec{})
		Expect(err).To(Succeed())
		err = task.WaitEx(ctx)
		Expect(err).To(Succeed())

		result, err := CheckForMaintenance(ctx, CheckParameters{vCenters, HostInfo{
			AvailabilityZone: vcServer.URL.Host,
			Name:             HostSystemName,
		}, GinkgoLogr})
		Expect(err).To(Succeed())
		Expect(result).To(Equal(InMaintenance))
	})
})

var _ = Describe("FetchVersion", func() {

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

	It("should return the version", func(ctx SpecContext) {
		version, err := FetchVersion(ctx, CheckParameters{vCenters, HostInfo{
			AvailabilityZone: vcServer.URL.Host,
			Name:             HostSystemName,
		}, GinkgoLogr})
		Expect(err).To(Succeed())
		Expect(version).To(Equal("8.0.2"))
	})

})

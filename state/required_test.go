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

package state

import (
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("MaintenanceRequired State", func() {

	It("should have Required Label", func() {
		mr := newMaintenanceRequired(PluginChains{}, time.Hour)
		Expect(mr.Label()).To(Equal(Required))
	})

	Context("with empty CheckChain", func() {

		It("transitions to in maintenance", func() {
			mr := newMaintenanceRequired(PluginChains{}, time.Hour)
			next, err := mr.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(InMaintenance))
		})

	})

	Context("with initalized chains", func() {

		var chains PluginChains
		var trigger *successfulTrigger
		var notification *successfulNotification
		var check *mockCheck

		BeforeEach(func() {
			var checkChain plugin.CheckChain
			checkChain, check = mockCheckChain()
			var notificationChain plugin.NotificationChain
			notificationChain, notification = mockNotificationChain()
			var triggerChain plugin.TriggerChain
			triggerChain, trigger = mockTriggerChain()
			chains = PluginChains{
				Check:        checkChain,
				Notification: notificationChain,
				Trigger:      triggerChain,
			}
		})

		It("executes the triggers", func() {
			mr := newMaintenanceRequired(chains, time.Hour)
			err := mr.Trigger(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("executes the notifications", func() {
			mr := newMaintenanceRequired(chains, time.Hour)
			err := mr.Notify(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to in maintenance if checks pass", func() {
			check.Result = true
			mr := newMaintenanceRequired(chains, time.Hour)
			next, err := mr.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(InMaintenance))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to required if checks do not pass", func() {
			check.Result = false
			mr := newMaintenanceRequired(chains, time.Hour)
			next, err := mr.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(Required))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to required if checks fail", func() {
			check.Fail = true
			mr := newMaintenanceRequired(chains, time.Hour)
			next, err := mr.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(HaveOccurred())
			Expect(next).To(Equal(Required))
			Expect(check.Invoked).To(Equal(1))
		})

	})

})

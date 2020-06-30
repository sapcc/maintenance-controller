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

var _ = Describe("Operational State", func() {

	It("should have Operational Label", func() {
		op := newOperational(PluginChains{}, time.Hour)
		Expect(op.Label()).To(Equal(Operational))
	})

	Context("with empty CheckChain", func() {

		It("transitions to Operational", func() {
			op := newOperational(PluginChains{}, time.Hour)
			next, err := op.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(Operational))
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
			op := newOperational(chains, time.Hour)
			err := op.Trigger(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("executes the notifications", func() {
			op := newOperational(chains, time.Hour)
			err := op.Notify(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to required if checks pass", func() {
			check.Result = true
			op := newOperational(chains, time.Hour)
			next, err := op.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(Required))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to operational if checks do not pass", func() {
			check.Result = false
			op := newOperational(chains, time.Hour)
			next, err := op.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(Operational))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to operational if checks fail", func() {
			check.Fail = true
			op := newOperational(chains, time.Hour)
			next, err := op.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(HaveOccurred())
			Expect(next).To(Equal(Operational))
			Expect(check.Invoked).To(Equal(1))
		})

	})

})

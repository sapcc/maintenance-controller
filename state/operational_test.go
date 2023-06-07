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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("Operational State", func() {

	It("should have Operational Label", func() {
		op := newOperational(PluginChains{})
		Expect(op.Label()).To(Equal(Operational))
	})

	Context("with empty CheckChain", func() {

		It("transitions to Operational", func() {
			op := newOperational(PluginChains{})
			result, err := op.Transition(plugin.Parameters{Log: GinkgoLogr}, &DataV2{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Operational))
		})

	})

	Context("with initialized chains", func() {

		var chains PluginChains
		var trigger *mockTrigger
		var notification *mockNotificaiton
		var check *mockCheck

		BeforeEach(func() {
			var checkChain plugin.CheckChain
			checkChain, check = mockCheckChain()
			var notificationChain plugin.NotificationChain
			notificationChain, notification = mockNotificationChain(1)
			var triggerChain plugin.TriggerChain
			triggerChain, trigger = mockTriggerChain()
			chains = PluginChains{
				Transitions: []Transition{
					{
						Check:   checkChain,
						Trigger: triggerChain,
						Next:    Required,
					},
				},
				Notification: notificationChain,
			}
		})

		It("executes the triggers", func() {
			op := newOperational(chains)
			err := op.Trigger(plugin.Parameters{Log: GinkgoLogr}, Required, &DataV2{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("fails to transition if target state is not defined", func() {
			op := newOperational(chains)
			err := op.Trigger(plugin.Parameters{Log: GinkgoLogr}, InMaintenance, &DataV2{})
			Expect(err).ToNot(Succeed())
			Expect(trigger.Invoked).To(Equal(0))
		})

		It("executes the notifications", func() {
			op := newOperational(chains)
			// we set in-maintenance as previous state below, so the special case in NotifyPeriodic does not apply
			err := op.Notify(
				plugin.Parameters{Log: GinkgoLogr, Profile: "p"},
				&DataV2{
					Profiles:      map[string]*ProfileData{"p": {Current: Operational, Previous: InMaintenance}},
					Notifications: make(map[string]time.Time),
				},
			)
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to required if checks pass", func() {
			check.Result = true
			op := newOperational(chains)
			result, err := op.Transition(plugin.Parameters{Log: GinkgoLogr}, &DataV2{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Required))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to operational if checks do not pass", func() {
			check.Result = false
			op := newOperational(chains)
			result, err := op.Transition(plugin.Parameters{Log: GinkgoLogr}, &DataV2{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Operational))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to operational if checks fail", func() {
			check.Fail = true
			op := newOperational(chains)
			result, err := op.Transition(plugin.Parameters{Log: GinkgoLogr}, &DataV2{})
			Expect(err).To(HaveOccurred())
			Expect(result.Next).To(Equal(Operational))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).ToNot(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

	})

	It("should execute the notification chain if the state has changed", func() {
		chain, notification := mockNotificationChain(1)
		data := DataV2{
			Notifications: map[string]time.Time{"mock": time.Now().UTC()},
			Profiles: map[string]*ProfileData{
				"mock": {
					Transition: time.Now().UTC(),
					Previous:   InMaintenance,
					Current:    Operational,
				},
			},
		}
		oper := operational{
			chains: PluginChains{Notification: chain},
		}
		err := oper.Notify(plugin.Parameters{Log: GinkgoLogr, Profile: "mock"}, &data)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(1))
	})

	It("should not execute the notification chain if the state has not changed", func() {
		chain, notification := mockNotificationChain(1)
		data := DataV2{
			Notifications: map[string]time.Time{"mock": time.Now().UTC()},
			Profiles: map[string]*ProfileData{
				"mock": {
					Transition: time.Now().UTC(),
					Previous:   Operational,
					Current:    Operational,
				},
			},
		}
		oper := newOperational(PluginChains{Notification: chain})
		err := oper.Notify(plugin.Parameters{Log: GinkgoLogr, Profile: "mock"}, &data)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

})

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

	"github.com/go-logr/logr"
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
			result, err := op.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
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
			err := op.Trigger(plugin.Parameters{Log: logr.Discard()}, Required, &Data{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("fails to transition if target state is not defined", func() {
			op := newOperational(chains)
			err := op.Trigger(plugin.Parameters{Log: logr.Discard()}, InMaintenance, &Data{})
			Expect(err).ToNot(Succeed())
			Expect(trigger.Invoked).To(Equal(0))
		})

		It("executes the notifications", func() {
			op := newOperational(chains)
			err := op.Notify(plugin.Parameters{Log: logr.Discard()}, &Data{LastNotificationTimes: make(map[string]time.Time)})
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to required if checks pass", func() {
			check.Result = true
			op := newOperational(chains)
			result, err := op.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Required))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to operational if checks do not pass", func() {
			check.Result = false
			op := newOperational(chains)
			result, err := op.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Operational))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to operational if checks fail", func() {
			check.Fail = true
			op := newOperational(chains)
			result, err := op.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(HaveOccurred())
			Expect(result.Next).To(Equal(Operational))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).ToNot(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

	})

	It("should execute the notification chain if the state has changed", func() {
		chain, notification := mockNotificationChain(1)
		data := Data{
			LastTransition:        time.Now().UTC(),
			LastNotificationTimes: map[string]time.Time{"mock": time.Now().UTC()},
			PreviousStates:        map[string]NodeStateLabel{"mock": InMaintenance},
		}
		oper := operational{
			chains: PluginChains{Notification: chain},
		}
		err := oper.Notify(plugin.Parameters{Log: logr.Discard(), Profile: "mock"}, &data)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(1))
	})

	It("should not execute the notification chain if the state has not changed", func() {
		chain, notification := mockNotificationChain(1)
		data := Data{
			LastTransition:        time.Now().UTC(),
			LastNotificationTimes: map[string]time.Time{"mock": time.Now().UTC()},
			PreviousStates:        map[string]NodeStateLabel{"mock": Operational},
		}
		oper := newOperational(PluginChains{Notification: chain})
		err := oper.Notify(plugin.Parameters{Log: logr.Discard(), Profile: "mock"}, &data)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

})

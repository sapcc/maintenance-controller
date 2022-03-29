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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
)

var _ = Describe("InMaintenance State", func() {

	It("should have InMaintenance Label", func() {
		im := newInMaintenance(PluginChains{})
		Expect(im.Label()).To(Equal(InMaintenance))
	})

	Context("with empty CheckChain", func() {

		It("transitions to in-maintenance", func() {
			im := newInMaintenance(PluginChains{})
			next, err := im.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(InMaintenance))
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
						Next:    Operational,
					},
				},
				Notification: notificationChain,
			}
		})

		It("executes the triggers", func() {
			im := newInMaintenance(chains)
			err := im.Trigger(plugin.Parameters{Log: logr.Discard()}, Operational, &Data{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("fails to transition if target state is not defined", func() {
			im := newInMaintenance(chains)
			err := im.Trigger(plugin.Parameters{Log: logr.Discard()}, Required, &Data{})
			Expect(err).ToNot(Succeed())
			Expect(trigger.Invoked).To(Equal(0))
		})

		It("executes the notifications", func() {
			im := newInMaintenance(chains)
			err := im.Notify(plugin.Parameters{Log: logr.Discard()}, &Data{LastNotificationTimes: make(map[string]time.Time)})
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to in operational if checks pass", func() {
			check.Result = true
			im := newInMaintenance(chains)
			next, err := im.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(Operational))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to inMaintenance if checks do not pass", func() {
			check.Result = false
			im := newInMaintenance(chains)
			next, err := im.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(Succeed())
			Expect(next).To(Equal(InMaintenance))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to inMaintenance if checks fail", func() {
			check.Fail = true
			im := newInMaintenance(chains)
			next, err := im.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(HaveOccurred())
			Expect(next).To(Equal(InMaintenance))
			Expect(check.Invoked).To(Equal(1))
		})

	})

})

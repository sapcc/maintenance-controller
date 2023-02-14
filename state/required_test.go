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
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("MaintenanceRequired State", func() {

	It("should have Required Label", func() {
		mr := newMaintenanceRequired(PluginChains{})
		Expect(mr.Label()).To(Equal(Required))
	})

	Context("with empty CheckChain", func() {

		It("transitions to maintenance-required", func() {
			mr := newMaintenanceRequired(PluginChains{})
			result, err := mr.Transition(plugin.Parameters{}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Required))
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
						Next:    InMaintenance,
					},
				},
				Notification: notificationChain,
			}
		})

		It("executes the triggers", func() {
			mr := newMaintenanceRequired(chains)
			err := mr.Trigger(plugin.Parameters{Log: logr.Discard()}, InMaintenance, &Data{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("fails to transition if target state is not defined", func() {
			mr := newMaintenanceRequired(chains)
			err := mr.Trigger(plugin.Parameters{Log: logr.Discard()}, Operational, &Data{})
			Expect(err).ToNot(Succeed())
			Expect(trigger.Invoked).To(Equal(0))
		})

		It("executes the notifications", func() {
			mr := newMaintenanceRequired(chains)
			err := mr.Notify(plugin.Parameters{Log: logr.Discard()}, &Data{LastNotificationTimes: make(map[string]time.Time)})
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to in maintenance if checks pass", func() {
			check.Result = true
			mr := newMaintenanceRequired(chains)
			result, err := mr.Transition(plugin.Parameters{
				Log:    logr.Discard(),
				Node:   &v1.Node{},
				Client: fake.NewClientBuilder().Build(),
			}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(InMaintenance))
			Expect(result.Infos).To(HaveLen(1))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to required if checks do not pass", func() {
			check.Result = false
			mr := newMaintenanceRequired(chains)
			result, err := mr.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Required))
			Expect(result.Infos).To(HaveLen(1))
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to required if checks fail", func() {
			check.Fail = true
			mr := newMaintenanceRequired(chains)
			result, err := mr.Transition(plugin.Parameters{Log: logr.Discard()}, &Data{})
			Expect(err).To(HaveOccurred())
			Expect(result.Next).To(Equal(Required))
			Expect(result.Infos).To(HaveLen(1))
			Expect(check.Invoked).To(Equal(1))
		})

	})

})

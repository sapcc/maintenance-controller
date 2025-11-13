// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
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
			result, err := im.Transition(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(InMaintenance))
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
			err := im.Trigger(plugin.Parameters{Log: GinkgoLogr}, Operational, &Data{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("fails to transition if target state is not defined", func() {
			im := newInMaintenance(chains)
			err := im.Trigger(plugin.Parameters{Log: GinkgoLogr}, Required, &Data{})
			Expect(err).ToNot(Succeed())
			Expect(trigger.Invoked).To(Equal(0))
		})

		It("executes the notifications", func() {
			im := newInMaintenance(chains)
			err := im.Notify(
				plugin.Parameters{Log: GinkgoLogr, Profile: "p"},
				&Data{
					Profiles:      map[string]*ProfileData{"p": {Current: InMaintenance, Previous: InMaintenance}},
					Notifications: make(map[string]time.Time),
				},
			)
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to in operational if checks pass", func() {
			check.Result = true
			im := newInMaintenance(chains)
			result, err := im.Transition(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Operational))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to inMaintenance if checks do not pass", func() {
			check.Result = false
			im := newInMaintenance(chains)
			result, err := im.Transition(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(InMaintenance))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to inMaintenance if checks fail", func() {
			check.Fail = true
			im := newInMaintenance(chains)
			result, err := im.Transition(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(HaveOccurred())
			Expect(result.Next).To(Equal(InMaintenance))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).ToNot(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("executes the enter chain", func() {
			chain, enter := mockTriggerChain()
			chains.Enter = chain
			im := newInMaintenance(chains)
			err := im.Enter(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(Succeed())
			Expect(enter.Invoked).To(Equal(1))
		})

	})
})

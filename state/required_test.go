// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/sapcc/maintenance-controller/plugin"
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
			err := mr.Trigger(plugin.Parameters{Log: GinkgoLogr}, InMaintenance, &Data{})
			Expect(err).To(Succeed())
			Expect(trigger.Invoked).To(Equal(1))
		})

		It("fails to transition if target state is not defined", func() {
			mr := newMaintenanceRequired(chains)
			err := mr.Trigger(plugin.Parameters{Log: GinkgoLogr}, Operational, &Data{})
			Expect(err).ToNot(Succeed())
			Expect(trigger.Invoked).To(Equal(0))
		})

		It("executes the notifications", func() {
			mr := newMaintenanceRequired(chains)
			err := mr.Notify(
				plugin.Parameters{Log: GinkgoLogr, Profile: "p"},
				&Data{
					Profiles:      map[string]*ProfileData{"p": {Current: Required, Previous: Required}},
					Notifications: make(map[string]time.Time),
				},
			)
			Expect(err).To(Succeed())
			Expect(notification.Invoked).To(Equal(1))
		})

		It("transitions to in maintenance if checks pass", func() {
			check.Result = true
			mr := newMaintenanceRequired(chains)
			result, err := mr.Transition(plugin.Parameters{
				Log:    GinkgoLogr,
				Node:   &v1.Node{},
				Client: fake.NewClientBuilder().Build(),
			}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(InMaintenance))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to required if checks do not pass", func() {
			check.Result = false
			mr := newMaintenanceRequired(chains)
			result, err := mr.Transition(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(Succeed())
			Expect(result.Next).To(Equal(Required))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).To(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("transitions to required if checks fail", func() {
			check.Fail = true
			mr := newMaintenanceRequired(chains)
			result, err := mr.Transition(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(HaveOccurred())
			Expect(result.Next).To(Equal(Required))
			Expect(result.Infos).To(HaveLen(1))
			Expect(result.Infos[0].Error).ToNot(BeEmpty())
			Expect(check.Invoked).To(Equal(1))
		})

		It("executes the enter chain", func() {
			chain, enter := mockTriggerChain()
			chains.Enter = chain
			mr := newOperational(chains)
			err := mr.Enter(plugin.Parameters{Log: GinkgoLogr}, &Data{})
			Expect(err).To(Succeed())
			Expect(enter.Invoked).To(Equal(1))
		})

	})
})

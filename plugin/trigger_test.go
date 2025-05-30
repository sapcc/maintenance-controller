// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
)

type successfulTrigger struct {
	Invoked int
}

func (n *successfulTrigger) Trigger(params Parameters) error {
	n.Invoked++
	return nil
}

func (n *successfulTrigger) New(config *ucfgwrap.Config) (Trigger, error) {
	return &successfulTrigger{}, nil
}

func (n *successfulTrigger) ID() string {
	return "success"
}

type failingTrigger struct {
	Invoked int
}

func (n *failingTrigger) Trigger(params Parameters) error {
	n.Invoked++
	return errors.New("this notification is expected to fail")
}

func (n *failingTrigger) New(config *ucfgwrap.Config) (Trigger, error) {
	return &failingTrigger{}, nil
}

func (n *failingTrigger) ID() string {
	return "fail"
}

var _ = Describe("TriggerChain", func() {
	emptyParams := Parameters{Log: GinkgoLogr}

	Context("is empty", func() {

		var chain TriggerChain
		It("should not error", func() {
			err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
		})

	})

	Context("contains plugins", func() {
		var (
			success TriggerInstance
			failing TriggerInstance
		)

		BeforeEach(func() {
			success = TriggerInstance{
				Plugin: &successfulTrigger{},
				Name:   "success",
			}
			failing = TriggerInstance{
				Plugin: &failingTrigger{},
				Name:   "failing",
			}
		})

		It("should run all plugins", func() {
			chain := TriggerChain{
				Plugins: []TriggerInstance{success, success},
			}
			err := chain.Execute(emptyParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(success.Plugin).To(Equal(&successfulTrigger{Invoked: 2}))
		})

		It("should propagate errors", func() {
			chain := TriggerChain{
				Plugins: []TriggerInstance{success, failing, success},
			}
			err := chain.Execute(emptyParams)
			Expect(err).To(HaveOccurred())
			Expect(success.Plugin).To(Equal(&successfulTrigger{Invoked: 1}))
			Expect(failing.Plugin).To(Equal(&failingTrigger{Invoked: 1}))
		})
	})
})

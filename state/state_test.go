// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PaesslerAG/gval"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"

	"github.com/sapcc/maintenance-controller/plugin"
)

func TestState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "State Suite")
}

type mockTrigger struct {
	Invoked int
	Fail    bool
}

func (n *mockTrigger) Trigger(params plugin.Parameters) error {
	n.Invoked++
	if n.Fail {
		return errors.New("mocked fail")
	}
	return nil
}

func (n *mockTrigger) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	return &mockTrigger{}, nil
}

func (n *mockTrigger) ID() string {
	return "mock"
}

func mockTriggerChain() (plugin.TriggerChain, *mockTrigger) {
	p := &mockTrigger{}
	instance := plugin.TriggerInstance{
		Plugin: p,
		Name:   "mock",
	}
	chain := plugin.TriggerChain{
		Plugins: []plugin.TriggerInstance{instance},
	}
	return chain, p
}

type mockNotification struct {
	Invoked int
	Fail    bool
}

func (n *mockNotification) Notify(params plugin.Parameters) error {
	n.Invoked++
	if n.Fail {
		return errors.New("mocked fail")
	}
	return nil
}

func (n *mockNotification) New(config *ucfgwrap.Config) (plugin.Notifier, error) {
	return &mockNotification{}, nil
}

func (n *mockNotification) ID() string {
	return "mock"
}

func mockNotificationChain(instanceCount int) (plugin.NotificationChain, *mockNotification) {
	if instanceCount < 0 {
		panic("mockNotificationChain requires at least zero instances.")
	}
	p := &mockNotification{}
	instances := make([]plugin.NotificationInstance, 0)
	for i := range instanceCount {
		instances = append(instances, plugin.NotificationInstance{
			Schedule: &plugin.NotifyPeriodic{Interval: time.Hour},
			Plugin:   p,
			Name:     fmt.Sprintf("mock%v", i),
		})
	}
	chain := plugin.NotificationChain{
		Plugins: instances,
	}
	return chain, p
}

type mockCheck struct {
	Result  bool
	Fail    bool
	Invoked int
}

func (c *mockCheck) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	c.Invoked++
	if c.Fail {
		return plugin.Failed(nil), errors.New("expected to fail")
	}
	return plugin.CheckResult{Passed: c.Result}, nil
}

func (c *mockCheck) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	return &mockCheck{}, nil
}

func (c *mockCheck) OnTransition(params plugin.Parameters) error {
	return nil
}

func (c *mockCheck) ID() string {
	return "Mock"
}

func mockCheckChain() (plugin.CheckChain, *mockCheck) {
	p := &mockCheck{}
	instance := plugin.CheckInstance{
		Plugin: p,
		Name:   "mock",
	}
	eval, err := gval.Full().NewEvaluable("mock")
	Expect(err).To(Succeed())
	chain := plugin.CheckChain{
		Plugins:   []plugin.CheckInstance{instance},
		Evaluable: eval,
	}
	return chain, p
}

var _ = Describe("NotifyDefault", func() {

	makeProfileMap := func(current, previous NodeStateLabel) map[string]*ProfileData {
		return map[string]*ProfileData{
			"p": {
				Transition: time.Now().UTC(),
				Current:    current,
				Previous:   previous,
			},
		}
	}

	It("should not execute the notification chain if the interval has not passed", func() {
		chain, notification := mockNotificationChain(1)
		data := Data{
			Profiles:      makeProfileMap(Operational, Operational),
			Notifications: map[string]time.Time{"mock0": time.Now().UTC()},
		}
		err := notifyDefault(plugin.Parameters{Profile: "p"}, &data, &chain)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

	It("should execute the notification chain if the interval has passed", func() {
		chain, notification := mockNotificationChain(1)
		data := Data{
			Profiles:      makeProfileMap(Operational, InMaintenance),
			Notifications: map[string]time.Time{"mock0": time.Now().UTC()},
		}
		notifyPeriodic, ok := chain.Plugins[0].Schedule.(*plugin.NotifyPeriodic)
		Expect(ok).To(BeTrue())
		notifyPeriodic.Interval = 30 * time.Millisecond
		time.Sleep(40 * time.Millisecond)
		err := notifyDefault(plugin.Parameters{Log: GinkgoLogr, Profile: "p"}, &data, &chain)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(1))
	})

	It("should not execute the notification chain if the interval has passed in operational state", func() {
		chain, notification := mockNotificationChain(1)
		data := Data{
			Profiles:      makeProfileMap(Operational, Operational),
			Notifications: map[string]time.Time{"mock0": time.Now().UTC()},
		}
		notifyPeriodic, ok := chain.Plugins[0].Schedule.(*plugin.NotifyPeriodic)
		Expect(ok).To(BeTrue())
		notifyPeriodic.Interval = 30 * time.Millisecond
		time.Sleep(40 * time.Millisecond)
		err := notifyDefault(plugin.Parameters{Log: GinkgoLogr, Profile: "p"}, &data, &chain)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

	It("should execute each notification instance once", func() {
		chain, notification := mockNotificationChain(3)
		data := Data{
			Profiles: makeProfileMap(InMaintenance, InMaintenance),
			Notifications: map[string]time.Time{
				"mock0": time.Date(2000, time.April, 13, 2, 3, 4, 9, time.UTC),
				"mock1": time.Date(2000, time.April, 13, 2, 3, 4, 9, time.UTC),
				"mock2": time.Date(2000, time.April, 13, 2, 3, 4, 9, time.UTC),
			},
		}
		err := notifyDefault(plugin.Parameters{Log: GinkgoLogr, Profile: "p"}, &data, &chain)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(3))
	})

})

var _ = Describe("Apply", func() {

	buildParams := func() plugin.Parameters {
		return plugin.Parameters{
			Recorder: events.NewFakeRecorder(128),
			Profile:  "profile",
			State:    string(Operational),
			Log:      GinkgoLogr,
		}
	}

	It("fails if the notification plugin fails", func() {
		chain, notify := mockNotificationChain(1)
		notify.Fail = true
		nodeState := operational{
			label: Operational,
			chains: PluginChains{
				Notification: chain,
			},
		}
		result, err := Apply(&nodeState, &v1.Node{}, &Data{Notifications: make(map[string]time.Time)}, buildParams())
		Expect(err).To(HaveOccurred())
		Expect(result.Next).To(Equal(Operational))
		Expect(result.Transitions).To(BeEmpty())
		Expect(result.Error).ToNot(BeEmpty())
	})

	It("fails if the check plugin fails", func() {
		chain, check := mockCheckChain()
		check.Fail = true
		nodeState := operational{
			label: Operational,
			chains: PluginChains{
				Transitions: []Transition{
					{
						Check: chain,
						Next:  Required,
					},
				},
			},
		}
		data := Data{Profiles: map[string]*ProfileData{"profile": {Current: Operational, Previous: Operational}}}
		result, err := Apply(&nodeState, &v1.Node{}, &data, buildParams())
		Expect(err).To(HaveOccurred())
		Expect(result.Next).To(Equal(Operational))
		Expect(result.Transitions).To(HaveLen(1))
		Expect(result.Error).ToNot(BeEmpty())
	})

	It("fails if the trigger plugin fails", func() {
		checkChain, check := mockCheckChain()
		check.Result = true
		triggerChain, trigger := mockTriggerChain()
		trigger.Fail = true
		nodeState := operational{
			label: Operational,
			chains: PluginChains{
				Transitions: []Transition{
					{
						Check:   checkChain,
						Trigger: triggerChain,
						Next:    Required,
					},
				},
			},
		}
		data := Data{Profiles: map[string]*ProfileData{"profile": {Current: Operational, Previous: Operational}}}
		result, err := Apply(&nodeState, &v1.Node{}, &data, buildParams())
		Expect(err).To(HaveOccurred())
		Expect(result.Next).To(Equal(Operational))
		Expect(result.Transitions).To(HaveLen(1))
		Expect(result.Error).ToNot(BeEmpty())
	})

	It("invokes Enter() when the previous state is different from the current state", func() {
		chain, enter := mockTriggerChain()
		nodeState := operational{
			label: Operational,
			chains: PluginChains{
				Enter: chain,
			},
		}
		data := Data{Profiles: map[string]*ProfileData{"profile": {Current: Operational, Previous: InMaintenance}}}
		result, err := Apply(&nodeState, &v1.Node{}, &data, buildParams())
		Expect(err).To(Succeed())
		Expect(result.Next).To(Equal(Operational))
		Expect(result.Transitions).To(BeEmpty())
		Expect(enter.Invoked).To(Equal(1))
	})

})

var _ = Describe("ParseData", func() {
	It("should initialize the notification times map", func() {
		data, err := ParseData("{}")
		Expect(err).To(Succeed())
		Expect(data.Notifications).ToNot(BeNil())
	})

	It("should fail with invalid json", func() {
		_, err := ParseData("{{}")
		Expect(err).ToNot(Succeed())
	})
})

var _ = Describe("ValidateLabel", func() {
	It("fails on invalid input", func() {
		_, err := ValidateLabel("hello")
		Expect(err).ToNot(Succeed())
	})
})

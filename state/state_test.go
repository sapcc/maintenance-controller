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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/PaesslerAG/gval"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
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
		return fmt.Errorf("mocked fail")
	}
	return nil
}

func (n *mockTrigger) New(config *ucfgwrap.Config) (plugin.Trigger, error) {
	return &mockTrigger{}, nil
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

type mockNotificaiton struct {
	Invoked int
	Fail    bool
}

func (n *mockNotificaiton) Notify(params plugin.Parameters) error {
	n.Invoked++
	if n.Fail {
		return fmt.Errorf("mocked fail")
	}
	return nil
}

func (n *mockNotificaiton) New(config *ucfgwrap.Config) (plugin.Notifier, error) {
	return &mockNotificaiton{}, nil
}

func mockNotificationChain(instanceCount int) (plugin.NotificationChain, *mockNotificaiton) {
	if instanceCount < 0 {
		panic("mockNotificationChain requires at least zero instances.")
	}
	p := &mockNotificaiton{}
	instances := make([]plugin.NotificationInstance, 0)
	for i := 0; i < instanceCount; i++ {
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

	It("should not execute the notification chain if the interval has not passed", func() {
		chain, notification := mockNotificationChain(1)
		err := notifyDefault(plugin.Parameters{}, &Data{
			LastTransition:        time.Now().UTC(),
			LastNotificationTimes: map[string]time.Time{"mock0": time.Now().UTC()},
		}, &chain, Operational, Operational)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

	It("should execute the notification chain if the interval has passed", func() {
		chain, notification := mockNotificationChain(1)
		data := Data{
			LastTransition:        time.Now().UTC(),
			LastNotificationTimes: map[string]time.Time{"mock0": time.Now().UTC()},
		}
		chain.Plugins[0].Schedule.(*plugin.NotifyPeriodic).Interval = 30 * time.Millisecond
		time.Sleep(40 * time.Millisecond)
		err := notifyDefault(plugin.Parameters{Log: logr.Discard()}, &data, &chain, Operational, InMaintenance)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(1))
	})

	It("should not execute the notification chain if the interval has passed in operational state", func() {
		chain, notification := mockNotificationChain(1)
		data := Data{
			LastTransition:        time.Now().UTC(),
			LastNotificationTimes: map[string]time.Time{"mock0": time.Now().UTC()},
		}
		chain.Plugins[0].Schedule.(*plugin.NotifyPeriodic).Interval = 30 * time.Millisecond
		time.Sleep(40 * time.Millisecond)
		err := notifyDefault(plugin.Parameters{Log: logr.Discard()}, &data, &chain, Operational, Operational)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

	It("should execute each notification instance once", func() {
		chain, notification := mockNotificationChain(3)
		data := Data{
			LastTransition: time.Now().UTC(),
			LastNotificationTimes: map[string]time.Time{
				"mock0": time.Date(2000, time.April, 13, 2, 3, 4, 9, time.UTC),
				"mock1": time.Date(2000, time.April, 13, 2, 3, 4, 9, time.UTC),
				"mock2": time.Date(2000, time.April, 13, 2, 3, 4, 9, time.UTC),
			},
		}
		err := notifyDefault(plugin.Parameters{Log: logr.Discard()}, &data, &chain, InMaintenance, InMaintenance)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(3))
	})

})

var _ = Describe("Apply", func() {

	buildParams := func() plugin.Parameters {
		return plugin.Parameters{
			Recorder: record.NewFakeRecorder(128),
			Profile:  "profile",
			State:    string(Operational),
			Log:      logr.Discard(),
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
		result, err := Apply(&nodeState, &v1.Node{}, &Data{LastNotificationTimes: make(map[string]time.Time)}, buildParams())
		Expect(err).To(HaveOccurred())
		Expect(result.Next).To(Equal(Operational))
		Expect(result.Transitions).To(HaveLen(0))
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
		result, err := Apply(&nodeState, &v1.Node{}, &Data{}, buildParams())
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
		result, err := Apply(&nodeState, &v1.Node{}, &Data{}, buildParams())
		Expect(err).To(HaveOccurred())
		Expect(result.Next).To(Equal(Operational))
		Expect(result.Transitions).To(HaveLen(1))
		Expect(result.Error).ToNot(BeEmpty())
	})

})

var _ = Describe("ParseData", func() {

	It("should initialize the notification times map", func() {
		var node v1.Node
		node.Annotations = map[string]string{constants.DataAnnotationKey: "{}"}
		data, err := ParseData(&node)
		Expect(err).To(Succeed())
		Expect(data.LastNotificationTimes).ToNot(BeNil())
	})

	It("should fail with invalid json", func() {
		var node v1.Node
		node.Annotations = map[string]string{constants.DataAnnotationKey: "{{}"}
		_, err := ParseData(&node)
		Expect(err).ToNot(Succeed())
	})

})

var _ = Describe("ValidateLabel", func() {

	It("fails on invalid input", func() {
		_, err := ValidateLabel("hello")
		Expect(err).ToNot(Succeed())
	})

})

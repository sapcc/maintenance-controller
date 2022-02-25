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
	"github.com/elastic/go-ucfg"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
)

func TestPlugins(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Suite")
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

func (n *mockTrigger) New(config *ucfg.Config) (plugin.Trigger, error) {
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

func (n *mockNotificaiton) New(config *ucfg.Config) (plugin.Notifier, error) {
	return &mockNotificaiton{}, nil
}

func mockNotificationChain() (plugin.NotificationChain, *mockNotificaiton) {
	p := &mockNotificaiton{}
	instance := plugin.NotificationInstance{
		Schedule: &plugin.NotifyPeriodic{Interval: time.Hour},
		Plugin:   p,
		Name:     "mock",
	}
	chain := plugin.NotificationChain{
		Plugins: []plugin.NotificationInstance{instance},
	}
	return chain, p
}

type mockCheck struct {
	Result  bool
	Fail    bool
	Invoked int
}

func (c *mockCheck) Check(params plugin.Parameters) (bool, error) {
	c.Invoked++
	if c.Fail {
		return false, errors.New("expected to fail")
	}
	return c.Result, nil
}

func (c *mockCheck) New(config *ucfg.Config) (plugin.Checker, error) {
	return &mockCheck{}, nil
}

func (c *mockCheck) AfterEval(chainResult bool, params plugin.Parameters) error {
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
		chain, notification := mockNotificationChain()
		err := notifyDefault(plugin.Parameters{}, &Data{
			LastTransition:        time.Now(),
			LastNotificationTimes: map[string]time.Time{"mock": time.Now()},
			LastNotificationState: Operational,
		}, &chain, Operational)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

	It("should execute the notification chain if the interval has passed", func() {
		chain, notification := mockNotificationChain()
		data := Data{
			LastTransition:        time.Now(),
			LastNotificationTimes: map[string]time.Time{"mock": time.Now()},
			LastNotificationState: InMaintenance,
		}
		chain.Plugins[0].Schedule.(*plugin.NotifyPeriodic).Interval = 30 * time.Millisecond
		time.Sleep(40 * time.Millisecond)
		err := notifyDefault(plugin.Parameters{Log: logr.Discard()}, &data, &chain, Operational)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(1))
	})

	It("should not execute the notification chain if the interval has passed in operational state", func() {
		chain, notification := mockNotificationChain()
		data := Data{
			LastTransition:        time.Now(),
			LastNotificationTimes: map[string]time.Time{"mock": time.Now()},
			LastNotificationState: Operational,
		}
		chain.Plugins[0].Schedule.(*plugin.NotifyPeriodic).Interval = 30 * time.Millisecond
		time.Sleep(40 * time.Millisecond)
		err := notifyDefault(plugin.Parameters{Log: logr.Discard()}, &data, &chain, Operational)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

})

var _ = Describe("Apply", func() {

	buildParams := func() plugin.Parameters {
		return plugin.Parameters{
			Recorder: record.NewFakeRecorder(128),
			Profile:  plugin.ProfileInfo{Current: "profile"},
			State:    string(Operational),
			Log:      logr.Discard(),
		}
	}

	It("fails if the notification plugin fails", func() {
		chain, notify := mockNotificationChain()
		notify.Fail = true
		nodeState := operational{
			label: Operational,
			chains: PluginChains{
				Notification: chain,
			},
		}
		result, err := Apply(&nodeState, &v1.Node{}, &Data{LastNotificationTimes: make(map[string]time.Time)}, buildParams())
		Expect(err).To(HaveOccurred())
		Expect(result).To(Equal(Operational))
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
		Expect(result).To(Equal(Operational))
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
		Expect(result).To(Equal(Operational))
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

})

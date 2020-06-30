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
	"testing"
	"time"

	"github.com/elastic/go-ucfg"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
)

func TestPlugins(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Suite")
}

type successfulTrigger struct {
	Invoked int
}

func (n *successfulTrigger) Trigger(params plugin.Parameters) error {
	n.Invoked++
	return nil
}

func (n *successfulTrigger) New(config *ucfg.Config) (plugin.Trigger, error) {
	return &successfulTrigger{}, nil
}

func mockTriggerChain() (plugin.TriggerChain, *successfulTrigger) {
	p := &successfulTrigger{}
	instance := plugin.TriggerInstance{
		Plugin: p,
		Name:   "mock",
	}
	chain := plugin.TriggerChain{
		Plugins: []plugin.TriggerInstance{instance},
	}
	return chain, p
}

type successfulNotification struct {
	Invoked int
}

func (n *successfulNotification) Notify(params plugin.Parameters) error {
	n.Invoked++
	return nil
}

func (n *successfulNotification) New(config *ucfg.Config) (plugin.Notifier, error) {
	return &successfulNotification{}, nil
}

func mockNotificationChain() (plugin.NotificationChain, *successfulNotification) {
	p := &successfulNotification{}
	instance := plugin.NotificationInstance{
		Plugin: p,
		Name:   "mock",
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

func mockCheckChain() (plugin.CheckChain, *mockCheck) {
	p := &mockCheck{}
	instance := plugin.CheckInstance{
		Plugin: p,
		Name:   "mock",
	}
	chain := plugin.CheckChain{
		Plugins: []plugin.CheckInstance{instance},
	}
	return chain, p
}

var _ = Describe("NotifyDefault", func() {

	It("should not execute the notification chain if the interval has not passed", func() {
		chain, notification := mockNotificationChain()
		err := notifyDefault(plugin.Parameters{}, &Data{
			LastTransition:        time.Now(),
			LastNotification:      time.Now(),
			LastNotificationState: Operational,
		}, 1*time.Hour, &chain, Operational)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(0))
	})

	It("should execute the notification chain if the interval has passed", func() {
		chain, notification := mockNotificationChain()
		data := Data{
			LastTransition:        time.Now(),
			LastNotification:      time.Now(),
			LastNotificationState: Operational,
		}
		time.Sleep(40 * time.Millisecond)
		err := notifyDefault(plugin.Parameters{}, &data, 30*time.Millisecond, &chain, Operational)
		Expect(err).To(Succeed())
		Expect(notification.Invoked).To(Equal(1))
	})

})

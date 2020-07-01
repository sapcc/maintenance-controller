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

package plugin

import (
	"errors"
	"testing"

	"github.com/elastic/go-ucfg/yaml"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestPlugins(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Suite")
}

var _ = Describe("CheckError", func() {
	chainErr := ChainError{
		Message: "msg",
		Err:     errors.New("err"),
	}

	It("should combine messages", func() {
		result := chainErr.Error()
		Expect(result).To(Equal("msg: err"))
	})

	It("should unwrap the root error", func() {
		err := chainErr.Unwrap()
		Expect(err.Error()).To(Equal("err"))
	})
})

var _ = Describe("Registry", func() {

	Context("is uninitalized", func() {

		It("should find plugins", func() {
			registry := NewRegistry()
			// a new registry should not have any instances
			Expect(registry.CheckInstances).To(HaveLen(0))
			Expect(registry.NotificationInstances).To(HaveLen(0))
			Expect(registry.TriggerInstances).To(HaveLen(0))
			// the lengths below need to be update when plugins are actually added to the registry
			Expect(registry.CheckPlugins).To(HaveLen(0))
			Expect(registry.NotificationPlugins).To(HaveLen(0))
			Expect(registry.TriggerPlugins).To(HaveLen(0))
		})

		Context("gets a valid configuration", func() {

			It("loads check plugin instances", func() {
				var configStr = `check:
                - someCheckPlugin:
                    name: test
                    config:
                        key: somekey
                        value: someval
                `
				registry := NewRegistry()
				registry.CheckPlugins["someCheckPlugin"] = &trueCheck{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				err = registry.LoadInstances(config)
				Expect(err).To(Succeed())
				Expect(registry.CheckInstances).To(HaveLen(1))
				instance := registry.CheckInstances["test"]
				Expect(instance.Name).To(Equal("test"))
			})

			It("loads notification plugin instances", func() {
				var configStr = `notify:
                - someNotificationPlugin:
                    name: test
                    config:
                        key: somekey
                        value: someval
                `
				registry := NewRegistry()
				registry.NotificationPlugins["someNotificationPlugin"] = &successfulNotification{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				err = registry.LoadInstances(config)
				Expect(err).To(Succeed())
				Expect(registry.NotificationInstances).To(HaveLen(1))
				instance := registry.NotificationInstances["test"]
				Expect(instance.Name).To(Equal("test"))
			})

			It("loads trigger plugin instances", func() {
				var configStr = `trigger:
                - someTriggerPlugin:
                    name: test
                    config:
                        key: somekey
                        value: someval
                `
				registry := NewRegistry()
				registry.TriggerPlugins["someTriggerPlugin"] = &successfulTrigger{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				err = registry.LoadInstances(config)
				Expect(err).To(Succeed())
				Expect(registry.TriggerInstances).To(HaveLen(1))
				instance := registry.TriggerInstances["test"]
				Expect(instance.Name).To(Equal("test"))
			})

		})

	})

	Context("is initalized", func() {

		Context("gets valid config strings", func() {

			var (
				config   string
				registry Registry
			)

			BeforeEach(func() {
				config = "instance && instance"
				registry = NewRegistry()
				registry.CheckInstances["instance"] = CheckInstance{Plugin: &trueCheck{}, Name: "instance"}
				registry.NotificationInstances["instance"] = NotificationInstance{Plugin: &successfulNotification{}, Name: "instance"}
				registry.TriggerInstances["instance"] = TriggerInstance{Plugin: &successfulTrigger{}, Name: "instance"}
			})

			It("should create CheckChains", func() {
				chain, err := registry.NewCheckChain(config)
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(HaveLen(2))
			})

			It("should create CheckChains using all possible operators", func() {
				config := "instance && !(instance || instance)"
				chain, err := registry.NewCheckChain(config)
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(HaveLen(3))
				Expect(chain.Plugins[0].Name).To(Equal("instance"))
				Expect(chain.Plugins[1].Name).To(Equal("instance"))
				Expect(chain.Plugins[2].Name).To(Equal("instance"))
			})

			It("should create NotificationChains", func() {
				chain, err := registry.NewNotificationChain(config)
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(HaveLen(2))
			})

			It("should create TriggerChains", func() {
				chain, err := registry.NewTriggerChain(config)
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(HaveLen(2))
			})

		})

		Context("gets invalid config strings", func() {

			var (
				config   string
				registry Registry
			)

			BeforeEach(func() {
				config = "invalid&&invalid"
				registry = NewRegistry()
			})

			It("should not create CheckChains", func() {
				chain, err := registry.NewCheckChain(config)
				Expect(err).To(HaveOccurred())
				Expect(chain.Plugins).To(HaveLen(0))
			})

			It("should not create NotificationChains", func() {
				chain, err := registry.NewNotificationChain(config)
				Expect(err).To(HaveOccurred())
				Expect(chain.Plugins).To(HaveLen(0))
			})

			It("should not create TriggerChains", func() {
				chain, err := registry.NewTriggerChain(config)
				Expect(err).To(HaveOccurred())
				Expect(chain.Plugins).To(HaveLen(0))
			})

		})

	})

})

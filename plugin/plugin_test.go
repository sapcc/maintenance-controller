// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"errors"
	"testing"

	"github.com/elastic/go-ucfg/yaml"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
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
	var emptyConfig *ucfgwrap.Config

	BeforeEach(func() {
		cfg, err := ucfgwrap.FromJSON([]byte("{}"))
		Expect(err).To(Succeed())
		emptyConfig = &cfg
	})

	Context("is uninitialized", func() {
		It("should find plugins", func() {
			registry := NewRegistry()
			// a new registry should not have any instances
			Expect(registry.CheckInstances).To(BeEmpty())
			Expect(registry.NotificationInstances).To(BeEmpty())
			Expect(registry.TriggerInstances).To(BeEmpty())
			// the lengths below need to be update when plugins are actually added to the registry
			Expect(registry.CheckPlugins).To(BeEmpty())
			Expect(registry.NotificationPlugins).To(BeEmpty())
			Expect(registry.TriggerPlugins).To(BeEmpty())
		})

		Context("gets a valid configuration", func() {
			It("loads check plugin instances", func() {
				var configStr = `check:
                - type: someCheckPlugin
                  name: test
                  config:
                    key: somekey
                    value: someval
                `
				registry := NewRegistry()
				registry.CheckPlugins["someCheckPlugin"] = &trueCheck{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				var descriptor InstancesDescriptor
				Expect(config.Unpack(&descriptor)).To(Succeed())
				err = registry.LoadInstances(emptyConfig, &descriptor)
				Expect(err).To(Succeed())
				Expect(registry.CheckInstances).To(HaveLen(1))
				instance := registry.CheckInstances["test"]
				Expect(instance.Name).To(Equal("test"))
			})

			It("loads periodic notification plugin instances", func() {
				var configStr = `notify:
                - type: someNotificationPlugin
                  name: test
                  schedule:
                    type: periodic
                    config:
                      interval: 5m
                  config:
                    key: somekey
                    value: someval
                `
				registry := NewRegistry()
				registry.NotificationPlugins["someNotificationPlugin"] = &successfulNotification{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				var descriptor InstancesDescriptor
				Expect(config.Unpack(&descriptor)).To(Succeed())
				err = registry.LoadInstances(emptyConfig, &descriptor)
				Expect(err).To(Succeed())
				Expect(registry.NotificationInstances).To(HaveLen(1))
				instance := registry.NotificationInstances["test"]
				Expect(instance.Name).To(Equal("test"))
			})

			It("loads scheduled notification plugin instances", func() {
				var configStr = `notify:
                - type: someNotificationPlugin
                  name: test
                  schedule:
                    type: scheduled
                    config:
                      instant: 11:00
                      weekdays: ["tue"]
                  config:
                    key: somekey
                    value: someval
                `
				registry := NewRegistry()
				registry.NotificationPlugins["someNotificationPlugin"] = &successfulNotification{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				var descriptor InstancesDescriptor
				Expect(config.Unpack(&descriptor)).To(Succeed())
				err = registry.LoadInstances(emptyConfig, &descriptor)
				Expect(err).To(Succeed())
				Expect(registry.NotificationInstances).To(HaveLen(1))
				instance := registry.NotificationInstances["test"]
				Expect(instance.Name).To(Equal("test"))
			})

			It("loads trigger plugin instances", func() {
				var configStr = `trigger:
                - type: someTriggerPlugin
                  name: test
                  config:
                    key: somekey
                    value: someval
                `
				registry := NewRegistry()
				registry.TriggerPlugins["someTriggerPlugin"] = &successfulTrigger{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				var descriptor InstancesDescriptor
				Expect(config.Unpack(&descriptor)).To(Succeed())
				err = registry.LoadInstances(emptyConfig, &descriptor)
				Expect(err).To(Succeed())
				Expect(registry.TriggerInstances).To(HaveLen(1))
				instance := registry.TriggerInstances["test"]
				Expect(instance.Name).To(Equal("test"))
			})

		})

		Context("gets an invalid configuration", func() {

			assertError := func(configStr string) {
				registry := NewRegistry()
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				var descriptor InstancesDescriptor
				err = config.Unpack(&descriptor)
				if err != nil {
					return
				}
				err = registry.LoadInstances(emptyConfig, &descriptor)
				Expect(err).To(HaveOccurred())
			}

			It("should not parse check instances if plugin type contains more than 1 entry", func() {
				var configStr = `check:
                - someCheckPlugin:
                  invalid:
                    name: test
                    config:
                        key: somekey
                `
				assertError(configStr)
			})

			It("should not parse check instances if instance has no configuration", func() {
				var configStr = `check:
                - someCheckPlugin:
                `
				assertError(configStr)
			})

			It("should not parse check instances if an instance has no name", func() {
				var configStr = `check:
                - someCheckPlugin:
                    config:
                        key: somekey
                `
				assertError(configStr)
			})

			It("should not parse check instances if an instance as a name, but no config", func() {
				var configStr = `check:
                - someCheckPlugin:
                    name: an_instance
                `
				assertError(configStr)
			})

			It("should not parse check instances if plugin type is not registered", func() {
				var configStr = `check:
                - someCheckPlugin:
                    name: test
                    config: null
                `
				assertError(configStr)
			})

			It("should not parse notification instances if plugin type contains more than 1 entry", func() {
				var configStr = `notify:
                - someNotificationPlugin:
                  invalid:
                    name: test
                    config:
                        key: somekey
                `
				assertError(configStr)
			})

			It("should not parse notification instances if instance has no configuration", func() {
				var configStr = `notify:
                - someNotificationPlugin:
                `
				assertError(configStr)
			})

			It("should not parse notification instances if an instance has no name", func() {
				var configStr = `notify:
                - someNotificationPlugin:
                    config:
                        key: somekey
                `
				assertError(configStr)
			})

			It("should not parse notification instances if an instance as a name, but no config", func() {
				var configStr = `notify:
                - someNotificationPlugin:
                    name: an_instance
                `
				assertError(configStr)
			})

			It("should not parse notification instances if plugin type is not registered", func() {
				var configStr = `notify:
                - someNotificationPlugin:
                    name: test
                    config: null
                `
				assertError(configStr)
			})

			It("should not parse trigger instances if plugin type contains more than 1 entry", func() {
				var configStr = `trigger:
                - someTriggerPlugin:
                  invalid:
                    name: test
                    config:
                        key: somekey
                `
				assertError(configStr)
			})

			It("should not parse trigger instances if instance has no configuration", func() {
				var configStr = `trigger:
                - someTriggerPlugin:
                `
				assertError(configStr)
			})

			It("should not parse trigger instances if an instance has no name", func() {
				var configStr = `trigger:
                - someTriggerPlugin:
                    config:
                        key: somekey
                `
				assertError(configStr)
			})

			It("should not parse trigger instances if an instance as a name, but no config", func() {
				var configStr = `trigger:
                - someTriggerPlugin:
                    name: an_instance
                `
				assertError(configStr)
			})

			It("should not parse trigger instances if plugin type is not registered", func() {
				var configStr = `trigger:
                - someTriggerPlugin:
                    name: test
                    config: null
                `
				assertError(configStr)
			})

			It("fails to load notification plugin instances with unknown scheduler", func() {
				var configStr = `notify:
                - type: someNotificationPlugin
                  name: test
                  schedule:
                    type: unknown
                    config: null
                  config:
                    key: somekey
                    value: someval
                `
				registry := NewRegistry()
				registry.NotificationPlugins["someNotificationPlugin"] = &successfulNotification{}
				config, err := yaml.NewConfig([]byte(configStr))
				Expect(err).To(Succeed())
				var descriptor InstancesDescriptor
				Expect(config.Unpack(&descriptor)).To(Succeed())
				err = registry.LoadInstances(emptyConfig, &descriptor)
				Expect(err).ToNot(Succeed())
			})
		})
	})

	Context("is initialized", func() {
		Context("gets valid config strings", func() {
			var (
				config   string
				registry Registry
			)

			BeforeEach(func() {
				config = "instance && instance"
				registry = NewRegistry()
				registry.CheckInstances["instance"] = CheckInstance{
					Plugin: &trueCheck{},
					Name:   "instance",
				}
				registry.NotificationInstances["instance"] = NotificationInstance{
					Plugin: &successfulNotification{},
					Name:   "instance",
				}
				registry.TriggerInstances["instance"] = TriggerInstance{
					Plugin: &successfulTrigger{},
					Name:   "instance",
				}
			})

			It("should create an empty CheckChain from an empty config", func() {
				chain, err := registry.NewCheckChain("")
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(BeEmpty())
			})

			It("should create CheckChains", func() {
				chain, err := registry.NewCheckChain(config)
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(HaveLen(2))
				Expect(chain.Expression).To(Equal("instance && instance"))
			})

			It("should create CheckChains using all possible operators", func() {
				cfg := "instance && !(instance || instance)"
				chain, err := registry.NewCheckChain(cfg)
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(HaveLen(3))
				Expect(chain.Plugins[0].Name).To(Equal("instance"))
				Expect(chain.Plugins[1].Name).To(Equal("instance"))
				Expect(chain.Plugins[2].Name).To(Equal("instance"))
				Expect(chain.Expression).To(Equal(cfg))
			})

			It("should create an empty NotificationChain from an empty config", func() {
				chain, err := registry.NewNotificationChain("")
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(BeEmpty())
			})

			It("should create NotificationChains", func() {
				chain, err := registry.NewNotificationChain(config)
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(HaveLen(2))
			})

			It("should create an empty TriggerChain from an empty config", func() {
				chain, err := registry.NewTriggerChain("")
				Expect(err).To(Succeed())
				Expect(chain.Plugins).To(BeEmpty())
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
				Expect(chain.Plugins).To(BeEmpty())
			})

			It("should not create NotificationChains", func() {
				chain, err := registry.NewNotificationChain(config)
				Expect(err).To(HaveOccurred())
				Expect(chain.Plugins).To(BeEmpty())
			})

			It("should not create TriggerChains", func() {
				chain, err := registry.NewTriggerChain(config)
				Expect(err).To(HaveOccurred())
				Expect(chain.Plugins).To(BeEmpty())
			})
		})
	})
})

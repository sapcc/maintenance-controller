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

package controllers

import (
	"time"

	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/plugin/impl"
	"github.com/sapcc/maintenance-controller/state"
)

// Config represents the controllers global configuration.
type Config struct {
	// RequeueInterval defines a duration after the a node is reconceiled again by the controller
	RequeueInterval time.Duration
	// NotificationInterval specifies a duration after which notifications are resend
	NotificationInterval time.Duration
	// Registry is the global plugin Registry
	Registry plugin.Registry
	// Profiles contains all known profiles
	Profiles map[string]state.Profile
}

// LoadConfig (re-)initializes the config with values provided by the given ucfg.Config.
func LoadConfig(config *ucfg.Config) (*Config, error) {
	c := &Config{}
	global := struct {
		Intervals struct {
			Requeue time.Duration `config:"requeue" validate:"required"`
			Notify  time.Duration `config:"notify" validate:"required"`
		} `config:"intervals" validate:"required"`
		Instances *ucfg.Config
		Profiles  *ucfg.Config
	}{}
	err := config.Unpack(&global)
	if err != nil {
		return nil, err
	}
	c.RequeueInterval = global.Intervals.Requeue
	c.NotificationInterval = global.Intervals.Notify

	c.Registry = plugin.NewRegistry()
	addPluginsToRegistry(&c.Registry)
	err = c.Registry.LoadInstances(global.Instances)
	if err != nil {
		return nil, err
	}

	c.Profiles = make(map[string]state.Profile)
	err = loadProfiles(global.Profiles, c)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func loadProfiles(config *ucfg.Config, c *Config) error {
	// add an empty default profile
	c.Profiles[DefaultProfileName] = state.Profile{
		Name: DefaultProfileName,
		Chains: map[state.NodeStateLabel]state.PluginChains{
			state.Operational:   {},
			state.InMaintenance: {},
			state.Required:      {},
		},
	}
	for _, profileName := range config.GetFields() {
		currentProfile, err := config.Child(profileName, -1)
		if err != nil {
			return err
		}
		states := struct {
			Operational *ucfg.Config `config:"operational"`
			Required    *ucfg.Config `config:"maintenance-required"`
			Maintenance *ucfg.Config `config:"in-maintenance"`
		}{}
		err = currentProfile.Unpack(&states)
		if err != nil {
			return err
		}
		operationalChain, err := loadPluginChains(states.Operational, &c.Registry)
		if err != nil {
			return err
		}
		requiredChain, err := loadPluginChains(states.Required, &c.Registry)
		if err != nil {
			return err
		}
		maintenanceChain, err := loadPluginChains(states.Maintenance, &c.Registry)
		if err != nil {
			return err
		}
		c.Profiles[profileName] = state.Profile{
			Name: profileName,
			Chains: map[state.NodeStateLabel]state.PluginChains{
				state.Operational:   operationalChain,
				state.Required:      requiredChain,
				state.InMaintenance: maintenanceChain,
			},
		}
	}
	return nil
}

func loadPluginChains(config *ucfg.Config, registry *plugin.Registry) (state.PluginChains, error) {
	var chains state.PluginChains
	if config == nil {
		return chains, nil
	}
	texts := struct {
		Check   string
		Notify  string
		Trigger string
	}{}
	err := config.Unpack(&texts)
	if err != nil {
		return chains, err
	}
	checkChain, err := registry.NewCheckChain(texts.Check)
	if err != nil {
		return chains, err
	}
	chains.Check = checkChain
	notificationChain, err := registry.NewNotificationChain(texts.Notify)
	if err != nil {
		return chains, err
	}
	chains.Notification = notificationChain
	triggerChain, err := registry.NewTriggerChain(texts.Trigger)
	if err != nil {
		return chains, err
	}
	chains.Trigger = triggerChain
	return chains, nil
}

// addPluginsToRegistry adds known plugins to the registry.
func addPluginsToRegistry(registry *plugin.Registry) {
	registry.CheckPlugins["hasAnnotation"] = &impl.HasAnnotation{}
	registry.CheckPlugins["hasLabel"] = &impl.HasLabel{}
	registry.CheckPlugins["maxMaintenance"] = &impl.MaxMaintenance{}
	registry.CheckPlugins["timeWindow"] = &impl.TimeWindow{}
	registry.CheckPlugins["wait"] = &impl.Wait{}

	registry.NotificationPlugins["slack"] = &impl.Slack{}
	registry.NotificationPlugins["mail"] = &impl.Mail{}

	registry.TriggerPlugins["alterAnnotation"] = &impl.AlterAnnotation{}
	registry.TriggerPlugins["alterLabel"] = &impl.AlterLabel{}
}

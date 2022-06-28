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
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/plugin/impl"
	"github.com/sapcc/maintenance-controller/state"
)

type ProfileDescriptor struct {
	Name                string `config:"name" validate:"required"`
	Operational         StateDescriptor
	MaintenanceRequired StateDescriptor `config:"maintenance-required"`
	InMaintenance       StateDescriptor `config:"in-maintenance"`
}

type StateDescriptor struct {
	Notify      string
	Transitions []TransitionDescriptor
}

type TransitionDescriptor struct {
	Check   string `config:"check" validate:"required"`
	Next    string `config:"next" validate:"required"`
	Trigger string
}

// ConfigDescriptor describes the configuration structure to be parsed.
type ConfigDescriptor struct {
	Intervals struct {
		Requeue time.Duration `config:"requeue" validate:"required"`
	} `config:"intervals" validate:"required"`
	Instances plugin.InstancesDescriptor
	Profiles  []ProfileDescriptor
}

// Config represents the controllers global configuration.
type Config struct {
	// RequeueInterval defines a duration after the a node is reconceiled again by the controller
	RequeueInterval time.Duration
	// Profiles contains all known profiles
	Profiles map[string]state.Profile
	// Contains reference to all plugins and their instances
	Registry plugin.Registry
}

// LoadConfig (re-)initializes the config with values provided by the given ucfg.Config.
func LoadConfig(config *ucfg.Config) (*Config, error) {
	var global ConfigDescriptor
	err := config.Unpack(&global)
	if err != nil {
		return nil, err
	}
	registry := plugin.NewRegistry()
	addPluginsToRegistry(&registry)
	err = registry.LoadInstances(&global.Instances)
	if err != nil {
		return nil, err
	}
	profileMap, err := loadProfiles(global.Profiles, &registry)
	if err != nil {
		return nil, err
	}
	return &Config{
		RequeueInterval: global.Intervals.Requeue,
		Profiles:        profileMap,
		Registry:        registry,
	}, nil
}

func loadProfiles(profiles []ProfileDescriptor, registry *plugin.Registry) (map[string]state.Profile, error) {
	profileMap := make(map[string]state.Profile)
	// add an empty default profile
	profileMap[constants.DefaultProfileName] = state.Profile{
		Name: constants.DefaultProfileName,
		Chains: map[state.NodeStateLabel]state.PluginChains{
			state.Operational:   {},
			state.InMaintenance: {},
			state.Required:      {},
		},
	}
	for _, profile := range profiles {
		operationalChain, err := loadPluginChains(profile.Operational, registry)
		if err != nil {
			return nil, err
		}
		requiredChain, err := loadPluginChains(profile.MaintenanceRequired, registry)
		if err != nil {
			return nil, err
		}
		maintenanceChain, err := loadPluginChains(profile.InMaintenance, registry)
		if err != nil {
			return nil, err
		}
		profileMap[profile.Name] = state.Profile{
			Name: profile.Name,
			Chains: map[state.NodeStateLabel]state.PluginChains{
				state.Operational:   operationalChain,
				state.Required:      requiredChain,
				state.InMaintenance: maintenanceChain,
			},
		}
	}
	return profileMap, nil
}

func loadPluginChains(config StateDescriptor, registry *plugin.Registry) (state.PluginChains, error) {
	var chains state.PluginChains
	notificationChain, err := registry.NewNotificationChain(config.Notify)
	if err != nil {
		return chains, err
	}
	chains.Notification = notificationChain
	chains.Transitions = make([]state.Transition, 0)
	for _, transitionConfig := range config.Transitions {
		var transition state.Transition
		checkChain, err := registry.NewCheckChain(transitionConfig.Check)
		if err != nil {
			return chains, err
		}
		transition.Check = checkChain
		triggerChain, err := registry.NewTriggerChain(transitionConfig.Trigger)
		if err != nil {
			return chains, err
		}
		transition.Trigger = triggerChain
		label, err := state.ValidateLabel(transitionConfig.Next)
		if err != nil {
			return chains, err
		}
		transition.Next = label
		chains.Transitions = append(chains.Transitions, transition)
	}
	return chains, nil
}

// addPluginsToRegistry adds known plugins to the registry.
func addPluginsToRegistry(registry *plugin.Registry) {
	registry.CheckPlugins["affinity"] = &impl.Affinity{}
	registry.CheckPlugins["clusterSemver"] = &impl.ClusterSemver{}
	registry.CheckPlugins["condition"] = &impl.Condition{}
	registry.CheckPlugins["hasAnnotation"] = &impl.HasAnnotation{}
	registry.CheckPlugins["hasLabel"] = &impl.HasLabel{}
	registry.CheckPlugins["kubernikusCount"] = &impl.KubernikusCount{}
	registry.CheckPlugins["maxMaintenance"] = &impl.MaxMaintenance{}
	registry.CheckPlugins["nodeCount"] = &impl.NodeCount{}
	registry.CheckPlugins["stagger"] = &impl.Stagger{}
	registry.CheckPlugins["timeWindow"] = &impl.TimeWindow{}
	registry.CheckPlugins["wait"] = &impl.Wait{}
	registry.CheckPlugins["waitExclude"] = &impl.WaitExclude{}

	registry.NotificationPlugins["mail"] = &impl.Mail{}
	registry.NotificationPlugins["slack"] = &impl.SlackWebhook{}
	registry.NotificationPlugins["slackThread"] = &impl.SlackThread{}

	registry.TriggerPlugins["alterAnnotation"] = &impl.AlterAnnotation{}
	registry.TriggerPlugins["alterLabel"] = &impl.AlterLabel{}
}

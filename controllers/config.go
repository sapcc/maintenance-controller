// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"time"

	"github.com/sapcc/ucfgwrap"

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
	Enter       string
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
	Dashboard struct {
		LabelFilter []string `config:"labelFilter"`
	} `config:"dashboard"`
}

// Config represents the controllers global configuration.
type Config struct {
	// RequeueInterval defines a duration after the a node is reconceiled again by the controller
	RequeueInterval time.Duration
	// Profiles contains all known profiles
	Profiles map[string]state.Profile
	// Contains reference to all plugins and their instances
	Registry plugin.Registry
	// Keys of labels to show on the dashboard
	DashboardLabelFilter []string
}

// LoadConfig (re-)initializes the config with values provided by the given ucfg.Config.
func LoadConfig(config *ucfgwrap.Config) (*Config, error) {
	var global ConfigDescriptor
	err := config.Unpack(&global)
	if err != nil {
		return nil, err
	}
	registry := plugin.NewRegistry()
	addPluginsToRegistry(&registry)
	err = registry.LoadInstances(config, &global.Instances)
	if err != nil {
		return nil, err
	}
	profileMap, err := loadProfiles(global.Profiles, &registry)
	if err != nil {
		return nil, err
	}
	dashboardLabelFilter := make([]string, 0)
	if global.Dashboard.LabelFilter != nil {
		dashboardLabelFilter = global.Dashboard.LabelFilter
	}
	return &Config{
		RequeueInterval:      global.Intervals.Requeue,
		Profiles:             profileMap,
		Registry:             registry,
		DashboardLabelFilter: dashboardLabelFilter,
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

	enterChain, err := registry.NewTriggerChain(config.Enter)
	if err != nil {
		return chains, err
	}
	chains.Enter = enterChain

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
	checkers := []plugin.Checker{
		&impl.Affinity{},
		&impl.AnyLabel{},
		&impl.CheckHypervisor{},
		&impl.ClusterSemver{},
		&impl.Condition{},
		&impl.HasAnnotation{},
		&impl.HasLabel{},
		&impl.HypervisorCondition{},
		&impl.KubernikusCount{},
		&impl.MaxMaintenance{},
		&impl.NodeCount{},
		&impl.PrometheusInstant{},
		&impl.Stagger{},
		&impl.TimeWindow{},
		&impl.Wait{},
		&impl.WaitExclude{},
	}
	for _, checker := range checkers {
		registry.CheckPlugins[checker.ID()] = checker
	}

	notifiers := []plugin.Notifier{&impl.Mail{}, &impl.SlackThread{}, &impl.SlackWebhook{}}
	for _, notifier := range notifiers {
		registry.NotificationPlugins[notifier.ID()] = notifier
	}

	triggers := []plugin.Trigger{
		&impl.AlterAnnotation{},
		&impl.AlterFinalizer{},
		&impl.AlterHypervisor{},
		&impl.AlterLabel{},
		&impl.Eviction{},
	}
	for _, trigger := range triggers {
		registry.TriggerPlugins[trigger.ID()] = trigger
	}
}

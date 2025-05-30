// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PaesslerAG/gval"
	"github.com/elastic/go-ucfg"
	"github.com/go-logr/logr"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AndSeparator is a string that is used to combine two plugin instance within a config string.
const AndSeparator = "&&"

// OrSeparator is a string that is used to combine two plugin instance within a config string.
const OrSeparator = "||"

// ChainError wraps an error that causes a PluginChain to fail.
type ChainError struct {
	Message string
	Err     error
}

func (e *ChainError) Error() string {
	return fmt.Sprintf("%v: %v", e.Message, e.Err)
}

func (e *ChainError) Unwrap() error {
	return e.Err
}

// Specifies the configuration for a Scheduler.
type ScheduleDescriptor struct {
	Type   string
	Config *ucfg.Config
}

// Specifies the configuration for instances.
type InstancesDescriptor struct {
	Check   []InstanceDescriptor
	Notify  []NotificationDescriptor
	Trigger []InstanceDescriptor
}

// Specifies the configuration for a single notification instance.
type NotificationDescriptor struct {
	Name     string
	Type     string
	Schedule ScheduleDescriptor `config:"schedule" validate:"required"`
	Config   *ucfg.Config
}

// Specifies the configuration for a single check/trigger instance.
type InstanceDescriptor struct {
	Name   string
	Type   string
	Config *ucfg.Config
}

// Parameters describes the parameters plugins get to work with.
type Parameters struct {
	// the current evaluated node
	Node *corev1.Node
	// the current state of the evaluated node
	State string
	// the profile that is currently evaluated
	Profile string
	// if any profile is in-maintenance on the evaluated node
	InMaintenance bool
	// whether to log failing checks, notifications, ...
	LogDetails     bool
	Client         client.Client
	Clientset      kubernetes.Interface
	Ctx            context.Context //nolint: containedctx
	Log            logr.Logger
	Recorder       record.EventRecorder
	LastTransition time.Time
}

// Registry is a central storage for all plugins and their instances.
type Registry struct {
	NotificationInstances map[string]NotificationInstance
	NotificationPlugins   map[string]Notifier
	CheckInstances        map[string]CheckInstance
	CheckPlugins          map[string]Checker
	TriggerInstances      map[string]TriggerInstance
	TriggerPlugins        map[string]Trigger
}

// NewRegistry creates a new registry with non-null maps.
func NewRegistry() Registry {
	registry := Registry{
		NotificationInstances: make(map[string]NotificationInstance),
		NotificationPlugins:   make(map[string]Notifier),
		CheckInstances:        make(map[string]CheckInstance),
		CheckPlugins:          make(map[string]Checker),
		TriggerInstances:      make(map[string]TriggerInstance),
		TriggerPlugins:        make(map[string]Trigger),
	}
	return registry
}

// NewCheckChain creates a CheckChain based the given config string.
func (r *Registry) NewCheckChain(config string) (CheckChain, error) {
	var chain CheckChain
	if config == "" {
		return chain, nil
	}
	stripped := stripSymbols(config, AndSeparator, OrSeparator, "(", ")", "!")
	for _, name := range strings.Split(stripped, " ") {
		// due to stripping multiple whitespace may pile up so that empty strings can be created while splitting
		if name == "" {
			continue
		}
		instance, ok := r.CheckInstances[strings.Trim(name, " ")]
		if !ok {
			return chain, fmt.Errorf("the requested check instance \"%v\" is not known to the registry", name)
		}
		chain.Plugins = append(chain.Plugins, instance)
	}
	eval, err := gval.Full().NewEvaluable(config)
	if err != nil {
		return chain, err
	}
	chain.Evaluable = eval
	chain.Expression = config
	return chain, nil
}

func stripSymbols(base string, symbols ...string) string {
	for _, symbol := range symbols {
		base = strings.ReplaceAll(base, symbol, "")
	}
	return base
}

// NewNotificationChain creates a NotificaitonChain based the given config string.
func (r *Registry) NewNotificationChain(config string) (NotificationChain, error) {
	var chain NotificationChain
	if config == "" {
		return chain, nil
	}
	for _, name := range strings.Split(config, AndSeparator) {
		instance, ok := r.NotificationInstances[strings.Trim(name, " ")]
		if !ok {
			return chain, fmt.Errorf("the requested notification instance \"%v\" is not known to the registry", name)
		}
		chain.Plugins = append(chain.Plugins, instance)
	}
	return chain, nil
}

// NewTriggerChain creates a TriggerChain based the given config string.
func (r *Registry) NewTriggerChain(config string) (TriggerChain, error) {
	var chain TriggerChain
	if config == "" {
		return chain, nil
	}
	for _, name := range strings.Split(config, AndSeparator) {
		instance, ok := r.TriggerInstances[strings.Trim(name, " ")]
		if !ok {
			return chain, fmt.Errorf("the requested trigger instance \"%v\" is not known to the registry", name)
		}
		chain.Plugins = append(chain.Plugins, instance)
	}
	return chain, nil
}

// LoadInstances parses the given config and constructs plugin instances accordingly.
// These instances are put into the respective instances map within the registry.
func (r *Registry) LoadInstances(config *ucfgwrap.Config, descriptor *InstancesDescriptor) error {
	for _, instance := range descriptor.Check {
		err := r.loadCheckInstance(config, instance)
		if err != nil {
			return err
		}
	}
	for _, instance := range descriptor.Notify {
		err := r.loadNotificationInstance(config, instance)
		if err != nil {
			return err
		}
	}
	for _, instance := range descriptor.Trigger {
		err := r.loadTriggerInstance(config, instance)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) loadCheckInstance(config *ucfgwrap.Config, descriptor InstanceDescriptor) error {
	baseChecker, ok := r.CheckPlugins[descriptor.Type]
	if !ok {
		return fmt.Errorf("the requested check plugin type \"%v\" is not known to the registry", descriptor.Type)
	}
	plugin, err := baseChecker.New(wrapConfig(config, descriptor))
	if err != nil {
		return err
	}
	r.CheckInstances[descriptor.Name] = CheckInstance{
		Name:   descriptor.Name,
		Plugin: plugin,
	}
	return nil
}

func (r *Registry) loadNotificationInstance(config *ucfgwrap.Config, descriptor NotificationDescriptor) error {
	instanceName := descriptor.Name
	basePlugin, ok := r.NotificationPlugins[descriptor.Type]
	if !ok {
		return fmt.Errorf("the requested notification plugin type \"%v\" is not known to the registry", descriptor.Type)
	}
	// don't warp nil configs
	var commonConf *ucfgwrap.Config
	if descriptor.Config != nil {
		localConf := config.Wrap(descriptor.Config)
		commonConf = &localConf
	} else {
		commonConf = nil
	}
	plugin, err := basePlugin.New(commonConf)
	if err != nil {
		return err
	}
	var schedule Scheduler
	if descriptor.Schedule.Config == nil {
		return errors.New("a notification instance does not have a schedule assigned")
	}
	scheduleConf := config.Wrap(descriptor.Schedule.Config)
	switch strings.ToLower(descriptor.Schedule.Type) {
	case "periodic":
		schedule, err = newNotifyPeriodic(&scheduleConf)
	case "scheduled":
		schedule, err = newNotifyScheduled(&scheduleConf)
	case "oneshot":
		schedule, err = newNotifyOneshot(&scheduleConf)
	default:
		return fmt.Errorf("notification scheduler with name %s is unknown", descriptor.Schedule.Type)
	}
	if err != nil {
		return err
	}
	r.NotificationInstances[instanceName] = NotificationInstance{
		Name:     instanceName,
		Plugin:   plugin,
		Schedule: schedule,
	}
	return nil
}

func (r *Registry) loadTriggerInstance(config *ucfgwrap.Config, descriptor InstanceDescriptor) error {
	baseTrigger, ok := r.TriggerPlugins[descriptor.Type]
	if !ok {
		return fmt.Errorf("the requested trigger plugin type \"%v\" is not known to the registry", descriptor.Type)
	}
	plugin, err := baseTrigger.New(wrapConfig(config, descriptor))
	if err != nil {
		return err
	}
	r.TriggerInstances[descriptor.Name] = TriggerInstance{
		Name:   descriptor.Name,
		Plugin: plugin,
	}
	return nil
}

func wrapConfig(original *ucfgwrap.Config, descriptor InstanceDescriptor) *ucfgwrap.Config {
	// don't warp nil configs
	if descriptor.Config == nil {
		return nil
	}
	localConf := original.Wrap(descriptor.Config)
	return &localConf
}

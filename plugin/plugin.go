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
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PaesslerAG/gval"
	"github.com/elastic/go-ucfg"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
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

type InstancesDescriptor struct {
	Check   []InstanceDescriptor
	Notify  []InstanceDescriptor
	Trigger []InstanceDescriptor
}

type InstanceDescriptor struct {
	Name   string
	Type   string
	Config *ucfg.Config
}

type ProfileInfo struct {
	Current string
	Last    string
}

// Parameters describes the parameters plugins get to work with.
type Parameters struct {
	Node           *corev1.Node
	State          string
	Profile        ProfileInfo
	Client         client.Client
	Ctx            context.Context
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
func (r *Registry) LoadInstances(config *InstancesDescriptor) error {
	for _, instance := range config.Check {
		err := r.loadCheckInstance(instance)
		if err != nil {
			return err
		}
	}
	for _, instance := range config.Notify {
		err := r.loadNotificationInstance(instance)
		if err != nil {
			return err
		}
	}
	for _, instance := range config.Trigger {
		err := r.loadTriggerInstance(instance)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) loadCheckInstance(config InstanceDescriptor) error {
	instanceName := config.Name
	subConfig := config.Config
	basePlugin, ok := r.CheckPlugins[config.Type]
	if !ok {
		return fmt.Errorf("the requested plugin type \"%v\" is not known to the registry", config.Type)
	}
	plugin, err := basePlugin.New(subConfig)
	if err != nil {
		return err
	}
	r.CheckInstances[instanceName] = CheckInstance{
		Name:   instanceName,
		Plugin: plugin,
	}
	return nil
}

func (r *Registry) loadNotificationInstance(config InstanceDescriptor) error {
	instanceName := config.Name
	subConfig := config.Config
	basePlugin, ok := r.NotificationPlugins[config.Type]
	if !ok {
		return fmt.Errorf("the requested plugin type \"%v\" is not known to the registry", config.Type)
	}
	plugin, err := basePlugin.New(subConfig)
	if err != nil {
		return err
	}
	r.NotificationInstances[instanceName] = NotificationInstance{
		Name:   instanceName,
		Plugin: plugin,
	}
	return nil
}

func (r *Registry) loadTriggerInstance(config InstanceDescriptor) error {
	instanceName := config.Name
	subConfig := config.Config
	basePlugin, ok := r.TriggerPlugins[config.Type]
	if !ok {
		return fmt.Errorf("the requested plugin type \"%v\" is not known to the registry", config.Type)
	}
	plugin, err := basePlugin.New(subConfig)
	if err != nil {
		return err
	}
	r.TriggerInstances[instanceName] = TriggerInstance{
		Name:   instanceName,
		Plugin: plugin,
	}
	return nil
}

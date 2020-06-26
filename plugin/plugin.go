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
	"fmt"
	"strings"

	"github.com/elastic/go-ucfg"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AndSeparator is a string that is used to combine two plugin instance within a config string.
const AndSeparator = "&"

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

// Parameters desecribes the parameters plugins get to work with.
type Parameters struct {
	Node   corev1.Node
	Client client.Client
	Log    logr.Logger
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

// NewRegistry creates a new regsitry and appends all kown plugins to the according plugin maps.
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
	for _, name := range strings.Split(config, AndSeparator) {
		instance, ok := r.CheckInstances[name]
		if !ok {
			return chain, fmt.Errorf("the requested check instance \"%v\" is not known to the registry", name)
		}
		chain.Plugins = append(chain.Plugins, instance)
	}
	return chain, nil
}

// NewNotificationChain creates a NotificaitonChain based the given config string.
func (r *Registry) NewNotificationChain(config string) (NotificationChain, error) {
	var chain NotificationChain
	for _, name := range strings.Split(config, AndSeparator) {
		instance, ok := r.NotificationInstances[name]
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
	for _, name := range strings.Split(config, AndSeparator) {
		instance, ok := r.TriggerInstances[name]
		if !ok {
			return chain, fmt.Errorf("the requested trigger instance \"%v\" is not known to the registry", name)
		}
		chain.Plugins = append(chain.Plugins, instance)
	}
	return chain, nil
}

// LoadInstances parses the given config and constructs plugin instances accordingly.
// These instacnes are put into the respective instances map within the registry.
func (r *Registry) LoadInstances(config *ucfg.Config) error {
	err := r.loadCheckInstances(config)
	if err != nil {
		return err
	}
	err = r.loadNotificationInstances(config)
	if err != nil {
		return err
	}
	err = r.loadTriggerInstances(config)
	if err != nil {
		return err
	}
	return nil
}

func (r *Registry) loadCheckInstances(config *ucfg.Config) error {
	checkID := "check"
	checkCount, err := config.CountField(checkID)
	// no checks configured, return without error is valid
	if err != nil {
		return nil
	}
	for i := 0; i < checkCount; i++ {
		oneCheck, err := config.Child(checkID, i)
		if err != nil {
			return err
		}
		fieldNames := oneCheck.GetFields()
		if len(fieldNames) != 1 {
			return fmt.Errorf("check list entry has %v values. Expected a single value", len(fieldNames))
		}
		pluginType := fieldNames[0]
		pluginConfig, err := oneCheck.Child(pluginType, -1)
		if err != nil {
			return err
		}
		err = r.loadCheckInstance(pluginConfig, pluginType)
	}
	return nil
}

func (r *Registry) loadCheckInstance(config *ucfg.Config, pluginType string) error {
	instanceName, err := config.String("name", -1)
	if err != nil {
		return err
	}
	subConfig, err := config.Child("config", -1)
	if err != nil {
		return err
	}
	basePlugin, ok := r.CheckPlugins[pluginType]
	if !ok {
		return fmt.Errorf("the requested plugin type \"%v\" is not known to the registry", pluginType)
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

func (r *Registry) loadNotificationInstances(config *ucfg.Config) error {
	notificationID := "notify"
	notificationCount, err := config.CountField(notificationID)
	// no notifications configured, return without error is valid
	if err != nil {
		return nil
	}
	for i := 0; i < notificationCount; i++ {
		oneCheck, err := config.Child(notificationID, i)
		if err != nil {
			return err
		}
		fieldNames := oneCheck.GetFields()
		if len(fieldNames) != 1 {
			return fmt.Errorf("notification list entry has %v values. Expected a single value", len(fieldNames))
		}
		pluginType := fieldNames[0]
		pluginConfig, err := oneCheck.Child(pluginType, -1)
		if err != nil {
			return err
		}
		err = r.loadNotificationInstance(pluginConfig, pluginType)
	}
	return nil
}

func (r *Registry) loadNotificationInstance(config *ucfg.Config, pluginType string) error {
	instanceName, err := config.String("name", -1)
	if err != nil {
		return err
	}
	subConfig, err := config.Child("config", -1)
	if err != nil {
		return err
	}
	basePlugin, ok := r.NotificationPlugins[pluginType]
	if !ok {
		return fmt.Errorf("the requested plugin type \"%v\" is not known to the registry", pluginType)
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

func (r *Registry) loadTriggerInstances(config *ucfg.Config) error {
	triggerID := "trigger"
	triggerCount, err := config.CountField(triggerID)
	// no triggers configured, return without error is valid
	if err != nil {
		return nil
	}
	for i := 0; i < triggerCount; i++ {
		oneCheck, err := config.Child(triggerID, i)
		if err != nil {
			return err
		}
		fieldNames := oneCheck.GetFields()
		if len(fieldNames) != 1 {
			return fmt.Errorf("trigger list entry has %v values. Expected a single value", len(fieldNames))
		}
		pluginType := fieldNames[0]
		pluginConfig, err := oneCheck.Child(pluginType, -1)
		if err != nil {
			return err
		}
		err = r.loadTriggerInstance(pluginConfig, pluginType)
	}
	return nil
}

func (r *Registry) loadTriggerInstance(config *ucfg.Config, pluginType string) error {
	instanceName, err := config.String("name", -1)
	if err != nil {
		return err
	}
	subConfig, err := config.Child("config", -1)
	if err != nil {
		return err
	}
	basePlugin, ok := r.TriggerPlugins[pluginType]
	if !ok {
		return fmt.Errorf("the requested plugin type \"%v\" is not known to the registry", pluginType)
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

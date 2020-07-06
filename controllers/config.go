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
)

// Config represents the controllers global configuration.
type Config struct {
	// RequeueInterval defines a duration after the a node is reconceiled again by the controller
	RequeueInterval time.Duration
	// NotificationInterval specifies a duration after which notifications are resend
	NotificationInterval time.Duration
	// StateKey is the full label key, which the controller attaches the node state information to
	StateKey string
	// AnnotationBaseKey serves as a base annotation keys, which the controllers uses to parse node plugin chains
	AnnotationBaseKey string
	// Registry is the global plugin Registry
	Registry plugin.Registry
}

// LoadConfig (re-)initializes the config with values provided by the given ucfg.Config.
func LoadConfig(config *ucfg.Config) (*Config, error) {
	c := &Config{}
	intervals, err := config.Child("intervals", -1)
	if err != nil {
		return nil, err
	}
	requeueStr, err := intervals.String("requeue", -1)
	if err != nil {
		return nil, err
	}
	c.RequeueInterval, err = time.ParseDuration(requeueStr)
	if err != nil {
		return nil, err
	}
	notificationStr, err := intervals.String("notify", -1)
	if err != nil {
		return nil, err
	}
	c.NotificationInterval, err = time.ParseDuration(notificationStr)
	if err != nil {
		return nil, err
	}

	keys, err := config.Child("keys", -1)
	if err != nil {
		return nil, err
	}
	c.StateKey, err = keys.String("state", -1)
	if err != nil {
		return nil, err
	}
	c.AnnotationBaseKey, err = keys.String("chain", -1)
	if err != nil {
		return nil, err
	}

	instances, err := config.Child("instances", -1)
	if err != nil {
		return nil, err
	}
	c.Registry = plugin.NewRegistry()
	addPluginsToRegistry(&c.Registry)
	err = c.Registry.LoadInstances(instances)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// addPluginsToRegistry adds known plugins to the registry.
func addPluginsToRegistry(registry *plugin.Registry) {
	registry.CheckPlugins["hasAnnotation"] = &impl.HasAnnotation{}
	registry.CheckPlugins["hasLabel"] = &impl.HasLabel{}
	registry.CheckPlugins["timeWindow"] = &impl.TimeWindow{}

	registry.NotificationPlugins["slack"] = &impl.Slack{}
	registry.NotificationPlugins["mail"] = &impl.Mail{}

	registry.TriggerPlugins["alterAnnotation"] = &impl.AlterAnnotation{}
	registry.TriggerPlugins["alterLabel"] = &impl.AlterLabel{}
}

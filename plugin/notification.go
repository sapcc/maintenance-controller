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

	"github.com/elastic/go-ucfg"
)

// Notifier is the interface that notification plugins need to implement.
// It is recommend to make notification plugins idempotent, as the same message might be send multiple times.
// A zero-initialized notification plugin should not actually work as it is used to create
// the actual usable configured instances.
type Notifier interface {
	Notify(params Parameters) error
	New(config *ucfg.Config) (Notifier, error)
}

// NotificationInstance represents a configured and named instance of a notification plugin.
type NotificationInstance struct {
	Plugin Notifier
	Name   string
}

// NotificationChain represents a collection of multiple NotificationInstance that can be executed one after another.
type NotificationChain struct {
	Plugins []NotificationInstance
}

// Execute invokes Notify on each NotificationInstance in the chain and aborts when a plugin returns an error.
func (chain *NotificationChain) Execute(params Parameters) error {
	for _, notifier := range chain.Plugins {
		err := notifier.Plugin.Notify(params)
		if err != nil {
			return &ChainError{
				Message: fmt.Sprintf("Notification instance %v failed", notifier.Name),
				Err:     err,
			}
		}
	}
	return nil
}

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

package impl

import (
	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MaxMaintenance is a check plugin that checks whether the amount
// of nodes with the in-maintenance state does not exceed the specified amount.
type MaxMaintenance struct {
	MaxNodes int
}

// New creates a new MaxMaintenance instance with the given config.
func (m *MaxMaintenance) New(config *ucfg.Config) (plugin.Checker, error) {
	conf := struct {
		Max int `config:"max" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &MaxMaintenance{MaxNodes: conf.Max}, nil
}

// Check asserts that no more then the specified amount of nodes is in the in-maintenance state.
func (m *MaxMaintenance) Check(params plugin.Parameters) (bool, error) {
	var nodeList corev1.NodeList
	err := params.Client.List(params.Ctx, &nodeList, client.MatchingLabels{params.StateKey: string(state.InMaintenance)})
	if err != nil {
		return false, err
	}
	return m.checkInternal(&nodeList)
}

func (m *MaxMaintenance) checkInternal(nodes *corev1.NodeList) (bool, error) {
	if len(nodes.Items) >= m.MaxNodes {
		return false, nil
	}
	return true, nil
}

func (m *MaxMaintenance) AfterEval(chainResult bool, params plugin.Parameters) error {
	return nil
}

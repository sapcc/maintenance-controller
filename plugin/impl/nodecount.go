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
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"

	"github.com/sapcc/maintenance-controller/plugin"
)

type NodeCount struct {
	Count int
}

// New creates a new Count instance with the given config.
func (n *NodeCount) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Count int `config:"count" validate:"required"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &NodeCount{Count: conf.Count}, nil
}

func (n *NodeCount) ID() string {
	return "nodeCount"
}

// Check asserts that the cluster has at least the configured amount of nodes.
func (n *NodeCount) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	nodeList := &v1.NodeList{}
	err := params.Client.List(params.Ctx, nodeList)
	if err != nil {
		return plugin.Failed(nil), err
	}
	current := len(nodeList.Items)
	info := map[string]any{"current": current, "expected": n.Count}
	return plugin.CheckResult{Passed: current >= n.Count, Info: info}, nil
}

func (n *NodeCount) OnTransition(params plugin.Parameters) error {
	return nil
}

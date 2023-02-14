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

package cache

import (
	"encoding/json"
	"sync"

	"github.com/sapcc/maintenance-controller/state"
	"golang.org/x/exp/maps"
)

type NodeInfoCache interface {
	Update(state.NodeInfo)
	JSON() ([]byte, error)
}

func NewNodeInfoCache() NodeInfoCache {
	return &nodeInfoCacheImpl{
		mutex: sync.Mutex{},
		nodes: make(map[string]state.NodeInfo),
	}
}

type nodeInfoCacheImpl struct {
	mutex sync.Mutex
	nodes map[string]state.NodeInfo
}

func (nic *nodeInfoCacheImpl) Update(info state.NodeInfo) {
	nic.mutex.Lock()
	defer nic.mutex.Unlock()
	nic.nodes[info.Node] = info
}

func (nic *nodeInfoCacheImpl) JSON() ([]byte, error) {
	// do not hand out data of the nodes map as it contains
	// pointers, which in turn open up for data races
	nic.mutex.Lock()
	defer nic.mutex.Unlock()
	return json.Marshal(maps.Values(nic.nodes))
}

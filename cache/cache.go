// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/sapcc/maintenance-controller/state"
)

type NodeInfoCache interface {
	Update(state.NodeInfo)
	Delete(string)
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
	info.Updated = time.Now()
	nic.nodes[info.Node] = info
}

func (nic *nodeInfoCacheImpl) Delete(node string) {
	nic.mutex.Lock()
	defer nic.mutex.Unlock()
	delete(nic.nodes, node)
}

func (nic *nodeInfoCacheImpl) JSON() ([]byte, error) {
	// do not hand out data of the nodes map as it contains
	// pointers, which in turn open up for data races
	nic.mutex.Lock()
	defer nic.mutex.Unlock()
	return json.Marshal(slices.Collect(maps.Values(nic.nodes)))
}

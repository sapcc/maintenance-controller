// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/sapcc/maintenance-controller/state"
)

func TestCache(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cache Suite")
}

var _ = Describe("NodeInfoCache", func() {

	It("caches NodeInfos", func() {
		cache := NewNodeInfoCache()
		cache.Update(state.NodeInfo{Node: "a"})
		cache.Update(state.NodeInfo{Node: "a"})
		cache.Update(state.NodeInfo{Node: "b"})
		jsonStr, err := cache.JSON()
		Expect(err).To(Succeed())
		result := make([]state.NodeInfo, 0)
		Expect(json.Unmarshal(jsonStr, &result)).To(Succeed())
		Expect(result).To(HaveLen(2))
	})

})

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

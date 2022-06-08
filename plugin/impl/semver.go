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
	"fmt"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/elastic/go-ucfg"
	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Collects all values of the given Label key
// and passes if the current nodes value is less
// than the clusters max value, which indicates
// that an update may be needed.
type ClusterSemver struct {
	Key           string
	ProfileScoped bool
}

func (cs *ClusterSemver) New(config *ucfg.Config) (plugin.Checker, error) {
	conf := struct {
		Key           string `config:"key" validate:"required"`
		ProfileScoped bool   `config:"profileScoped"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &ClusterSemver{Key: conf.Key, ProfileScoped: conf.ProfileScoped}, nil
}

func (cs *ClusterSemver) Check(params plugin.Parameters) (bool, error) {
	versionStr, ok := params.Node.Labels[cs.Key]
	if !ok {
		return false, fmt.Errorf("node does not have label %s containing version", cs.Key)
	}
	ownVersion, err := semver.Parse(versionStr)
	if err != nil {
		return false, fmt.Errorf("failed to parse current nodes version label: %w", err)
	}
	var nodeList v1.NodeList
	err = params.Client.List(params.Ctx, &nodeList, client.HasLabels{cs.Key})
	if err != nil {
		return false, err
	}
	nodes := nodeList.Items
	if cs.ProfileScoped {
		nodes = filterByProfile(nodes, params.Profile)
	}
	maxVersion := semver.MustParse("0.1.0")
	for _, node := range nodes {
		versionStr, ok := node.Labels[cs.Key]
		if !ok {
			return false, fmt.Errorf("node labels do not contain %s although filtered on it", cs.Key)
		}
		version, err := semver.Parse(versionStr)
		if err != nil {
			continue
		}
		if version.GT(maxVersion) {
			maxVersion = version
		}
	}
	return ownVersion.LT(maxVersion), nil
}

func filterByProfile(nodes []v1.Node, profile string) []v1.Node {
	filtered := make([]v1.Node, 0)
	for _, node := range nodes {
		nodeProfiles, ok := node.Labels[constants.ProfileLabelKey]
		if !ok {
			continue
		}
		if strings.Contains(nodeProfiles, profile) {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

func (cs *ClusterSemver) AfterEval(chainResult bool, params plugin.Parameters) error {
	return nil
}

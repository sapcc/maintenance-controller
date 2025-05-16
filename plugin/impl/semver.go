// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"fmt"

	"github.com/blang/semver/v4"
	"github.com/sapcc/ucfgwrap"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
)

// Collects all values of the given Label key
// and passes if the current nodes value is less
// than the clusters max value, which indicates
// that an update may be needed.
type ClusterSemver struct {
	Key           string
	ProfileScoped bool
}

func (cs *ClusterSemver) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Key           string `config:"key" validate:"required"`
		ProfileScoped bool   `config:"profileScoped"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &ClusterSemver{Key: conf.Key, ProfileScoped: conf.ProfileScoped}, nil
}

func (cs *ClusterSemver) ID() string {
	return "clusterSemver"
}

func (cs *ClusterSemver) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	versionStr, ok := params.Node.Labels[cs.Key]
	if !ok {
		return plugin.Failed(nil), fmt.Errorf("node does not have label %s containing version", cs.Key)
	}
	ownVersion, err := semver.Parse(versionStr)
	if err != nil {
		return plugin.Failed(nil), fmt.Errorf("failed to parse current nodes version label: %w", err)
	}
	var nodeList v1.NodeList
	err = params.Client.List(params.Ctx, &nodeList, client.HasLabels{cs.Key})
	if err != nil {
		return plugin.Failed(nil), err
	}
	nodes := nodeList.Items
	if cs.ProfileScoped {
		nodes = filterByProfile(nodes, params.Profile)
	}
	maxVersion := semver.MustParse("0.1.0")
	for _, node := range nodes {
		versionStr, ok := node.Labels[cs.Key]
		if !ok {
			return plugin.Failed(nil), fmt.Errorf("node labels do not contain %s although filtered on it", cs.Key)
		}
		version, err := semver.Parse(versionStr)
		if err != nil {
			continue
		}
		if version.GT(maxVersion) {
			maxVersion = version
		}
	}
	info := map[string]any{"own": ownVersion.String(), "max": maxVersion.String()}
	return plugin.CheckResult{Passed: ownVersion.LT(maxVersion), Info: info}, nil
}

func filterByProfile(nodes []v1.Node, profile string) []v1.Node {
	filtered := make([]v1.Node, 0)
	for _, node := range nodes {
		nodeProfiles, ok := node.Labels[constants.ProfileLabelKey]
		if !ok {
			continue
		}
		if state.ContainsProfile(nodeProfiles, profile) {
			filtered = append(filtered, node)
		}
	}
	return filtered
}

func (cs *ClusterSemver) OnTransition(params plugin.Parameters) error {
	return nil
}

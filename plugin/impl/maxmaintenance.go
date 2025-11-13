// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/maintenance-controller/constants"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
)

// MaxMaintenance is a check plugin that checks whether the amount
// of nodes with the in-maintenance state does not exceed the specified amount.
type MaxMaintenance struct {
	MaxNodes  int
	Profile   string
	GroupBy   []string
	SkipAfter time.Duration
}

// New creates a new MaxMaintenance instance with the given config.
func (m *MaxMaintenance) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		Max        int           `config:"max" validate:"required"`
		Profile    string        `config:"profile"`
		GroupLabel string        `config:"groupLabel"` // deprecated
		GroupBy    []string      `config:"groupBy"`
		SkipAfter  time.Duration `config:"skipAfter"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	groupBy := conf.GroupBy
	if groupBy == nil {
		groupBy = make([]string, 0)
	}
	if conf.GroupLabel != "" && !slices.Contains(groupBy, conf.GroupLabel) {
		groupBy = append(groupBy, conf.GroupLabel)
	}
	return &MaxMaintenance{
		MaxNodes:  conf.Max,
		Profile:   conf.Profile,
		GroupBy:   groupBy,
		SkipAfter: conf.SkipAfter,
	}, nil
}

func (m *MaxMaintenance) ID() string {
	return "maxMaintenance"
}

// Check asserts that no more then the specified amount of nodes is in the in-maintenance state.
func (m *MaxMaintenance) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	var nodeList corev1.NodeList
	err := params.Client.List(params.Ctx, &nodeList, client.MatchingLabels{
		constants.StateLabelKey: string(state.InMaintenance),
	})
	if err != nil {
		return plugin.Failed(nil), err
	}
	return m.checkInternal(params, nodeList.Items)
}

func (m *MaxMaintenance) checkInternal(params plugin.Parameters, nodes []corev1.Node) (plugin.CheckResult, error) {
	// profile == "" && skipAfter == nil => count all in-maintenance
	// profile == "abc" && skipAfter == nil => count all profiles containing "abc"
	// profile == "" && skipAfter != nil => count all which most recent transition does not exceed skipAfter
	// profile == "abc" && skipAfter != nil => count all where the transition of "abc" does not exceed skipAfter
	if m.Profile != "" {
		nodes = m.filterProfileName(nodes)
	}
	info := map[string]any{"scope": "all nodes in the cluster"}
	consideredLabels := make([]string, 0)
	for _, groupLabel := range m.GroupBy {
		if groupValue, ok := params.Node.Labels[groupLabel]; ok {
			nodes = m.filterGroupLabel(nodes, groupLabel, groupValue)
			consideredLabels = append(consideredLabels, fmt.Sprintf("%s=%s", groupLabel, groupValue))
		}
	}
	if len(consideredLabels) > 0 {
		info["scope"] = fmt.Sprintf("nodes matching the %s label selector", strings.Join(consideredLabels, ","))
	}
	if int64(m.SkipAfter) != 0 {
		filtered, err := m.filterRecentTransition(nodes, params.Log)
		if err != nil {
			return plugin.Failed(nil), err
		}
		nodes = filtered
	}
	info["maintained"] = len(nodes)
	info["max"] = m.MaxNodes
	if len(nodes) >= m.MaxNodes {
		return plugin.Failed(info), nil
	}
	return plugin.Passed(info), nil
}

func (m *MaxMaintenance) filterProfileName(nodes []corev1.Node) []corev1.Node {
	if m.Profile == "" {
		return nodes
	}
	matching := make([]corev1.Node, 0)
	for _, node := range nodes {
		profiles, ok := node.Labels[constants.ProfileLabelKey]
		if ok && state.ContainsProfile(profiles, m.Profile) {
			matching = append(matching, node)
		}
	}
	return matching
}

func (m *MaxMaintenance) filterRecentTransition(nodes []corev1.Node, log logr.Logger) ([]corev1.Node, error) {
	matching := make([]corev1.Node, 0)
	for i := range nodes {
		node := nodes[i]
		dataStr := node.Annotations[constants.DataAnnotationKey]
		stateData, err := state.ParseMigrateData(dataStr, log)
		if err != nil {
			return nil, err
		}
		dataMap := stateData.Profiles
		if m.Profile != "" {
			dataMap = map[string]*state.ProfileData{m.Profile: dataMap[m.Profile]}
		}
		var mostRecent time.Time
		for _, data := range dataMap {
			if data.Transition.After(mostRecent) {
				mostRecent = data.Transition
			}
		}
		if time.Since(mostRecent) < m.SkipAfter {
			matching = append(matching, node)
		}
	}
	return matching, nil
}

func (m *MaxMaintenance) filterGroupLabel(nodes []corev1.Node, label, value string) []corev1.Node {
	if label == "" || value == "" {
		return nodes
	}
	matching := make([]corev1.Node, 0)
	for _, node := range nodes {
		if val, ok := node.Labels[label]; ok {
			if value == val {
				matching = append(matching, node)
			}
		}
	}
	return matching
}

func (m *MaxMaintenance) OnTransition(params plugin.Parameters) error {
	return nil
}

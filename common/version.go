// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package common

import (
	"fmt"

	"github.com/blang/semver/v4"
	"k8s.io/client-go/kubernetes"
)

func GetAPIServerVersion(ki kubernetes.Interface) (semver.Version, error) {
	rsp, err := ki.Discovery().ServerVersion()
	if err != nil {
		return semver.Version{}, fmt.Errorf("failed to do request for API Server version: %w", err)
	}
	gitVersion := rsp.GitVersion[1:]
	version, err := semver.Parse(gitVersion)
	if err != nil {
		return semver.Version{}, fmt.Errorf("API Server version %s is not semver compatible: %w", gitVersion, err)
	}
	return version, nil
}

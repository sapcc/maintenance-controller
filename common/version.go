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

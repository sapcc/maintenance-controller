// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPluginImplementations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Plugin Implementation Suite")
}

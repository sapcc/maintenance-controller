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

package plugin

import (
	"errors"

	"github.com/PaesslerAG/gval"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
)

type trueCheck struct {
	Invoked int
}

func (c *trueCheck) Check(params Parameters) (CheckResult, error) {
	c.Invoked++
	return Passed(nil), nil
}

func (c *trueCheck) New(config *ucfgwrap.Config) (Checker, error) {
	return &trueCheck{}, nil
}

func (c *trueCheck) OnTransition(params Parameters) error {
	return nil
}

type falseCheck struct {
	Invoked int
}

func (c *falseCheck) Check(params Parameters) (CheckResult, error) {
	c.Invoked++
	return Failed(nil), nil
}

func (c *falseCheck) New(config *ucfgwrap.Config) (Checker, error) {
	return &falseCheck{}, nil
}

func (c *falseCheck) OnTransition(params Parameters) error {
	return nil
}

type errorCheck struct {
	Invoked int
}

func (c *errorCheck) Check(params Parameters) (CheckResult, error) {
	c.Invoked++
	return Failed(nil), errors.New("this check is expected to fail")
}

func (c *errorCheck) New(config *ucfgwrap.Config) (Checker, error) {
	return &errorCheck{}, nil
}

func (c *errorCheck) OnTransition(params Parameters) error {
	return nil
}

var _ = Describe("CheckChain", func() {

	var emptyParams Parameters

	Context("is empty", func() {

		var chain CheckChain
		It("should not error", func() {
			result, err := chain.Execute(emptyParams)
			Expect(result).To(BeTrue())
			Expect(err).To(Succeed())
		})

	})

	Context("contains plugins", func() {

		var (
			trueInstance  CheckInstance
			falseInstance CheckInstance
			errorInstance CheckInstance
		)

		BeforeEach(func() {
			trueInstance = CheckInstance{
				Plugin: &trueCheck{},
				Name:   "True",
			}
			falseInstance = CheckInstance{
				Plugin: &falseCheck{},
				Name:   "False",
			}
			errorInstance = CheckInstance{
				Plugin: &errorCheck{},
				Name:   "Error",
			}
		})

		It("should return true if all plugins of an and chain pass", func() {
			eval, err := gval.Full().NewEvaluable("True && True && True")
			Expect(err).To(Succeed())

			chain := CheckChain{
				Plugins:   []CheckInstance{trueInstance, trueInstance, trueInstance},
				Evaluable: eval,
			}
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
			Expect(trueInstance.Plugin.(*trueCheck).Invoked).To(Equal(3))
		})

		It("should return false if at least one check of an and chain does not pass", func() {
			eval, err := gval.Full().NewEvaluable("True && False && True && False")
			Expect(err).To(Succeed())

			chain := CheckChain{
				Plugins:   []CheckInstance{trueInstance, falseInstance, trueInstance, falseInstance},
				Evaluable: eval,
			}
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result).To(BeFalse())
			Expect(trueInstance.Plugin.(*trueCheck).Invoked).To(Equal(2))
			Expect(falseInstance.Plugin.(*falseCheck).Invoked).To(Equal(2))
		})

		It("should return true if all plugins of an or chain pass", func() {
			eval, err := gval.Full().NewEvaluable("True || True || True")
			Expect(err).To(Succeed())

			chain := CheckChain{
				Plugins:   []CheckInstance{trueInstance, trueInstance, trueInstance},
				Evaluable: eval,
			}
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
			Expect(trueInstance.Plugin.(*trueCheck).Invoked).To(Equal(3))
		})

		It("should return true if one plugin of an or chain does not pass", func() {
			eval, err := gval.Full().NewEvaluable("True || False || True")
			Expect(err).To(Succeed())

			chain := CheckChain{
				Plugins:   []CheckInstance{trueInstance, falseInstance, trueInstance},
				Evaluable: eval,
			}
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result).To(BeTrue())
			Expect(trueInstance.Plugin.(*trueCheck).Invoked).To(Equal(2))
		})

		It("should return false if a passing plugin is negated", func() {
			eval, err := gval.Full().NewEvaluable("!True")
			Expect(err).To(Succeed())

			chain := CheckChain{
				Plugins:   []CheckInstance{trueInstance},
				Evaluable: eval,
			}
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result).To(BeFalse())
			Expect(trueInstance.Plugin.(*trueCheck).Invoked).To(Equal(1))
		})

		It("should propagate errors", func() {
			chain := CheckChain{
				Plugins: []CheckInstance{trueInstance, errorInstance, trueInstance, trueInstance},
			}
			_, err := chain.Execute(emptyParams)
			Expect(err).To(HaveOccurred())
			Expect(trueInstance.Plugin.(*trueCheck).Invoked).To(Equal(1))
			Expect(errorInstance.Plugin.(*errorCheck).Invoked).To(Equal(1))
		})

	})
})

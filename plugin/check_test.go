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

const invokedKey = "invoked"

type trueCheck struct {
	Invoked int
}

func (c *trueCheck) Check(params Parameters) (CheckResult, error) {
	c.Invoked++
	return Passed(map[string]any{invokedKey: c.Invoked}), nil
}

func (c *trueCheck) New(config *ucfgwrap.Config) (Checker, error) {
	return &trueCheck{}, nil
}

func (c *trueCheck) OnTransition(params Parameters) error {
	return nil
}

func (c *trueCheck) ID() string {
	return "True"
}

type falseCheck struct {
	Invoked int
}

func (c *falseCheck) Check(params Parameters) (CheckResult, error) {
	c.Invoked++
	return Failed(map[string]any{invokedKey: c.Invoked}), nil
}

func (c *falseCheck) New(config *ucfgwrap.Config) (Checker, error) {
	return &falseCheck{}, nil
}

func (c *falseCheck) OnTransition(params Parameters) error {
	return nil
}

func (c *falseCheck) ID() string {
	return "False"
}

type errorCheck struct {
	Invoked int
}

func (c *errorCheck) Check(params Parameters) (CheckResult, error) {
	c.Invoked++
	return Failed(map[string]any{invokedKey: c.Invoked}), errors.New("this check is expected to fail")
}

func (c *errorCheck) New(config *ucfgwrap.Config) (Checker, error) {
	return &errorCheck{}, nil
}

func (c *errorCheck) OnTransition(params Parameters) error {
	return nil
}

func (c *errorCheck) ID() string {
	return "Error"
}

var _ = Describe("CheckChain", func() {
	var emptyParams Parameters

	Context("is empty", func() {
		var chain CheckChain
		It("should not error", func() {
			result, err := chain.Execute(emptyParams)
			Expect(result.Passed).To(BeTrue())
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

		makeChain := func(expr string, instances ...CheckInstance) CheckChain {
			eval, err := gval.Full().NewEvaluable(expr)
			ExpectWithOffset(1, err).To(Succeed())
			return CheckChain{
				Plugins:    instances,
				Evaluable:  eval,
				Expression: expr,
			}
		}

		It("should return true if all plugins of an and chain pass", func() {
			expr := "True && True && True"
			chain := makeChain(expr, trueInstance, trueInstance, trueInstance)
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
			Expect(result.Expression).To(Equal(expr))
			Expect(trueInstance.Plugin).To(Equal(&trueCheck{Invoked: 3}))
		})

		It("should return false if at least one check of an and chain does not pass", func() {
			expr := "True && False && True && False"
			chain := makeChain(expr, trueInstance, falseInstance, trueInstance, falseInstance)
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
			Expect(result.Expression).To(Equal(expr))
			Expect(trueInstance.Plugin).To(Equal(&trueCheck{Invoked: 2}))
			Expect(falseInstance.Plugin).To(Equal(&falseCheck{Invoked: 2}))
		})

		It("should return true if all plugins of an or chain pass", func() {
			expr := "True || True || True"
			chain := makeChain(expr, trueInstance, trueInstance, trueInstance)
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
			Expect(result.Expression).To(Equal(expr))
			Expect(trueInstance.Plugin).To(Equal(&trueCheck{Invoked: 3}))
		})

		It("should return true if one plugin of an or chain does not pass", func() {
			expr := "True || False || True"
			chain := makeChain(expr, trueInstance, falseInstance, trueInstance)
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
			Expect(result.Expression).To(Equal(expr))
			Expect(trueInstance.Plugin).To(Equal(&trueCheck{Invoked: 2}))
		})

		It("should return false if a passing plugin is negated", func() {
			expr := "!True"
			chain := makeChain(expr, trueInstance)
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
			Expect(result.Expression).To(Equal(expr))
			Expect(trueInstance.Plugin).To(Equal(&trueCheck{Invoked: 1}))
		})

		It("should propagate errors", func() {
			chain := CheckChain{
				Plugins: []CheckInstance{trueInstance, errorInstance, trueInstance, trueInstance},
			}
			result, err := chain.Execute(emptyParams)
			Expect(err).To(HaveOccurred())
			Expect(result.Passed).To(BeFalse())
			Expect(trueInstance.Plugin).To(Equal(&trueCheck{Invoked: 3}))
			Expect(errorInstance.Plugin).To(Equal(&errorCheck{Invoked: 1}))
		})

		It("should collect check infos", func() {
			expr := "True"
			chain := makeChain(expr, trueInstance)
			result, err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
			Expect(result.Expression).To(Equal(expr))
			Expect(result.Info["True"].Passed).To(BeTrue())
			Expect(result.Info["True"].Info[invokedKey]).To(Equal(1))
		})

		It("should collect error infos", func() {
			expr := "Error"
			chain := makeChain(expr, errorInstance)
			result, err := chain.Execute(emptyParams)
			Expect(err).To(HaveOccurred())
			Expect(result.Passed).To(BeFalse())
			Expect(result.Expression).To(Equal(expr))
			Expect(result.Info["Error"].Passed).To(BeFalse())
			Expect(result.Info["Error"].Info[invokedKey]).To(Equal(1))
			Expect(result.Info["Error"].Info).To(HaveKey("error"))
		})
	})
})

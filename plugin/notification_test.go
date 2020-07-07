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

	"github.com/elastic/go-ucfg"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

type successfulNotification struct {
	Invoked int
}

func (n *successfulNotification) Notify(params Parameters) error {
	n.Invoked++
	return nil
}

func (n *successfulNotification) New(config *ucfg.Config) (Notifier, error) {
	return &successfulNotification{}, nil
}

type failingNotification struct {
	Invoked int
}

func (n *failingNotification) Notify(params Parameters) error {
	n.Invoked++
	return errors.New("this notification is expected to fail")
}

func (n *failingNotification) New(config *ucfg.Config) (Notifier, error) {
	return &failingNotification{}, nil
}

var _ = Describe("NotificationChain", func() {

	var emptyParams Parameters

	Context("is empty", func() {

		var chain NotificationChain
		It("should not error", func() {
			err := chain.Execute(emptyParams)
			Expect(err).To(Succeed())
		})

	})

	Context("contains plugins", func() {

		var (
			success NotificationInstance
			failing NotificationInstance
		)

		BeforeEach(func() {
			success = NotificationInstance{
				Plugin: &successfulNotification{},
				Name:   "success",
			}
			failing = NotificationInstance{
				Plugin: &failingNotification{},
				Name:   "failing",
			}
		})

		It("should run all plugins", func() {
			chain := NotificationChain{
				Plugins: []NotificationInstance{success, success},
			}
			err := chain.Execute(emptyParams)
			Expect(err).ToNot(HaveOccurred())
			Expect(success.Plugin.(*successfulNotification).Invoked).To(Equal(2))
		})

		It("should propagate errors", func() {
			chain := NotificationChain{
				Plugins: []NotificationInstance{success, failing, success},
			}
			err := chain.Execute(emptyParams)
			Expect(err).To(HaveOccurred())
			Expect(success.Plugin.(*successfulNotification).Invoked).To(Equal(1))
			Expect(failing.Plugin.(*failingNotification).Invoked).To(Equal(1))
		})

	})

})

var _ = Describe("The notification", func() {

	It("should render its template", func() {
		result, err := RenderNotificationTemplate("{{.State}}", Parameters{State: "def"})
		Expect(err).To(Succeed())
		Expect(result).To(Equal("def"))
	})

})

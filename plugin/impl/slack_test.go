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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/ucfgwrap"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/maintenance-controller/state"
)

var _ = Describe("The slack webhook plugin", func() {

	It("should parse its config", func() {
		configStr := "hook: http://example.com\nchannel: thechannel\nmessage: msg"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base SlackWebhook
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*SlackWebhook).Hook).To(Equal("http://example.com"))
		Expect(plugin.(*SlackWebhook).Channel).To(Equal("thechannel"))
		Expect(plugin.(*SlackWebhook).Message).To(Equal("msg"))
	})

	It("should send a message", func() {
		// construct a http server, that accepts the slack request
		mux := http.NewServeMux()
		server := http.Server{
			Addr:              "localhost:25566",
			ReadTimeout:       60 * time.Second,
			ReadHeaderTimeout: 60 * time.Second,
			Handler:           mux,
		}
		requestChan := make(chan string, 1)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			requestBytes, err := io.ReadAll(r.Body)
			Expect(err).To(Succeed())
			requestChan <- string(requestBytes)
			_, err = w.Write([]byte("ok"))
			Expect(err).To(Succeed())
		})
		go func() {
			defer GinkgoRecover()
			err := server.ListenAndServe()
			Expect(err).To(HaveOccurred())
		}()
		defer server.Shutdown(context.Background()) //nolint:errcheck

		// wait for the server to come up
		time.Sleep(20 * time.Millisecond)
		// construct the slack plugin
		params := plugin.Parameters{
			Ctx: context.Background(),
			Node: &corev1.Node{
				ObjectMeta: v1.ObjectMeta{
					Name: "targetnode",
				},
			},
			State: string(state.Operational),
		}
		plugin := SlackWebhook{Hook: "http://localhost:25566/", Channel: "thechannel", Message: "abc"}
		err := plugin.Notify(params)
		Expect(err).To(Succeed())

		// fetch result from the channel
		resultStr := <-requestChan
		result := struct {
			Text    string
			Channel string
		}{}
		err = json.Unmarshal([]byte(resultStr), &result)
		Expect(err).To(Succeed())
		Expect(result.Channel).To(Equal("thechannel"))
		Expect(result.Text).ToNot(BeEmpty())
	})

})

var _ = Describe("The slack thread plugin", func() {
	It("should parse its config", func() {
		configStr := "token: token\n" +
			"channel: thechannel\n" +
			"title: title\n" +
			"message: msg\n" +
			"leaseName: lease\n" +
			"leaseNamespace: default\n" +
			"period: 1m\n"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base SlackThread
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*SlackThread).Token).To(Equal("token"))
		Expect(plugin.(*SlackThread).Channel).To(Equal("thechannel"))
		Expect(plugin.(*SlackThread).Message).To(Equal("msg"))
		Expect(plugin.(*SlackThread).Title).To(Equal("title"))
		Expect(plugin.(*SlackThread).LeaseName.Name).To(Equal("lease"))
		Expect(plugin.(*SlackThread).LeaseName.Namespace).To(Equal("default"))
		Expect(plugin.(*SlackThread).Period).To(Equal(time.Minute))
	})
})

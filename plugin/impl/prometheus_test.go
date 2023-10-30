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
	"net"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sapcc/maintenance-controller/plugin"
	"github.com/sapcc/ucfgwrap"
)

const promReply string = "{\"status\":\"success\",\"data\":{\"resultType\":\"vector\",\"result\":[{\"metric\":{\"__name__\":\"cool_metric\"},\"value\":[1680600891.782,\"1\"]}]}}" //nolint:lll

var _ = Describe("The prometheusInstant plugin", func() {

	It("can parse its configuration", func() {
		configStr := "url: http://abc.de\nquery: q\nexpr: value > 0"
		config, err := ucfgwrap.FromYAML([]byte(configStr))
		Expect(err).To(Succeed())
		var base PrometheusInstant
		plugin, err := base.New(&config)
		Expect(err).To(Succeed())
		Expect(plugin.(*PrometheusInstant).URL).To(Equal("http://abc.de"))
		Expect(plugin.(*PrometheusInstant).Query).To(Equal("q"))
	})

	Context("with a mock prometheus", Ordered, func() {

		var server http.Server
		const addr string = "127.0.0.1:29572"
		const url = "http://" + addr

		BeforeAll(func() {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/query", func(w http.ResponseWriter, r *http.Request) {
				defer GinkgoRecover()
				Expect(r.ParseForm()).To(Succeed())
				metric := r.Form.Get("query")
				GinkgoLogr.Info("query", "val", metric)
				if metric == "cool_metric" {
					_, err := w.Write([]byte(promReply))
					Expect(err).To(Succeed())
				} else {
					_, err := w.Write([]byte("{}"))
					Expect(err).To(Succeed())
				}
			})
			server = http.Server{
				Addr:           addr,
				Handler:        mux,
				MaxHeaderBytes: 1 << 20,
				// matches http.DefaultTransport keep-alive timeout
				IdleTimeout:       90 * time.Second,
				ReadHeaderTimeout: 32 * time.Second,
			}
			listenReady := make(chan struct{})
			go func() {
				defer GinkgoRecover()
				listener, err := net.Listen("tcp", addr)
				Expect(err).To(Succeed())
				listenReady <- struct{}{}
				close(listenReady)
				err = server.Serve(listener)
				Expect(err).To(MatchError(http.ErrServerClosed))
			}()
			<-listenReady
		})

		AfterAll(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()
			Expect(server.Shutdown(ctx)).To(Succeed())
		})

		It("passes if the expression is satisfied", func() {
			prom := PrometheusInstant{
				URL:   url,
				Query: "cool_metric",
				Expr:  "value == 1",
			}
			result, err := prom.Check(plugin.Parameters{Ctx: context.Background()})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeTrue())
		})

		It("fails if the expression is not satisfied", func() {
			prom := PrometheusInstant{
				URL:   url,
				Query: "cool_metric",
				Expr:  "value < 1",
			}
			result, err := prom.Check(plugin.Parameters{Ctx: context.Background()})
			Expect(err).To(Succeed())
			Expect(result.Passed).To(BeFalse())
		})

		It("fails if the server cannot be reached", func() {
			prom := PrometheusInstant{
				URL:   url + "/unreachable",
				Query: "cool_metric",
				Expr:  "value == 1",
			}
			result, err := prom.Check(plugin.Parameters{Ctx: context.Background()})
			Expect(err).To(HaveOccurred())
			Expect(result.Passed).To(BeFalse())
		})

		It("fails if the metric does not exist", func() {
			prom := PrometheusInstant{
				URL:   url,
				Query: "not_so_cool_metric",
				Expr:  "value == 1",
			}
			result, err := prom.Check(plugin.Parameters{Ctx: context.Background()})
			Expect(err).To(HaveOccurred())
			Expect(result.Passed).To(BeFalse())
		})

	})

})

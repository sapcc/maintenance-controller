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

// We run our on prometheus serving logic here, to "ensure" a last scrape in case
// the maintenance-controller drains itself. Otherwise shuffle metrics are inaccurate.

package metrics

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type PromServer struct {
	Address     string
	WaitTimeout time.Duration
	Log         logr.Logger
	counter     int
}

func (ps *PromServer) NeedLeaderElection() bool {
	return false
}

func (ps *PromServer) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", ps.Address)
	if err != nil {
		return err
	}
	handler := promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		ps.counter++
		handler.ServeHTTP(w, r)
	})
	// values copied over from controller-runtime
	server := &http.Server{
		Handler:        mux,
		MaxHeaderBytes: 1 << 20, //nolint:gomnd
		// matches http.DefaultTransport keep-alive timeout
		IdleTimeout:       90 * time.Second, //nolint:gomnd
		ReadHeaderTimeout: 32 * time.Second, //nolint:gomnd
	}
	go func() {
		_ = server.Serve(listener)
	}()
	<-ctx.Done()
	last := ps.counter
	ps.Log.Info("Awaiting an other metrics scrape", "timeout", ps.WaitTimeout)
	_ = wait.PollImmediate(1*time.Second, ps.WaitTimeout, func() (bool, error) {
		return ps.counter > last, nil
	})
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:gomnd
	err = server.Shutdown(timeout)
	cancel()
	return err
}

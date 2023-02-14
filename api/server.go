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

package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/maintenance-controller/cache"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type Server struct {
	Address       string
	WaitTimeout   time.Duration
	Log           logr.Logger
	NodeInfoCache cache.NodeInfoCache
	counter       int
	shutdown      chan struct{}
}

func (s *Server) NeedLeaderElection() bool {
	return false
}

// returns a channel that is closed, when the server properly terminates.
func (s *Server) Done() chan struct{} {
	return s.shutdown
}

func (s *Server) Start(ctx context.Context) error {
	s.shutdown = make(chan struct{})
	listener, err := net.Listen("tcp", s.Address)
	if err != nil {
		return err
	}
	handler := promhttp.HandlerFor(metrics.Registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		s.counter++
		handler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/v1/info", func(w http.ResponseWriter, r *http.Request) {
		jsonBytes, err := s.NodeInfoCache.JSON()
		if err != nil {
			jsonBytes = []byte(fmt.Sprintf("{\"error\":\"%s\"}", err.Error()))
		}
		_, err = w.Write(jsonBytes)
		if err != nil {
			s.Log.Error(err, "failed to write reply to /api/v1/info")
		}
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
	last := s.counter
	s.Log.Info("Awaiting an other metrics scrape", "timeout", s.WaitTimeout)
	_ = wait.PollImmediate(1*time.Second, s.WaitTimeout, func() (bool, error) {
		return s.counter > last, nil
	})
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:gomnd
	defer cancel()
	if err := server.Shutdown(timeout); err != nil {
		return err
	}
	close(s.shutdown)
	return nil
}

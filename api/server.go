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
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/maintenance-controller/cache"
	"github.com/sapcc/maintenance-controller/constants"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

type Server struct {
	Address       string
	WaitTimeout   time.Duration
	Log           logr.Logger
	NodeInfoCache cache.NodeInfoCache
	StaticPath    string
	Namespace     string
	Elected       <-chan struct{}
	Client        client.Client
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
		elected := false
		select {
		case _, ok := <-s.Elected:
			elected = !ok
			break
		default:
		}
		if elected {
			s.serveInfo(w)
		} else {
			s.fetchInfo(w)
		}
	})
	path := s.StaticPath
	if path == "" {
		path = "static"
	}
	static := http.FileServer(http.Dir(path))
	mux.Handle("/static/", http.StripPrefix("/static", static))
	mux.Handle("/", http.RedirectHandler("/static", http.StatusMovedPermanently))
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

func (s *Server) writeError(err error, w http.ResponseWriter) {
	jsonBytes := []byte(fmt.Sprintf("{\"error\":\"%s\"}", err.Error()))
	_, err = w.Write(jsonBytes)
	if err != nil {
		s.Log.Error(err, "failed to write error reply to /api/v1/info")
	}
}

func (s *Server) serveInfo(w http.ResponseWriter) {
	jsonBytes, err := s.NodeInfoCache.JSON()
	if err != nil {
		s.writeError(err, w)
		return
	}
	_, err = w.Write(jsonBytes)
	if err != nil {
		s.Log.Error(err, "failed to write reply to /api/v1/info")
	}
}

func (s *Server) fetchInfo(w http.ResponseWriter) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	namespace, err := s.getNamespace()
	if err != nil {
		s.writeError(err, w)
		return
	}
	var lease coordinationv1.Lease
	leaseName := types.NamespacedName{Namespace: namespace, Name: constants.LeaderElectionID}
	err = s.Client.Get(ctx, leaseName, &lease)
	if err != nil {
		s.writeError(err, w)
		return
	}
	if lease.Spec.HolderIdentity == nil {
		s.writeError(fmt.Errorf("no maintenance-controller is leading"), w)
		return
	}
	holder := *lease.Spec.HolderIdentity
	leader := strings.Split(holder, "_")[0]
	var pod corev1.Pod
	err = s.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: leader}, &pod)
	if err != nil {
		s.writeError(err, w)
		return
	}
	addr := net.JoinHostPort(pod.Status.PodIP, "8080")
	url := fmt.Sprintf("http://%s/api/v1/info", addr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		s.writeError(err, w)
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.writeError(err, w)
		return
	}
	_, err = io.Copy(w, res.Body)
	if err != nil {
		s.writeError(err, w)
		return
	}
	err = res.Body.Close()
	if err != nil {
		s.writeError(err, w)
		return
	}
}

// mostly copied from controller-runtime.
func (s *Server) getNamespace() (string, error) {
	if s.Namespace != "" {
		return s.Namespace, nil
	}
	path := "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	// Check whether the namespace file exists.
	// If not, we are not running in cluster so can't guess the namespace.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("namespace file does not exist")
	} else if err != nil {
		return "", fmt.Errorf("error checking namespace file: %w", err)
	}

	// Load the namespace file and return its content
	namespace, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("error reading namespace file: %w", err)
	}
	return string(namespace), nil
}

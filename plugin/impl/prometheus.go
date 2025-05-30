// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package impl

import (
	"errors"
	"fmt"
	"time"

	"github.com/PaesslerAG/gval"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/sapcc/ucfgwrap"

	"github.com/sapcc/maintenance-controller/plugin"
)

// HasLabel is a check plugin that queries a prometheus for the most recent
// value of a query, which is checked against a given expression.
type PrometheusInstant struct {
	URL   string
	Query string
	Expr  string
}

// New creates a new PrometheusInstant instance with the given config.
func (pi *PrometheusInstant) New(config *ucfgwrap.Config) (plugin.Checker, error) {
	conf := struct {
		URL   string `config:"url" validate:"required"`
		Query string `config:"query"`
		Expr  string `config:"expr"`
	}{}
	if err := config.Unpack(&conf); err != nil {
		return nil, err
	}
	return &PrometheusInstant{URL: conf.URL, Query: conf.Query, Expr: conf.Expr}, nil
}

func (pi *PrometheusInstant) ID() string {
	return "prometheusInstant"
}

// Queries the prometheus and evaluate the result against the given expression.
func (pi *PrometheusInstant) Check(params plugin.Parameters) (plugin.CheckResult, error) {
	info := map[string]any{"url": pi.URL, "query": pi.Query, "expr": pi.Expr}
	cfg := api.Config{
		Address: pi.URL,
	}
	promClient, err := api.NewClient(cfg)
	if err != nil {
		return plugin.Failed(info), fmt.Errorf("failed to create prometheus client for %s: %w", pi.URL, err)
	}
	promAPI := v1.NewAPI(promClient)
	result, warns, err := promAPI.Query(params.Ctx, pi.Query, time.Now())
	if err != nil {
		return plugin.Failed(info), fmt.Errorf("failed to query prometheus %s: %w", pi.URL, err)
	}
	if len(warns) > 0 {
		info["warns"] = warns
	}
	vector, ok := result.(model.Vector)
	if !ok {
		return plugin.Failed(info), errors.New("result from prometheus is not a vector")
	}
	if len(vector) != 1 {
		return plugin.Failed(info), errors.New("result does not contain exactly one element")
	}
	value := float64(vector[0].Value)
	info["value"] = value
	evaluable, err := gval.Full().NewEvaluable(pi.Expr)
	if err != nil {
		return plugin.Failed(info), err
	}
	passed, err := evaluable.EvalBool(params.Ctx, map[string]float64{"value": value})
	if err != nil {
		return plugin.Failed(info), fmt.Errorf("failed to evaluate prometheus expression: %w", err)
	}
	if !passed {
		return plugin.Failed(info), nil
	}
	return plugin.Passed(info), nil
}

func (pi *PrometheusInstant) OnTransition(params plugin.Parameters) error {
	return nil
}

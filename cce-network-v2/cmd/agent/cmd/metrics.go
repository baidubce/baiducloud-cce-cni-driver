/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package cmd

import (
	"fmt"
	"time"

	"github.com/go-openapi/runtime/middleware"
	"github.com/prometheus/client_golang/prometheus"

	restapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/spanstat"
)

type getMetrics struct {
	daemon *Daemon
}

// NewGetMetricsHandler returns the metrics handler
func NewGetMetricsHandler(d *Daemon) restapi.GetMetricsHandler {
	return &getMetrics{daemon: d}
}

func (h *getMetrics) Handle(params restapi.GetMetricsParams) middleware.Responder {
	metrics, err := metrics.DumpMetrics()
	if err != nil {
		return api.Error(
			restapi.GetMetricsInternalServerErrorCode,
			fmt.Errorf("Cannot gather metrics from daemon"))
	}

	return restapi.NewGetMetricsOK().WithPayload(metrics)
}

func initMetrics() <-chan error {
	var errs <-chan error

	if option.Config.PrometheusServeAddr != "" {
		log.Infof("Serving prometheus metrics on %s", option.Config.PrometheusServeAddr)
		errs = metrics.Enable(option.Config.PrometheusServeAddr)
	}

	return errs
}

type bootstrapStatistics struct {
	overall    spanstat.SpanStat
	earlyInit  spanstat.SpanStat
	k8sInit    spanstat.SpanStat
	restore    spanstat.SpanStat
	initAPI    spanstat.SpanStat
	initDaemon spanstat.SpanStat
	cleanup    spanstat.SpanStat
	ipam       spanstat.SpanStat
	daemonInit spanstat.SpanStat
	enim       spanstat.SpanStat
}

func (b *bootstrapStatistics) updateMetrics() {
	for scope, stat := range b.getMap() {
		if stat.SuccessTotal() != time.Duration(0) {
			metricBootstrapTimes.WithLabelValues(scope, metrics.LabelValueOutcomeSuccess).Observe(stat.SuccessTotal().Seconds())
		}
		if stat.FailureTotal() != time.Duration(0) {
			metricBootstrapTimes.WithLabelValues(scope, metrics.LabelValueOutcomeFail).Observe(stat.FailureTotal().Seconds())
		}
	}
}

func (b *bootstrapStatistics) getMap() map[string]*spanstat.SpanStat {
	return map[string]*spanstat.SpanStat{
		"overall":    &b.overall,
		"earlyInit":  &b.earlyInit,
		"k8sInit":    &b.k8sInit,
		"initAPI":    &b.initAPI,
		"initDaemon": &b.initDaemon,
		"ipam":       &b.ipam,
		"daemonInit": &b.daemonInit,
		"enim":       &b.enim,
	}
}

var (
	metricBootstrapTimes prometheus.ObserverVec
)

func registerBootstrapMetrics() {
	metricBootstrapTimes = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: metrics.Namespace,
		Subsystem: metrics.SubsystemAgent,
		Name:      "bootstrap_seconds",
		Help:      "Duration of bootstrap sequence",
	}, []string{metrics.LabelScope, metrics.LabelOutcome})

	if err := metrics.Register(metricBootstrapTimes); err != nil {
		log.WithError(err).Fatal("unable to register prometheus metric")
	}
}

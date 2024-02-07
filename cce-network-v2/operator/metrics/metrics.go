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

package metrics

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/operator/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/common"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
)

var (
	log = logging.DefaultLogger.WithField(logfields.LogSubsys, "metrics")
)

// Namespace is the namespace key to use for cce operator metrics.
const Namespace = "cce_operator"

var (
	// Registry is the global prometheus registry for cce-operator metrics.
	Registry   *prometheus.Registry
	shutdownCh chan struct{}
)

// Register registers metrics for cce-operator.
func Register() {
	log.Info("Registering Operator metrics")

	Registry = prometheus.NewPedanticRegistry()
	registerMetrics()

	m := http.NewServeMux()
	m.Handle("/metrics", promhttp.HandlerFor(Registry, promhttp.HandlerOpts{}))
	srv := &http.Server{
		Addr:    operatorOption.Config.OperatorPrometheusServeAddr,
		Handler: m,
	}

	shutdownCh = make(chan struct{})
	go func() {
		go func() {
			err := srv.ListenAndServe()
			switch err {
			case http.ErrServerClosed:
				log.Info("Metrics server shutdown successfully")
				return
			default:
				log.WithError(err).Fatal("Metrics server ListenAndServe failed")
			}
		}()

		<-shutdownCh
		log.Info("Received shutdown signal")
		if err := srv.Shutdown(context.TODO()); err != nil {
			log.WithError(err).Error("Shutdown operator metrics server failed")
		}
	}()
}

// Unregister shuts down the metrics server.
func Unregister() {
	log.Info("Shutting down metrics server")

	if shutdownCh == nil {
		return
	}

	shutdownCh <- struct{}{}
}

var (
	// IdentityGCSize records the identity GC results
	IdentityGCSize *prometheus.GaugeVec

	// IdentityGCRuns records how many times identity GC has run
	IdentityGCRuns *prometheus.GaugeVec

	// EndpointGCObjects records the number of times endpoint objects have been
	// garbage-collected.
	EndpointGCObjects *prometheus.CounterVec

	// CCEEndpointSliceDensity indicates the number of CEPs batched in a CES and it used to
	// collect the number of CEPs in CES at various buckets. For example,
	// number of CESs in the CEP range <0, 10>
	// number of CESs in the CEP range <11, 20>
	// number of CESs in the CEP range <21, 30> and so on
	CCEEndpointSliceDensity prometheus.Histogram

	// CCEEndpointsChangeCount indicates the total number of CEPs changed for every CES request sent to k8s-apiserver.
	// This metric is used to collect number of CEP changes happening at various buckets.
	CCEEndpointsChangeCount *prometheus.HistogramVec

	// CCEEndpointSliceSyncErrors used to track the total number of errors occurred during syncing CES with k8s-apiserver.
	CCEEndpointSliceSyncErrors prometheus.Counter

	// CCEEndpointSliceQueueDelay measures the time spent by CES's in the workqueue. This measures time difference between
	// CES insert in the workqueue and removal from workqueue.
	CCEEndpointSliceQueueDelay prometheus.Histogram

	// Cloud API
	CloudAPIRequestDurationMillisesconds prometheus.HistogramVec

	// WorkQuqueLens is the gauge representing the length of the work queue
	WorkQueueLens *prometheus.GaugeVec

	// WorkQueueEventCount is the counter of the number of events processed
	WorkQueueEventCount *prometheus.CounterVec

	// ControllerHandlerDurationMilliseconds is the histogram of the duration
	ControllerHandlerDurationMilliseconds *prometheus.HistogramVec
)

const (
	// LabelStatus marks the status of a resource or completed task
	LabelStatus = "status"

	// LabelOutcome indicates whether the outcome of the operation was successful or not
	LabelOutcome = "outcome"

	// LabelOpcode indicates the kind of CES metric, could be CEP insert or remove
	LabelOpcode = "opcode"

	// Label values

	// LabelValueOutcomeSuccess is used as a successful outcome of an operation
	LabelValueOutcomeSuccess = "success"

	// LabelValueOutcomeFail is used as an unsuccessful outcome of an operation
	LabelValueOutcomeFail = "fail"

	// LabelValueOutcomeAlive is used as outcome of alive identity entries
	LabelValueOutcomeAlive = "alive"

	// LabelValueOutcomeDeleted is used as outcome of deleted identity entries
	LabelValueOutcomeDeleted = "deleted"

	// LabelValueCEPInsert is used to indicate the number of CEPs inserted in a CES
	LabelValueCEPInsert = "cepinserted"

	// LabelValueCEPRemove is used to indicate the number of CEPs removed from a CES
	LabelValueCEPRemove = "cepremoved"

	// LabelSourceCluster is the label for source cluster name
	LabelSourceCluster = "source_cluster"

	// LabelBuildQueueName is the name of the build queue
	LabelBuildQueueName = "name"

	// LabelEventMethod is the label for the method of an event
	LabelEventMethod = "method"
)

func registerMetrics() []prometheus.Collector {
	// Builtin process metrics
	Registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{Namespace: Namespace}))

	// Metrics Setup
	defaultMetrics := metrics.DefaultMetrics()
	var collectors []prometheus.Collector
	metricsSlice := common.MapStringStructToSlice(defaultMetrics)
	_, collectors = metrics.CreateConfiguration(metricsSlice)

	IdentityGCSize = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "identity_gc_entries",
		Help:      "The number of alive and deleted identities at the end of a garbage collector run",
	}, []string{LabelStatus})
	collectors = append(collectors, IdentityGCSize)

	IdentityGCRuns = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: Namespace,
		Name:      "identity_gc_runs",
		Help:      "The number of times identity garbage collector has run",
	}, []string{LabelOutcome})
	collectors = append(collectors, IdentityGCRuns)

	EndpointGCObjects = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "endpoint_gc_objects",
		Help:      "The number of times endpoint objects have been garbage-collected",
	}, []string{LabelOutcome})
	collectors = append(collectors, EndpointGCObjects)

	CCEEndpointSliceDensity = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "number_of_ceps_per_ces",
		Help:      "The number of CEPs batched in a CES",
	})
	collectors = append(collectors, CCEEndpointSliceDensity)

	CCEEndpointsChangeCount = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "number_of_cep_changes_per_ces",
		Help:      "The number of changed CEPs in each CES update",
	}, []string{LabelOpcode})
	collectors = append(collectors, CCEEndpointsChangeCount)

	CCEEndpointSliceSyncErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: Namespace,
		Name:      "ces_sync_errors_total",
		Help:      "Number of CES sync errors",
	})
	collectors = append(collectors, CCEEndpointSliceSyncErrors)

	CCEEndpointSliceQueueDelay = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: Namespace,
		Name:      "ces_queueing_delay_seconds",
		Help:      "CCEEndpointSlice queueing delay in seconds",
	})
	collectors = append(collectors, CCEEndpointSliceQueueDelay)

	WorkQueueLens = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "work_queue_len",
			Help: "work queue len",
		},
		[]string{
			LabelSourceCluster,
			LabelBuildQueueName,
		},
	)
	collectors = append(collectors, WorkQueueLens)

	WorkQueueEventCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "work_queue_event_counter",
			Help: "work queue event counter",
		},
		[]string{
			LabelSourceCluster,
			LabelBuildQueueName,
			LabelOpcode,
		},
	)
	collectors = append(collectors, WorkQueueEventCount)

	ControllerHandlerDurationMilliseconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "controller_handler_duration_milliseconds",
			Help:    "controller handler latency in ms",
			Buckets: []float64{50, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 14800, 16800, 20800, 28800, 44800},
		},
		[]string{
			LabelSourceCluster,
			LabelBuildQueueName,
			LabelOpcode,
			LabelStatus,
		},
	)
	collectors = append(collectors, ControllerHandlerDurationMilliseconds)

	Registry.MustRegister(collectors...)
	return collectors
}

// DumpMetrics gets the current CCE operator metrics and dumps all into a
// Metrics structure. If metrics cannot be retrieved, returns an error.
func DumpMetrics() ([]*models.Metric, error) {
	result := []*models.Metric{}
	if Registry == nil {
		return result, nil
	}

	currentMetrics, err := Registry.Gather()
	if err != nil {
		return result, err
	}

	for _, val := range currentMetrics {

		metricName := val.GetName()
		metricType := val.GetType()

		for _, metricLabel := range val.Metric {
			labels := map[string]string{}
			for _, label := range metricLabel.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}

			var value float64
			switch metricType {
			case dto.MetricType_COUNTER:
				value = metricLabel.Counter.GetValue()
			case dto.MetricType_GAUGE:
				value = metricLabel.GetGauge().GetValue()
			case dto.MetricType_UNTYPED:
				value = metricLabel.GetUntyped().GetValue()
			case dto.MetricType_SUMMARY:
				value = metricLabel.GetSummary().GetSampleSum()
			case dto.MetricType_HISTOGRAM:
				value = metricLabel.GetHistogram().GetSampleSum()
			default:
				continue
			}

			metric := &models.Metric{
				Name:   metricName,
				Labels: labels,
				Value:  value,
			}
			result = append(result, metric)
		}
	}

	return result, nil
}

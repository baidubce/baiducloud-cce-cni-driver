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

// Package metrics holds prometheus metrics objects and related utility functions. It
// does not abstract away the prometheus client but the caller rarely needs to
// refer to prometheus directly.
package metrics

// Adding a metric
// - Add a metric object of the appropriate type as an exported variable
// - Register the new object in the init function

import (
	"net/http"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/sirupsen/logrus"
)

const (
	// ErrorTimeout is the value used to notify timeout errors.
	ErrorTimeout = "timeout"

	// ErrorProxy is the value used to notify errors on Proxy.
	ErrorProxy = "proxy"

	//L7DNS is the value used to report DNS label on metrics
	L7DNS = "dns"

	// SubsystemBPF is the subsystem to scope metrics related to the bpf syscalls.
	SubsystemBPF = "bpf"

	// SubsystemDatapath is the subsystem to scope metrics related to management of
	// the datapath. It is prepended to metric names and separated with a '_'.
	SubsystemDatapath = "datapath"

	// SubsystemAgent is the subsystem to scope metrics related to the cce agent itself.
	SubsystemAgent = "agent"

	// SubsystemK8s is the subsystem to scope metrics related to Kubernetes
	SubsystemK8s = "k8s"

	// SubsystemK8sClient is the subsystem to scope metrics related to the kubernetes client.
	SubsystemK8sClient = "k8s_client"

	// SubsystemKVStore is the subsystem to scope metrics related to the kvstore.
	SubsystemKVStore = "kvstore"

	// SubsystemFQDN is the subsystem to scope metrics related to the FQDN proxy.
	SubsystemFQDN = "fqdn"

	// SubsystemNodes is the subsystem to scope metrics related to the node manager.
	SubsystemNodes = "nodes"

	// SubsystemTriggers is the subsystem to scope metrics related to the trigger package.
	SubsystemTriggers = "triggers"

	// SubsystemAPILimiter is the subsystem to scope metrics related to the API limiter package.
	SubsystemAPILimiter = "api_limiter"

	// SubsystemNodeNeigh is the subsystem to scope metrics related to management of node neighbor.
	SubsystemNodeNeigh = "node_neigh"

	// Namespace is used to scope metrics from cce. It is prepended to metric
	// names and separated with a '_'
	Namespace = "cce"

	// LabelError indicates the type of error (string)
	LabelError = "error"

	// LabelOutcome indicates whether the outcome of the operation was successful or not
	LabelOutcome = "outcome"

	// LabelAttempts is the number of attempts it took to complete the operation
	LabelAttempts = "attempts"

	// Labels

	// LabelValueOutcomeSuccess is used as a successful outcome of an operation
	LabelValueOutcomeSuccess = "success"

	// LabelValueOutcomeFail is used as an unsuccessful outcome of an operation
	LabelValueOutcomeFail = "fail"

	// LabelEventSourceAPI marks event-related metrics that come from the API
	LabelEventSourceAPI = "api"

	// LabelEventSourceK8s marks event-related metrics that come from k8s
	LabelEventSourceK8s = "k8s"

	// LabelEventSourceFQDN marks event-related metrics that come from pkg/fqdn
	LabelEventSourceFQDN = "fqdn"

	// LabelEventSourceContainerd marks event-related metrics that come from docker
	LabelEventSourceContainerd = "docker"

	// LabelDatapathArea marks which area the metrics are related to (eg, which BPF map)
	LabelDatapathArea = "area"

	// LabelDatapathName marks a unique identifier for this metric.
	// The name should be defined once for a given type of error.
	LabelDatapathName = "name"

	// LabelDatapathFamily marks which protocol family (IPv4, IPV6) the metric is related to.
	LabelDatapathFamily = "family"

	// LabelProtocol marks the L4 protocol (TCP, ANY) for the metric.
	LabelProtocol = "protocol"

	// LabelSignalType marks the signal name
	LabelSignalType = "signal"

	// LabelSignalData marks the signal data
	LabelSignalData = "data"

	// LabelStatus the label from completed task
	LabelStatus = "status"

	// LabelPolicyEnforcement is the label used to see the enforcement status
	LabelPolicyEnforcement = "enforcement"

	// LabelPolicySource is the label used to see the enforcement status
	LabelPolicySource = "source"

	// LabelScope is the label used to defined multiples scopes in the same
	// metric. For example, one counter may measure a metric over the scope of
	// the entire event (scope=global), or just part of an event
	// (scope=slow_path)
	LabelScope = "scope"

	// LabelProtocolL7 is the label used when working with layer 7 protocols.
	LabelProtocolL7 = "protocol_l7"

	// LabelBuildState is the state a build queue entry is in
	LabelBuildState = "state"

	// LabelBuildQueueName is the name of the build queue
	LabelBuildQueueName = "name"

	// LabelAction is the label used to defined what kind of action was performed in a metric
	LabelAction = "action"

	// LabelSubsystem is the label used to refer to any of the child process
	// started by cce (Envoy, monitor, etc..)
	LabelSubsystem = "subsystem"

	// LabelKind is the kind of a label
	LabelKind = "kind"

	// LabelEventSource is the source of a label for event metrics
	// i.e. k8s, containerd, api.
	LabelEventSource = "source"

	// LabelPath is the label for the API path
	LabelPath = "path"
	// LabelMethod is the label for the HTTP method
	LabelMethod = "method"

	// LabelAPIReturnCode is the HTTP code returned for that API path
	LabelAPIReturnCode = "return_code"

	// LabelOperation is the label for BPF maps operations
	LabelOperation = "operation"

	// LabelMapName is the label for the BPF map name
	LabelMapName = "map_name"

	// LabelVersion is the label for the version number
	LabelVersion = "version"

	// LabelDirection is the label for traffic direction
	LabelDirection = "direction"

	// LabelSourceCluster is the label for source cluster name
	LabelSourceCluster = "source_cluster"

	// LabelSourceNodeName is the label for source node name
	LabelSourceNodeName = "source_node_name"

	// LabelTargetCluster is the label for target cluster name
	LabelTargetCluster = "target_cluster"

	// LabelTargetNodeIP is the label for target node IP
	LabelTargetNodeIP = "target_node_ip"

	// LabelTargetNodeName is the label for target node name
	LabelTargetNodeName = "target_node_name"

	// LabelTargetNodeType is the label for target node type (local_node, remote_intra_cluster, vs remote_inter_cluster)
	LabelTargetNodeType = "target_node_type"

	LabelLocationLocalNode          = "local_node"
	LabelLocationRemoteIntraCluster = "remote_intra_cluster"
	LabelLocationRemoteInterCluster = "remote_inter_cluster"

	// LabelType is the label for type in general (e.g. endpoint, node)
	LabelType         = "type"
	LabelPeerEndpoint = "endpoint"
	LabelPeerNode     = "node"

	LabelTrafficHTTP = "http"
	LabelTrafficICMP = "icmp"

	LabelAddressType          = "address_type"
	LabelAddressTypePrimary   = "primary"
	LabelAddressTypeSecondary = "secondary"

	// LabelEventMethod is the label for the method of an event
	LabelEventMethod       = "method"
	LabelEventMethodAdd    = "add"
	LabelEventMethodUpdate = "update"
	LabelEventMethodDelete = "delete"

	LabelErrorReason = "reason"
)

var (
	defaultMillSecondBukets = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 15000, 30000, 60000}

	registry = prometheus.NewPedanticRegistry()

	// APIInteractions is the total time taken to process an API call made
	// to the cce-agent
	APIInteractions = NoOpObserverVec

	// Status

	// NodeConnectivityStatus is the connectivity status between local node to
	// other node intra or inter cluster.
	NodeConnectivityStatus = NoOpGaugeVec

	// NodeConnectivityLatency is the connectivity latency between local node to
	// other node intra or inter cluster.
	NodeConnectivityLatency = NoOpGaugeVec

	// Events

	// EventTS*is the time in seconds since epoch that we last received an
	// event that we will handle
	// source is one of k8s, docker or apia

	// EventTS is the timestamp of k8s resource events.
	EventTS = NoOpGaugeVec

	// EventLagK8s is the lag calculation for k8s Pod events.
	EventLagK8s = NoOpGauge

	// Signals

	// SignalsHandled is the number of signals received.
	SignalsHandled = NoOpCounterVec

	// Services

	// ServicesCount number of services
	ServicesCount = NoOpCounterVec

	// Errors and warnings

	// ErrorsWarnings is the number of errors and warnings in cce-agent instances
	ErrorsWarnings = NoOpCounterVec

	// ControllerRuns is the number of times that a controller process runs.
	ControllerRuns = NoOpCounterVec

	// ControllerRunsDuration the duration of the controller process in seconds
	ControllerRunsDuration = NoOpObserverVec

	// subprocess, labeled by Subsystem
	SubprocessStart = NoOpCounterVec

	// Kubernetes Events

	// KubernetesEventProcessed is the number of Kubernetes events
	// processed labeled by scope, action and execution result
	KubernetesEventProcessed = NoOpCounterVec

	// KubernetesEventReceived is the number of Kubernetes events received
	// labeled by scope, action, valid data and equalness.
	KubernetesEventReceived = NoOpCounterVec

	// Kubernetes interactions

	// KubernetesAPIInteractions is the total time taken to process an API call made
	// to the kube-apiserver
	KubernetesAPIInteractions = NoOpObserverVec

	// KubernetesAPICallsTotal is the counter for all API calls made to
	// kube-apiserver.
	KubernetesAPICallsTotal = NoOpCounterVec

	// KubernetesCNPStatusCompletion is the number of seconds it takes to
	// complete a CNP status update
	KubernetesCNPStatusCompletion = NoOpObserverVec

	// IPAM events

	// IpamEvent is the number of IPAM events received labeled by action and
	// datapath family type
	IpamEvent = NoOpCounterVec

	// VersionMetric labelled by CCE version
	VersionMetric = NoOpGaugeVec

	// APILimiterProcessHistoryDuration is a histogram that measures the
	// individual wait durations of API limiters
	APILimiterProcessHistoryDuration = NoOpObserverVec

	// APILimiterRequestsInFlight is the gauge of the current and max
	// requests in flight
	APILimiterRequestsInFlight = NoOpGaugeVec

	// APILimiterRateLimit is the gauge of the current rate limiting
	// configuration including limit and burst
	APILimiterRateLimit = NoOpGaugeVec

	// APILimiterAdjustmentFactor is the gauge representing the latest
	// adjustment factor that was applied
	APILimiterAdjustmentFactor = NoOpGaugeVec

	// APILimiterProcessedRequests is the counter of the number of
	// processed (successful and failed) requests
	APILimiterProcessedRequests = NoOpCounterVec

	// Cloud API
	CloudAPIRequestDurationMillisesconds = NoOpObserverVec

	// WorkQuqueLens is the gauge representing the length of the work queue
	WorkQueueLens = NoOpGaugeVec

	// WorkQueueEventCount is the counter of the number of events processed
	WorkQueueEventCount = NoOpCounterVec

	// ControllerHandlerDurationMilliseconds is the histogram of the duration
	ControllerHandlerDurationMilliseconds = NoOpObserverVec

	// NoAvailableSubnetNodeCount is the counter of nodes that no avaiable subnet to create new eni
	IPAMErrorCounter = NoOpCounterVec

	// SubnetIPsGuage is the gauge of available IPs in subnet and borrowed IPs by eni
	SubnetIPsGuage = NoOpGaugeVec
)

type Configuration struct {
	APIInteractionsEnabled         bool
	NodeConnectivityStatusEnabled  bool
	NodeConnectivityLatencyEnabled bool

	EventTSEnabled           bool
	EventLagK8sEnabled       bool
	EventTSContainerdEnabled bool
	EventTSAPIEnabled        bool

	NoOpObserverVecEnabled bool
	NoOpCounterVecEnabled  bool

	SignalsHandledEnabled                bool
	ServicesCountEnabled                 bool
	ErrorsWarningsEnabled                bool
	ControllerRunsEnabled                bool
	ControllerRunsDurationEnabled        bool
	SubprocessStartEnabled               bool
	KubernetesEventProcessedEnabled      bool
	KubernetesEventReceivedEnabled       bool
	KubernetesTimeBetweenEventsEnabled   bool
	KubernetesAPIInteractionsEnabled     bool
	KubernetesAPICallsEnabled            bool
	KubernetesCNPStatusCompletionEnabled bool
	IpamEventEnabled                     bool
	SubnetIPsGuageEnabled                bool

	VersionMetric                        bool
	APILimiterProcessHistoryDuration     bool
	APILimiterRequestsInFlight           bool
	APILimiterRateLimit                  bool
	APILimiterAdjustmentFactor           bool
	APILimiterProcessedRequests          bool
	CloudAPIRequestDurationMillisesconds bool

	WorkQueueLens                         bool
	WorkQueueEventCount                   bool
	ControllerHandlerDurationMilliseconds bool
}

func DefaultMetrics() map[string]struct{} {
	return map[string]struct{}{
		Namespace + "_" + SubsystemAgent + "_api_process_time_seconds": {},
		Namespace + "_event_ts": {},

		Namespace + "_node_connectivity_status":                                         {},
		Namespace + "_node_connectivity_latency_seconds":                                {},
		Namespace + "_errors_warnings_total":                                            {},
		Namespace + "_controllers_runs_total":                                           {},
		Namespace + "_controllers_runs_duration_seconds":                                {},
		Namespace + "_subprocess_start_total":                                           {},
		Namespace + "_kubernetes_events_total":                                          {},
		Namespace + "_kubernetes_events_received_total":                                 {},
		Namespace + "_" + SubsystemK8sClient + "_api_latency_time_seconds":              {},
		Namespace + "_" + SubsystemK8sClient + "_api_calls_total":                       {},
		Namespace + "_" + SubsystemK8s + "_cnp_status_completion_seconds":               {},
		Namespace + "_ipam_events_total":                                                {},
		Namespace + "_version":                                                          {},
		Namespace + "_" + SubsystemAPILimiter + "_process_history_duration_millseconds": {},
		Namespace + "_" + SubsystemAPILimiter + "_requests_in_flight":                   {},
		Namespace + "_" + SubsystemAPILimiter + "_rate_limit":                           {},
		Namespace + "_" + SubsystemAPILimiter + "_adjustment_factor":                    {},
		Namespace + "_" + SubsystemAPILimiter + "_processed_requests_total":             {},
		Namespace + "_cloud_api_request_duration_milliseconds":                          {},
		Namespace + "_work_queue_len":                                                   {},
		Namespace + "_work_queue_event_counter":                                         {},
		Namespace + "_controller_handler_duration_milliseconds":                         {},
		Namespace + "_ipam_error_counter":                                               {},
		Namespace + "_subnet_ips_guage":                                                 {},
	}
}

// CreateConfiguration returns a Configuration with all metrics that are
// considered enabled from the given slice of metricsEnabled as well as a slice
// of prometheus.Collectors that must be registered in the prometheus default
// register.
func CreateConfiguration(metricsEnabled []string) (Configuration, []prometheus.Collector) {
	var collectors []prometheus.Collector
	c := Configuration{}

	for _, metricName := range metricsEnabled {
		switch metricName {
		case Namespace + "_" + SubsystemAgent + "_api_process_time_seconds":
			APIInteractions = prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: Namespace,
				Subsystem: SubsystemAgent,
				Name:      "api_process_time_seconds",
				Help:      "Duration of processed API calls labeled by path, method and return code.",
			}, []string{LabelPath, LabelMethod, LabelAPIReturnCode})

			collectors = append(collectors, APIInteractions)
			c.APIInteractionsEnabled = true

		case Namespace + "_event_ts":
			EventTS = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Name:      "event_ts",
				Help:      "Last timestamp when we received an event",
			}, []string{LabelEventSource, LabelScope, LabelAction})

			collectors = append(collectors, EventTS)
			c.EventTSEnabled = true

			EventLagK8s = prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace:   Namespace,
				Name:        "k8s_event_lag_seconds",
				Help:        "Lag for Kubernetes events - computed value between receiving a CNI ADD event from kubelet and a Pod event received from kube-api-server",
				ConstLabels: prometheus.Labels{"source": LabelEventSourceK8s},
			})

			collectors = append(collectors, EventLagK8s)
			c.EventLagK8sEnabled = true

		case Namespace + "_" + SubsystemDatapath + "_signals_handled_total":
			SignalsHandled = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Subsystem: SubsystemDatapath,
				Name:      "signals_handled_total",
				Help: "Number of times that the datapath signal handler process was run " +
					"labeled by signal type, data and completion status",
			}, []string{LabelSignalType, LabelSignalData, LabelStatus})

			collectors = append(collectors, SignalsHandled)
			c.SignalsHandledEnabled = true

		case Namespace + "_services_events_total":
			ServicesCount = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Name:      "services_events_total",
				Help:      "Number of services events labeled by action type",
			}, []string{LabelAction})

			collectors = append(collectors, ServicesCount)
			c.ServicesCountEnabled = true

		case Namespace + "_errors_warnings_total":
			ErrorsWarnings = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Name:      "errors_warnings_total",
				Help:      "Number of total errors in cce-agent instances",
			}, []string{"level", "subsystem"})

			collectors = append(collectors, ErrorsWarnings)
			c.ErrorsWarningsEnabled = true

		case Namespace + "_controllers_runs_total":
			ControllerRuns = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Name:      "controllers_runs_total",
				Help:      "Number of times that a controller process was run labeled by completion status",
			}, []string{LabelStatus})

			collectors = append(collectors, ControllerRuns)
			c.ControllerRunsEnabled = true

		case Namespace + "_controllers_runs_duration_seconds":
			ControllerRunsDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: Namespace,
				Name:      "controllers_runs_duration_seconds",
				Help:      "Duration in seconds of the controller process labeled by completion status",
			}, []string{LabelStatus})

			collectors = append(collectors, ControllerRunsDuration)
			c.ControllerRunsDurationEnabled = true

		case Namespace + "_subprocess_start_total":
			SubprocessStart = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Name:      "subprocess_start_total",
				Help:      "Number of times that CCE has started a subprocess, labeled by subsystem",
			}, []string{LabelSubsystem})

			collectors = append(collectors, SubprocessStart)
			c.SubprocessStartEnabled = true

		case Namespace + "_kubernetes_events_total":
			KubernetesEventProcessed = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Name:      "kubernetes_events_total",
				Help:      "Number of Kubernetes events processed labeled by scope, action and execution result",
			}, []string{LabelScope, LabelAction, LabelStatus})

			collectors = append(collectors, KubernetesEventProcessed)
			c.KubernetesEventProcessedEnabled = true

		case Namespace + "_kubernetes_events_received_total":
			KubernetesEventReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Name:      "kubernetes_events_received_total",
				Help:      "Number of Kubernetes events received labeled by scope, action, valid data and equalness",
			}, []string{LabelScope, LabelAction, "valid", "equal"})

			collectors = append(collectors, KubernetesEventReceived)
			c.KubernetesEventReceivedEnabled = true

		case Namespace + "_" + SubsystemK8sClient + "_api_latency_time_seconds":
			KubernetesAPIInteractions = prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: Namespace,
				Subsystem: SubsystemK8sClient,
				Name:      "api_latency_time_seconds",
				Help:      "Duration of processed API calls labeled by path and method.",
			}, []string{LabelPath, LabelMethod})

			collectors = append(collectors, KubernetesAPIInteractions)
			c.KubernetesAPIInteractionsEnabled = true

		case Namespace + "_" + SubsystemK8sClient + "_api_calls_total":
			KubernetesAPICallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Subsystem: SubsystemK8sClient,
				Name:      "api_calls_total",
				Help:      "Number of API calls made to kube-apiserver labeled by host, method and return code.",
			}, []string{"host", LabelMethod, LabelAPIReturnCode})

			collectors = append(collectors, KubernetesAPICallsTotal)
			c.KubernetesAPICallsEnabled = true

		case Namespace + "_" + SubsystemK8s + "_cnp_status_completion_seconds":
			KubernetesCNPStatusCompletion = prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: Namespace,
				Subsystem: SubsystemK8s,
				Name:      "cnp_status_completion_seconds",
				Help:      "Duration in seconds in how long it took to complete a CNP status update",
			}, []string{LabelAttempts, LabelOutcome})

			collectors = append(collectors, KubernetesCNPStatusCompletion)
			c.KubernetesCNPStatusCompletionEnabled = true

		case Namespace + "_ipam_events_total":
			IpamEvent = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Name:      "ipam_events_total",
				Help:      "Number of IPAM events received labeled by action and datapath family type",
			}, []string{LabelAction, LabelDatapathFamily})

			collectors = append(collectors, IpamEvent)
			c.IpamEventEnabled = true

		case Namespace + "_subnet_ips_guage":
			SubnetIPsGuage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Name:      "subnet_ips_guage",
				Help:      "Number of IP addresses in a subnet labeled by eni and subnet id",
			}, []string{LabelKind, "eniid", "sbnid", "owner"})
			collectors = append(collectors, SubnetIPsGuage)
			c.SubnetIPsGuageEnabled = true

		case Namespace + "_version":
			VersionMetric = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Name:      "version",
				Help:      "CCE version",
			}, []string{LabelVersion})

			VersionMetric.WithLabelValues(version.GetCCEVersion().Version)

			collectors = append(collectors, VersionMetric)
			c.VersionMetric = true

		case Namespace + "_" + SubsystemAPILimiter + "_process_history_duration_millseconds":
			APILimiterProcessHistoryDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: Namespace,
				Subsystem: SubsystemAPILimiter,
				Name:      "process_history_duration_miliseconds",
				Help:      "Histogram over duration of waiting period for API calls subjects to rate limiting",
				Buckets:   defaultMillSecondBukets,
			}, []string{"api_call", LabelScope, LabelOutcome})

			collectors = append(collectors, APILimiterProcessHistoryDuration)
			c.APILimiterProcessHistoryDuration = true
		case Namespace + "_" + SubsystemAPILimiter + "_requests_in_flight":
			APILimiterRequestsInFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Subsystem: SubsystemAPILimiter,
				Name:      "requests_in_flight",
				Help:      "Current requests in flight",
			}, []string{"api_call", "value"})

			collectors = append(collectors, APILimiterRequestsInFlight)
			c.APILimiterRequestsInFlight = true

		case Namespace + "_" + SubsystemAPILimiter + "_rate_limit":
			APILimiterRateLimit = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Subsystem: SubsystemAPILimiter,
				Name:      "rate_limit",
				Help:      "Current rate limiting configuration",
			}, []string{"api_call", "value"})

			collectors = append(collectors, APILimiterRateLimit)
			c.APILimiterRateLimit = true

		case Namespace + "_" + SubsystemAPILimiter + "_adjustment_factor":
			APILimiterAdjustmentFactor = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Subsystem: SubsystemAPILimiter,
				Name:      "adjustment_factor",
				Help:      "Current adjustment factor while auto adjusting",
			}, []string{"api_call"})

			collectors = append(collectors, APILimiterAdjustmentFactor)
			c.APILimiterAdjustmentFactor = true

		case Namespace + "_" + SubsystemAPILimiter + "_processed_requests_total":
			APILimiterProcessedRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: Namespace,
				Subsystem: SubsystemAPILimiter,
				Name:      "processed_requests_total",
				Help:      "Total number of API requests processed",
			}, []string{"api_call", LabelOutcome})

			collectors = append(collectors, APILimiterProcessedRequests)
			c.APILimiterProcessedRequests = true

		case Namespace + "_node_connectivity_status":
			NodeConnectivityStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Name:      "node_connectivity_status",
				Help:      "The last observed status of both ICMP and HTTP connectivity between the current CCE agent and other CCE nodes",
			}, []string{
				LabelSourceCluster,
				LabelSourceNodeName,
				LabelTargetCluster,
				LabelTargetNodeName,
				LabelTargetNodeType,
				LabelType,
			})

			collectors = append(collectors, NodeConnectivityStatus)
			c.NodeConnectivityStatusEnabled = true

		case Namespace + "_node_connectivity_latency_seconds":
			NodeConnectivityLatency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: Namespace,
				Name:      "node_connectivity_latency_seconds",
				Help:      "The last observed latency between the current CCE agent and other CCE nodes in seconds",
			}, []string{
				LabelSourceCluster,
				LabelSourceNodeName,
				LabelTargetCluster,
				LabelTargetNodeName,
				LabelTargetNodeIP,
				LabelTargetNodeType,
				LabelType,
				LabelProtocol,
				LabelAddressType,
			})

			collectors = append(collectors, NodeConnectivityLatency)
			c.NodeConnectivityLatencyEnabled = true

		case Namespace + "_cloud_api_request_duration_milliseconds":
			CloudAPIRequestDurationMillisesconds = prometheus.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "cloud_api_request_duration_millseconds",
					Help:    "bce openapi latency in ms",
					Buckets: []float64{50, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 14800, 16800, 20800, 28800, 44800},
				},
				[]string{
					LabelSourceCluster,
					LabelPath,
					LabelError,
					LabelAPIReturnCode,
				},
			)
		case Namespace + "_work_queue_len":
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
		case Namespace + "_work_queue_event_counter":
			WorkQueueEventCount = prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "work_queue_event_counter",
					Help: "work queue event counter",
				},
				[]string{
					LabelSourceCluster,
					LabelBuildQueueName,
					LabelEventMethod,
				},
			)
		case Namespace + "_controller_handler_duration_milliseconds":
			ControllerHandlerDurationMilliseconds = prometheus.NewHistogramVec(
				prometheus.HistogramOpts{
					Name:    "controller_handler_duration_milliseconds",
					Help:    "controller handler latency in ms",
					Buckets: []float64{50, 100, 200, 400, 800, 1600, 3200, 6400, 12800, 14800, 16800, 20800, 28800, 44800},
				},
				[]string{
					LabelSourceCluster,
					LabelBuildQueueName,
					LabelEventMethod,
					LabelError,
				},
			)
		case Namespace + "_ipam_error_counter":
			IPAMErrorCounter = prometheus.NewCounterVec(
				prometheus.CounterOpts{
					Name: "ipam_error_counter",
					Help: "ipam error counter",
				},
				[]string{
					LabelError,
					LabelKind,
					LabelBuildQueueName,
				},
			)
		}

	}

	return c, collectors
}

// GaugeWithThreshold is a prometheus gauge that registers itself with
// prometheus if over a threshold value and unregisters when under.
type GaugeWithThreshold struct {
	gauge     prometheus.Gauge
	threshold float64
	active    bool
}

// Set the value of the GaugeWithThreshold.
func (gwt *GaugeWithThreshold) Set(value float64) {
	overThreshold := value > gwt.threshold
	if gwt.active && !overThreshold {
		gwt.active = !Unregister(gwt.gauge)
		if gwt.active {
			logrus.WithField("metric", gwt.gauge.Desc().String()).Warning("Failed to unregister metric")
		}
	} else if !gwt.active && overThreshold {
		err := Register(gwt.gauge)
		gwt.active = err == nil
		if err != nil {
			logrus.WithField("metric", gwt.gauge.Desc().String()).WithError(err).Warning("Failed to register metric")
		}
	}

	gwt.gauge.Set(value)
}

// NewGaugeWithThreshold creates a new GaugeWithThreshold.
func NewGaugeWithThreshold(name string, subsystem string, desc string, labels map[string]string, threshold float64) *GaugeWithThreshold {
	return &GaugeWithThreshold{
		gauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace:   Namespace,
			Subsystem:   subsystem,
			Name:        name,
			Help:        desc,
			ConstLabels: labels,
		}),
		threshold: threshold,
		active:    false,
	}
}

// NewBPFMapPressureGauge creates a new GaugeWithThreshold for the
// cce_bpf_map_pressure metric with the map name as constant label.
func NewBPFMapPressureGauge(mapname string, threshold float64) *GaugeWithThreshold {
	return NewGaugeWithThreshold(
		"map_pressure",
		SubsystemBPF,
		"Fill percentage of map, tagged by map name",
		map[string]string{
			LabelMapName: mapname,
		},
		threshold,
	)
}

func init() {
	MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{Namespace: Namespace}))
	MustRegister(collectors.NewGoCollector())
}

// MustRegister adds the collector to the registry, exposing this metric to
// prometheus scrapes.
// It will panic on error.
func MustRegister(c ...prometheus.Collector) {
	registry.MustRegister(c...)
}

// Register registers a collector
func Register(c prometheus.Collector) error {
	return registry.Register(c)
}

// RegisterList registers a list of collectors. If registration of one
// collector fails, no collector is registered.
func RegisterList(list []prometheus.Collector) error {
	registered := []prometheus.Collector{}

	for _, c := range list {
		if err := Register(c); err != nil {
			for _, c := range registered {
				Unregister(c)
			}
			return err
		}

		registered = append(registered, c)
	}

	return nil
}

// Unregister unregisters a collector
func Unregister(c prometheus.Collector) bool {
	return registry.Unregister(c)
}

// Enable begins serving prometheus metrics on the address passed in. Addresses
// of the form ":8080" will bind the port on all interfaces.
func Enable(addr string) <-chan error {
	errs := make(chan error, 1)

	go func() {
		// The Handler function provides a default handler to expose metrics
		// via an HTTP server. "/metrics" is the usual endpoint for that.
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
		srv := http.Server{
			Addr:    addr,
			Handler: mux,
		}
		errs <- srv.ListenAndServe()
	}()

	return errs
}

// GetCounterValue returns the current value
// stored for the counter
func GetCounterValue(m prometheus.Counter) float64 {
	var pm dto.Metric
	err := m.Write(&pm)
	if err == nil {
		return *pm.Counter.Value
	}
	return 0
}

// GetGaugeValue returns the current value stored for the gauge. This function
// is useful in tests.
func GetGaugeValue(m prometheus.Gauge) float64 {
	var pm dto.Metric
	err := m.Write(&pm)
	if err == nil {
		return *pm.Gauge.Value
	}
	return 0
}

// DumpMetrics gets the current CCE metrics and dumps all into a
// models.Metrics structure.If metrics cannot be retrieved, returns an error
func DumpMetrics() ([]*models.Metric, error) {
	result := []*models.Metric{}
	currentMetrics, err := registry.Gather()
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

// Error2Outcome converts an error to LabelOutcome
func Error2Outcome(err error) string {
	if err != nil {
		return LabelValueOutcomeFail
	}

	return LabelValueOutcomeSuccess
}

func BoolToFloat64(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

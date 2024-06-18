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

package option

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	bceapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/command"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/common"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/version"
)

var (
	log = logging.DefaultLogger.WithField(logfields.LogSubsys, "config")
)

const (
	// GopsPort is the TCP port for the gops server.
	GopsPort = "gops-port"
	// AgentHealthPort is the TCP port for agent health status API
	AgentHealthPort = "health-port"

	// ClusterHealthPort is the TCP port for cluster-wide network connectivity health API
	ClusterHealthPort = "cluster-health-port"

	// AgentLabels are additional labels to identify this agent
	AgentLabels = "agent-labels"

	// AllowLocalhost is the policy when to allow local stack to reach local endpoints { auto | always | policy }
	AllowLocalhost = "allow-localhost"

	// AllowLocalhostAuto defaults to policy except when running in
	// Kubernetes where it then defaults to "always"
	AllowLocalhostAuto = "auto"

	// AllowLocalhostAlways always allows the local stack to reach local
	// endpoints
	AllowLocalhostAlways = "always"

	// AnnotateK8sNode enables annotating a kubernetes node while bootstrapping
	// the daemon, which can also be disbled using this option.
	AnnotateK8sNode = "annotate-k8s-node"
	// ConfigFile is the Configuration file (default "$HOME/cced.yaml")
	ConfigFile = "config"

	// ConfigDir is the directory that contains a file for each option where
	// the filename represents the option name and the content of that file
	// represents the value of that option.
	ConfigDir = "config-dir"

	// DebugArg is the argument enables debugging mode
	DebugArg = "debug"

	// Add unreachable routes on pod deletion
	EnableUnreachableRoutes = "enable-unreachable-routes"

	// IPv4Range is the per-node IPv4 endpoint prefix, e.g. 10.16.0.0/16
	IPv4Range = "ipv4-range"

	// IPv6Range is the per-node IPv6 endpoint prefix, must be /96, e.g. fd02:1:1::/96
	IPv6Range = "ipv6-range"

	// IPv4ServiceRange is the Kubernetes IPv4 services CIDR if not inside cluster prefix
	IPv4ServiceRange = "ipv4-service-range"

	// IPv6ServiceRange is the Kubernetes IPv6 services CIDR if not inside cluster prefix
	IPv6ServiceRange = "ipv6-service-range"

	// IPv6ClusterAllocCIDRName is the name of the IPv6ClusterAllocCIDR option
	IPv6ClusterAllocCIDRName = "ipv6-cluster-alloc-cidr"

	// K8sRequireIPv4PodCIDRName is the name of the K8sRequireIPv4PodCIDR option
	K8sRequireIPv4PodCIDRName = "k8s-require-ipv4-pod-cidr"

	// K8sRequireIPv6PodCIDRName is the name of the K8sRequireIPv6PodCIDR option
	K8sRequireIPv6PodCIDRName = "k8s-require-ipv6-pod-cidr"

	// K8sWatcherEndpointSelector specifies the k8s endpoints that CCE
	// should watch for.
	K8sWatcherEndpointSelector = "k8s-watcher-endpoint-selector"

	// K8sAPIServer is the kubernetes api address server (for https use --k8s-kubeconfig-path instead)
	K8sAPIServer = "k8s-api-server"

	// K8sKubeConfigPath is the absolute path of the kubernetes kubeconfig file
	K8sKubeConfigPath = "k8s-kubeconfig-path"

	// K8sServiceCacheSize is service cache size for cce k8s package.
	K8sServiceCacheSize = "k8s-service-cache-size"

	// K8sSyncTimeout is the timeout since last event was received to synchronize all resources with k8s.
	K8sSyncTimeoutName = "k8s-sync-timeout"

	// LibDir enables the directory path to store runtime build environment
	LibDir = "lib-dir"

	// LogDriver sets logging endpoints to use for example syslog, fluentd
	LogDriver = "log-driver"

	// LogOpt sets log driver options for cce
	LogOpt = "log-opt"

	// Logstash enables logstash integration
	Logstash = "logstash"

	IPTablesLockTimeout = "iptables-lock-timeout"

	// IPTablesRandomFully sets iptables flag random-fully on masquerading rules
	IPTablesRandomFully = "iptables-random-fully"

	// IPv6NodeAddr is the IPv6 address of node
	IPv6NodeAddr = "ipv6-node"

	// IPv4NodeAddr is the IPv4 address of node
	IPv4NodeAddr = "ipv4-node"

	// Restore restores state, if possible, from previous daemon
	Restore = "restore"

	// SocketPath sets daemon's socket path to listen for connections
	SocketPath = "socket-path"

	// StateDir is the directory path to store runtime state
	StateDir = "state-dir"

	// TracePayloadlen length of payload to capture when tracing
	TracePayloadlen = "trace-payloadlen"

	// Version prints the version information
	Version = "version"

	// PProf enables serving the pprof debugging API
	PProf = "pprof"

	// PProfPort is the port that the pprof listens on
	PProfPort = "pprof-port"

	ProcFs = "procfs"

	// PrometheusServeAddr IP:Port on which to serve prometheus metrics (pass ":Port" to bind on all interfaces, "" is off)
	PrometheusServeAddr = "prometheus-serve-addr"

	// CMDRef is the path to cmdref output directory
	CMDRef = "cmdref"

	// MTUName is the name of the MTU option
	MTUName = "mtu"

	// HostServicesTCP is the name of EnableHostServicesTCP config
	HostServicesTCP = "tcp"

	// HostServicesUDP is the name of EnableHostServicesUDP config
	HostServicesUDP = "udp"

	// SingleClusterRouteName is the name of the SingleClusterRoute option
	//
	// SingleClusterRoute enables use of a single route covering the entire
	// cluster CIDR to point to the cce_host interface instead of using
	// a separate route for each cluster node CIDR. This option is not
	// compatible with Tunnel=TunnelDisabled
	SingleClusterRouteName = "single-cluster-route"

	// MonitorAggregationInterval configures interval for monitor-aggregation
	MonitorAggregationInterval = "monitor-aggregation-interval"

	// MonitorAggregationFlags configures TCP flags used by monitor aggregation.
	MonitorAggregationFlags = "monitor-aggregation-flags"

	// cceEnvPrefix is the prefix used for environment variables
	cceEnvPrefix = "CCE_"

	// LogSystemLoadConfigName is the name of the option to enable system
	// load loggging
	LogSystemLoadConfigName = "log-system-load"

	// DisableCCEEndpointCRDName is the name of the option to disable
	// use of the CEP CRD
	DisableCCEEndpointCRDName = "disable-endpoint-crd"

	// DisableENICRDName is the name of the option to disable
	// use of the ENI CRD
	DisableENICRDName = "disable-eni-crd"

	// MaxCtrlIntervalName and MaxCtrlIntervalNameEnv allow configuration
	// of MaxControllerInterval.
	MaxCtrlIntervalName = "max-controller-interval"

	// K8sNamespaceName is the name of the K8sNamespace option
	K8sNamespaceName = "k8s-namespace"

	// AgentNotReadyNodeTaintKeyName is the name of the option to set
	// AgentNotReadyNodeTaintKey
	AgentNotReadyNodeTaintKeyName = "agent-not-ready-taint-key"

	// EnableIPv4Name is the name of the option to enable IPv4 support
	EnableIPv4Name = "enable-ipv4"

	// EnableIPv6Name is the name of the option to enable IPv6 support
	EnableIPv6Name = "enable-ipv6"

	// EnableIPv6NDPName is the name of the option to enable IPv6 NDP support
	EnableIPv6NDPName = "enable-ipv6-ndp"

	// IPv6MCastDevice is the name of the option to select IPv6 multicast device
	IPv6MCastDevice = "ipv6-mcast-device"

	// EnableMonitor is the name of the option to enable the monitor socket
	EnableMonitorName = "enable-monitor"

	// MonitorQueueSizeName is the name of the option MonitorQueueSize
	MonitorQueueSizeName = "monitor-queue-size"

	// IPAllocationTimeout is the timeout when allocating CIDRs
	IPAllocationTimeout = "ip-allocation-timeout"

	// EnableHealthChecking is the name of the EnableHealthChecking option
	EnableHealthChecking = "enable-health-checking"

	// EnableEndpointHealthChecking is the name of the EnableEndpointHealthChecking option
	EnableEndpointHealthChecking = "enable-endpoint-health-checking"

	// EndpointGCInterval interval to attempt garbage collection of
	// endpoints that are no longer alive and healthy.
	EndpointGCInterval = "endpoint-gc-interval"

	// K8sEventHandover is the name of the K8sEventHandover option
	K8sEventHandover = "enable-k8s-event-handover"

	// Metrics represents the metrics subsystem that CCE should expose
	// to prometheus.
	Metrics = "metrics"

	// LoopbackIPv4 is the address to use for service loopback SNAT
	LoopbackIPv4 = "ipv4-service-loopback-address"

	// LocalRouterIPv4 is the link-local IPv4 address to use for CCE router device
	LocalRouterIPv4 = "local-router-ipv4"

	// LocalRouterIPv6 is the link-local IPv6 address to use for CCE router device
	LocalRouterIPv6 = "local-router-ipv6"

	// EndpointInterfaceNamePrefix is the prefix name of the interface
	// names shared by all endpoints
	EndpointInterfaceNamePrefix = "endpoint-interface-name-prefix"

	// SkipCRDCreation specifies whether the CustomResourceDefinition will be
	// created by the daemon
	SkipCRDCreation = "skip-crd-creation"

	// EnableEndpointRoutes enables use of per endpoint routes
	EnableEndpointRoutes = "enable-endpoint-routes"

	// ExcludeLocalAddress excludes certain addresses to be recognized as a
	// local address
	ExcludeLocalAddress = "exclude-local-address"

	EnableBandwidthManager   = "enable-bandwidth-manager"
	EnableEgressPriority     = "enable-egress-priority"
	EnableEgressPriorityDSCP = "enable-egress-priority-dscp"

	// IPv4PodSubnets A list of IPv4 subnets that pods may be
	// assigned from. Used with CNI chaining where IPs are not directly managed
	// by CCE.
	IPv4PodSubnets = "ipv4-pod-subnets"

	// IPv6PodSubnets A list of IPv6 subnets that pods may be
	// assigned from. Used with CNI chaining where IPs are not directly managed
	// by CCE.
	IPv6PodSubnets = "ipv6-pod-subnets"

	// IPAM is the IPAM method to use
	IPAM = "ipam"

	// K8sClientQPSLimit is the queries per second limit for the K8s client. Defaults to k8s client defaults.
	K8sClientQPSLimit = "k8s-client-qps"

	// K8sClientBurst is the burst value allowed for the K8s client. Defaults to k8s client defaults.
	K8sClientBurst        = "k8s-client-burst"
	K8sEnableAPIDiscovery = "k8s-api-discovery"

	// AutoCreateNetResourceSetResource enables automatic creation of a
	// NetResourceSet resource for the local node
	AutoCreateNetResourceSetResource = "auto-create-network-resource-set-resource"

	// IPv4NativeRoutingCIDR describes a v4 CIDR in which pod IPs are routable
	IPv4NativeRoutingCIDR = "ipv4-native-routing-cidr"

	// IPv6NativeRoutingCIDR describes a v6 CIDR in which pod IPs are routable
	IPv6NativeRoutingCIDR = "ipv6-native-routing-cidr"

	// K8sHeartbeatTimeout configures the timeout for apiserver heartbeat
	K8sHeartbeatTimeout = "k8s-heartbeat-timeout"

	// APIRateLimitName enables configuration of the API rate limits
	// API Rate Limiting

	// APIRateLimitName enables configuration of the API rate limits
	APIRateLimitName = "api-rate-limit"

	// DefaultAPIBurst is the burst value allowed when accessing external Cloud APIs
	DefaultAPIBurst = "default-api-burst"

	// DefaultAPIQPSLimit is the queries per second limit when accessing external Cloud APIs
	DefaultAPIQPSLimit = "default-api-qps"

	// DefaultAPITimeoutLimit is the timeout limit when accessing external Cloud APIs
	DefaultAPITimeoutLimit = "default-api-timeout"

	// CRDWaitTimeout is the timeout in which CCE will exit if CRDs are not
	// available.
	CRDWaitTimeout = "crd-wait-timeout"

	// CCEEndpointGCInterval interval of single machine recycling endpoint
	CCEEndpointGCInterval = "cce-endpoint-gc-interval"
	// FixedIPTimeout Timeout for waiting for the fixed IP assignment to succeed
	FixedIPTimeout = "fixed-ip-allocate-timeout"

	// for bce configuration

	// BCECloudVPCID allows user to specific vpc
	BCECloudVPCID = "bce-cloud-vpc-id"
	// ClusterID is the cluster ID of the CCE cluster
	ClusterID              = "cce-cluster-id"
	ResourceResyncInterval = "resource-resync-interval"
	// eni configuration

	// this flags only use for vpc-eni mode
	ENIUseMode                    = "eni-use-mode"
	MaxAllocateENI                = "max-allocate-eni"
	MaxIPsPerENI                  = "max-ips-per-eni"
	ENIPreAllocateENINum          = "eni-pre-allocate-num"
	ENISubnets                    = "eni-subnet-ids"
	ENIRouteTableOffset           = "eni-route-table-offset"
	ENISecurityGroupIDs           = "eni-security-group-ids"
	ENIEnterpriseSecurityGroupIds = "eni-enterprise-security-group-ids"
	ENIInstallSourceBasedRouting  = "eni-install-source-based-routing"
	IPPoolMinAllocateIPs          = "ippool-min-allocate-ips"
	IPPoolPreAllocate             = "ippool-pre-allocate"
	IPPoolMaxAboveWatermark       = "ippool-max-above-watermark"

	ExtCNIPluginsList = "ext-cni-plugins"
)

// Available option for DaemonConfig.Tunnel
const (
	// TunnelVXLAN specifies VXLAN encapsulation
	TunnelVXLAN = "vxlan"

	// TunnelGeneve specifies Geneve encapsulation
	TunnelGeneve = "geneve"

	// TunnelDisabled specifies to disable encapsulation
	TunnelDisabled = "disabled"
)

const (
	// WriteCNIConfigurationWhenReady writes the CNI configuration to the
	// specified location once the agent is ready to serve requests. This
	// allows to keep a Kubernetes node NotReady until CCE is up and
	// running and able to schedule endpoints.
	WriteCNIConfigurationWhenReady = "write-cni-conf-when-ready"

	// EnableCCEEndpointSlice enables the cce endpoint slicing feature.
	EnableCCEEndpointSlice = "enable-cce-endpoint-slice"
)

const (
	// NodePortMinDefault is the minimal port to listen for NodePort requests
	NodePortMinDefault = 30000

	// NodePortMaxDefault is the maximum port to listen for NodePort requests
	NodePortMaxDefault = 32767
)

// GetTunnelModes returns the list of all tunnel modes
func GetTunnelModes() string {
	return fmt.Sprintf("%s, %s, %s", TunnelVXLAN, TunnelGeneve, TunnelDisabled)
}

// getEnvName returns the environment variable to be used for the given option name.
func getEnvName(option string) string {
	under := strings.Replace(option, "-", "_", -1)
	upper := strings.ToUpper(under)
	return cceEnvPrefix + upper
}

// RegisteredOptions maps all options that are bind to viper.
var RegisteredOptions = map[string]struct{}{}

// BindEnv binds the option name with an deterministic generated environment
// variable which s based on the given optName. If the same optName is bind
// more than 1 time, this function panics.
func BindEnv(optName string) {
	registerOpt(optName)
	viper.BindEnv(optName, getEnvName(optName))
}

// BindEnvWithLegacyEnvFallback binds the given option name with either the same
// environment variable as BindEnv, if it's set, or with the given legacyEnvName.
//
// The function is used to work around the viper.BindEnv limitation that only
// one environment variable can be bound for an option, and we need multiple
// environment variables due to backward compatibility reasons.
func BindEnvWithLegacyEnvFallback(optName, legacyEnvName string) {
	registerOpt(optName)

	envName := getEnvName(optName)
	if os.Getenv(envName) == "" {
		envName = legacyEnvName
	}

	viper.BindEnv(optName, envName)
}

func registerOpt(optName string) {
	_, ok := RegisteredOptions[optName]
	if ok || optName == "" {
		panic(fmt.Errorf("option already registered: %s", optName))
	}
	RegisteredOptions[optName] = struct{}{}
}

// LogRegisteredOptions logs all options that where bind to viper.
func LogRegisteredOptions(entry *logrus.Entry) {
	keys := make([]string, 0, len(RegisteredOptions))
	for k := range RegisteredOptions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := viper.GetStringSlice(k)
		if len(v) > 0 {
			entry.Infof("  --%s='%s'", k, strings.Join(v, ","))
		} else {
			entry.Infof("  --%s='%s'", k, viper.GetString(k))
		}
	}
}

// IpvlanConfig is the configuration used by Daemon when in ipvlan mode.
type IpvlanConfig struct {
	MasterDeviceIndex int
	OperationMode     string
}

// DaemonConfig is the configuration used by Daemon.
type DaemonConfig struct {
	CreationTime time.Time
	RunDir       string // CCE runtime directory
	HostV4Addr   net.IP // Host v4 address of the snooping device
	HostV6Addr   net.IP // Host v6 address of the snooping device
	DryMode      bool   // Do not create BPF maps, devices, ..

	IPAllocationTimeout time.Duration
	// AllowLocalhost defines when to allows the local stack to local endpoints
	// values: { auto | always | policy }
	AllowLocalhost string

	// StateDir is the directory where runtime state of endpoints is stored
	StateDir string

	SocketPath string

	// Options changeable at runtime
	Opts *IntOptions

	// Mutex for serializing configuration updates to the daemon.
	ConfigPatchMutex lock.RWMutex

	// Monitor contains the configuration for the node monitor.
	Monitor  *models.MonitorStatus
	GopsPort int
	// AgentHealthPort is the TCP port for agent health status API
	AgentHealthPort int

	// AgentLabels contains additional labels to identify this agent in monitor events.
	AgentLabels []string

	// IPv6ClusterAllocCIDR is the base CIDR used to allocate IPv6 node
	// CIDRs if allocation is not performed by an orchestration system
	IPv6ClusterAllocCIDR string

	// IPv6ClusterAllocCIDRBase is derived from IPv6ClusterAllocCIDR and
	// contains the CIDR without the mask, e.g. "fdfd::1/64" -> "fdfd::"
	//
	// This variable should never be written to, it is initialized via
	// DaemonConfig.Validate()
	IPv6ClusterAllocCIDRBase string

	// K8sRequireIPv4PodCIDR requires the k8s node resource to specify the
	// IPv4 PodCIDR. CCE will block bootstrapping until the information
	// is available.
	K8sRequireIPv4PodCIDR bool

	// K8sRequireIPv6PodCIDR requires the k8s node resource to specify the
	// IPv6 PodCIDR. CCE will block bootstrapping until the information
	// is available.
	K8sRequireIPv6PodCIDR bool

	// K8sServiceCacheSize is the service cache size for cce k8s package.
	K8sServiceCacheSize uint

	// MTU is the maximum transmission unit of the underlying network
	MTU int

	// EnableMonitor enables the monitor unix domain socket server
	EnableMonitor bool

	// MonitorAggregationInterval configures the interval between monitor
	// messages when monitor aggregation is enabled.
	MonitorAggregationInterval time.Duration

	// MonitorAggregationFlags determines which TCP flags that the monitor
	// aggregation ensures reports are generated for when monitor-aggragation
	// is enabled. Network byte-order.
	MonitorAggregationFlags uint16

	// DisableCCEEndpointCRD disables the use of CCEEndpoint CRD
	DisableCCEEndpointCRD bool

	// DisableENICRD disables the use of ENI CRD
	DisableENICRD bool

	// MaxControllerInterval is the maximum value for a controller's
	// RunInterval. Zero means unlimited.
	MaxControllerInterval int

	// UseSingleClusterRoute specifies whether to use a single cluster route
	// instead of per-node routes.
	UseSingleClusterRoute bool

	ProcFs string

	// K8sNamespace is the name of the namespace in which CCE is
	// deployed in when running in Kubernetes mode
	K8sNamespace string

	// AgentNotReadyNodeTaint is a node taint which prevents pods from being
	// scheduled. Once cce is setup it is removed from the node. Mostly
	// used in cloud providers to prevent existing CNI plugins from managing
	// pods.
	AgentNotReadyNodeTaintKey string

	// EnableIPv4 is true when IPv4 is enabled
	EnableIPv4 bool

	// EnableIPv6 is true when IPv6 is enabled
	EnableIPv6 bool

	// EnableIPv6NDP is true when NDP is enabled for IPv6
	EnableIPv6NDP bool

	// IPv6MCastDevice is the name of device that joins IPv6's solicitation multicast group
	IPv6MCastDevice string
	// MonitorQueueSize is the size of the monitor event queue
	MonitorQueueSize int

	ConfigFile                 string
	ConfigDir                  string
	Debug                      bool
	DebugVerbose               []string
	EnableTracing              bool
	IPv4Range                  string
	IPv6Range                  string
	K8sAPIServer               string
	K8sKubeConfigPath          string
	K8sClientBurst             int
	K8sClientQPSLimit          float64
	K8sHeartbeatTimeout        time.Duration
	K8sSyncTimeout             time.Duration
	K8sWatcherEndpointSelector string
	LogDriver                  []string
	LogOpt                     map[string]string
	Logstash                   bool
	LogSystemLoadConfig        bool

	TracePayloadlen     int
	Version             string
	PProf               bool
	PProfPort           int
	PrometheusServeAddr string

	// EnableAutoDirectRouting enables installation of direct routes to
	// other nodes when available
	EnableAutoDirectRouting bool
	EnableUnreachableRoutes bool

	// EnableLocalNodeRoute controls installation of the route which points
	// the allocation prefix of the local node.
	EnableLocalNodeRoute bool

	// EnableHealthChecking enables health checking between nodes and
	// health endpoints
	EnableHealthChecking bool

	// EnableEndpointHealthChecking enables health checking between virtual
	// health endpoints
	EnableEndpointHealthChecking bool

	// EnableHealthCheckNodePort enables health checking of NodePort by
	// cce
	EnableHealthCheckNodePort bool

	// EndpointGCInterval is interval to attempt garbage collection of
	// endpoints that are no longer alive and healthy.
	EndpointGCInterval time.Duration

	// ConntrackGCInterval is the connection tracking garbage collection
	// interval
	ConntrackGCInterval time.Duration

	// K8sEventHandover enables use of the kvstore to optimize Kubernetes
	// event handling by listening for k8s events in the operator and
	// mirroring it into the kvstore for reduced overhead in large
	// clusters.
	K8sEventHandover bool

	// MetricsConfig is the configuration set in metrics
	MetricsConfig metrics.Configuration

	// LoopbackIPv4 is the address to use for service loopback SNAT
	LoopbackIPv4 string

	// LocalRouterIPv4 is the link-local IPv4 address used for CCE's router device
	LocalRouterIPv4 string

	// LocalRouterIPv6 is the link-local IPv6 address used for CCE's router device
	LocalRouterIPv6 string

	// ForceLocalPolicyEvalAtSource forces a policy decision at the source
	// endpoint for all local communication
	ForceLocalPolicyEvalAtSource bool

	// EnableEndpointRoutes enables use of per endpoint routes
	EnableEndpointRoutes bool

	// Specifies wheather to annotate the kubernetes nodes or not
	AnnotateK8sNode bool

	// RunMonitorAgent indicates whether to run the monitor agent
	RunMonitorAgent bool

	// WriteCNIConfigurationWhenReady writes the CNI configuration to the
	// specified location once the agent is ready to serve requests. This
	// allows to keep a Kubernetes node NotReady until CCE is up and
	// running and able to schedule endpoints.
	WriteCNIConfigurationWhenReady string

	// EnableHealthDatapath enables IPIP health probes data path
	EnableHealthDatapath bool

	// KernelHz is the HZ rate the kernel is operating in
	KernelHz int

	// ExcludeLocalAddresses excludes certain addresses to be recognized as
	// a local address
	ExcludeLocalAddresses []*net.IPNet

	// IPv4PodSubnets available subnets to be assign IPv4 addresses to pods from
	IPv4PodSubnets []*net.IPNet

	// IPv6PodSubnets available subnets to be assign IPv6 addresses to pods from
	IPv6PodSubnets []*net.IPNet

	// IPAM is the IPAM method to use
	IPAM                    string
	IPPoolMinAllocateIPs    int
	IPPoolPreAllocate       int
	IPPoolMaxAboveWatermark int

	// AutoCreateNetResourceSetResource enables automatic creation of a
	// NetResourceSet resource for the local node
	AutoCreateNetResourceSetResource bool

	// IPv4NativeRoutingCIDR describes a CIDR in which pod IPs are routable
	IPv4NativeRoutingCIDR *cidr.CIDR

	// IPv6NativeRoutingCIDR describes a CIDR in which pod IPs are routable
	IPv6NativeRoutingCIDR *cidr.CIDR

	K8sEnableAPIDiscovery bool

	// k8sEnableLeasesFallbackDiscovery enables k8s to fallback to API probing to check
	// for the support of Leases in Kubernetes when there is an error in discovering
	// API groups using Discovery API.
	// We require to check for Leases capabilities in operator only, which uses Leases for leader
	// election purposes in HA mode.
	// This is only enabled for cce-operator
	K8sEnableLeasesFallbackDiscovery bool

	// APIRateLimitName enables configuration of the API rate limits
	APIRateLimit map[string]string

	// DefaultAPIBurst is the burst value allowed when accessing external Cloud APIs
	DefaultAPIBurst int

	// DefaultAPIQPSLimit is the queries per second limit when accessing external Cloud APIs
	DefaultAPIQPSLimit float64

	// DefaultAPITimeoutLimit is the timeout limit when accessing external Cloud APIs
	DefaultAPITimeoutLimit time.Duration

	// CRDWaitTimeout is the timeout in which CCE will exit if CRDs are not
	// available.
	CRDWaitTimeout time.Duration

	// CCEEndpointGC interval of single machine recycling endpoint
	CCEEndpointGC time.Duration

	// FixedIPTimeout Timeout for waiting for the fixed IP assignment to succeed
	FixedIPTimeout time.Duration

	// For BCE CCE
	// ResourceResyncInterval is the interval between attempts of the sync between Cloud and k8s
	// like ENIs,Subnets
	ResourceResyncInterval time.Duration

	// ClusterID is the unique identifier of the cluster
	ClusterID string
	// only use for vpc-eni mode
	ENI *bceapi.ENISpec

	// EnableBandwidthManager enables bandwidth manager
	EnableBandwidthManager   bool
	EnableEgressPriority     bool
	EnableEgressPriorityDSCP bool

	// ExtCNIPluginsList Expand the list of CNI plugins, such as 'sbr-eip'
	ExtCNIPluginsList []string
}

var (
	// Config represents the daemon configuration
	Config = &DaemonConfig{
		CreationTime:                     time.Now(),
		Opts:                             NewIntOptions(&DaemonOptionLibrary),
		Monitor:                          &models.MonitorStatus{Cpus: int64(runtime.NumCPU()), Npages: 64, Pagesize: int64(os.Getpagesize()), Lost: 0, Unknown: 0},
		IPv6ClusterAllocCIDR:             defaults.IPv6ClusterAllocCIDR,
		IPv6ClusterAllocCIDRBase:         defaults.IPv6ClusterAllocCIDRBase,
		EnableHealthChecking:             defaults.EnableHealthChecking,
		EnableEndpointHealthChecking:     defaults.EnableEndpointHealthChecking,
		EnableHealthCheckNodePort:        defaults.EnableHealthCheckNodePort,
		EnableIPv4:                       defaults.EnableIPv4,
		EnableIPv6:                       defaults.EnableIPv6,
		EnableIPv6NDP:                    defaults.EnableIPv6NDP,
		LogOpt:                           make(map[string]string),
		LoopbackIPv4:                     defaults.LoopbackIPv4,
		ForceLocalPolicyEvalAtSource:     defaults.ForceLocalPolicyEvalAtSource,
		EnableEndpointRoutes:             defaults.EnableEndpointRoutes,
		AnnotateK8sNode:                  defaults.AnnotateK8sNode,
		K8sServiceCacheSize:              defaults.K8sServiceCacheSize,
		AutoCreateNetResourceSetResource: defaults.AutoCreateNetResourceSetResource,
		K8sEnableAPIDiscovery:            defaults.K8sEnableAPIDiscovery,

		K8sEnableLeasesFallbackDiscovery: defaults.K8sEnableLeasesFallbackDiscovery,
		APIRateLimit:                     make(map[string]string),
		FixedIPTimeout:                   defaults.CCEEndpointGCInterval,
		CCEEndpointGC:                    defaults.CCEEndpointGCInterval,
	}
)

// GetIPv4NativeRoutingCIDR returns the native routing CIDR if configured
func (c *DaemonConfig) GetIPv4NativeRoutingCIDR() (cidr *cidr.CIDR) {
	c.ConfigPatchMutex.RLock()
	cidr = c.IPv4NativeRoutingCIDR
	c.ConfigPatchMutex.RUnlock()
	return
}

// SetIPv4NativeRoutingCIDR sets the native routing CIDR
func (c *DaemonConfig) SetIPv4NativeRoutingCIDR(cidr *cidr.CIDR) {
	c.ConfigPatchMutex.Lock()
	c.IPv4NativeRoutingCIDR = cidr
	c.ConfigPatchMutex.Unlock()
}

// GetIPv6NativeRoutingCIDR returns the native routing CIDR if configured
func (c *DaemonConfig) GetIPv6NativeRoutingCIDR() (cidr *cidr.CIDR) {
	c.ConfigPatchMutex.RLock()
	cidr = c.IPv6NativeRoutingCIDR
	c.ConfigPatchMutex.RUnlock()
	return
}

// SetIPv6NativeRoutingCIDR sets the native routing CIDR
func (c *DaemonConfig) SetIPv6NativeRoutingCIDR(cidr *cidr.CIDR) {
	c.ConfigPatchMutex.Lock()
	c.IPv6NativeRoutingCIDR = cidr
	c.ConfigPatchMutex.Unlock()
}

// IsExcludedLocalAddress returns true if the specified IP matches one of the
// excluded local IP ranges
func (c *DaemonConfig) IsExcludedLocalAddress(ip net.IP) bool {
	for _, ipnet := range c.ExcludeLocalAddresses {
		if ipnet.Contains(ip) {
			return true
		}
	}

	return false
}

// IsPodSubnetsDefined returns true if encryption subnets should be configured at init time.
func (c *DaemonConfig) IsPodSubnetsDefined() bool {
	return len(c.IPv4PodSubnets) > 0 || len(c.IPv6PodSubnets) > 0
}

// NodeConfigFile is the name of the C header which contains the node's
// network parameters.
const nodeConfigFile = "node_config.h"

// GetNodeConfigPath returns the full path of the NodeConfigFile.
func (c *DaemonConfig) GetNodeConfigPath() string {
	return filepath.Join(c.GetGlobalsDir(), nodeConfigFile)
}

// GetGlobalsDir returns the path for the globals directory.
func (c *DaemonConfig) GetGlobalsDir() string {
	return filepath.Join(c.StateDir, "globals")
}

// AlwaysAllowLocalhost returns true if the daemon has the option set that
// localhost can always reach local endpoints
func (c *DaemonConfig) AlwaysAllowLocalhost() bool {
	switch c.AllowLocalhost {
	case AllowLocalhostAlways:
		return true
	default:
		return false
	}
}

// IPv4Enabled returns true if IPv4 is enabled
func (c *DaemonConfig) IPv4Enabled() bool {
	return c.EnableIPv4
}

// IPv6Enabled returns true if IPv6 is enabled
func (c *DaemonConfig) IPv6Enabled() bool {
	return c.EnableIPv6
}

// IPv6NDPEnabled returns true if IPv6 NDP support is enabled
func (c *DaemonConfig) IPv6NDPEnabled() bool {
	return c.EnableIPv6NDP
}

// HealthCheckingEnabled returns true if health checking is enabled
func (c *DaemonConfig) HealthCheckingEnabled() bool {
	return c.EnableHealthChecking
}

// IPAMMode returns the IPAM mode
func (c *DaemonConfig) IPAMMode() string {
	return strings.ToLower(c.IPAM)
}

// TracingEnabled returns if tracing policy (outlining which rules apply to a
// specific set of labels) is enabled.
func (c *DaemonConfig) TracingEnabled() bool {
	return c.Opts.IsEnabled(PolicyTracing)
}

// UnreachableRoutesEnabled returns true if unreachable routes is enabled
func (c *DaemonConfig) UnreachableRoutesEnabled() bool {
	return c.EnableUnreachableRoutes
}

// LocalClusterName returns the name of the cluster CCE is deployed in
func (c *DaemonConfig) LocalClusterName() string {
	return c.ClusterID
}

// CCENamespaceName returns the name of the namespace in which CCE is
// deployed in
func (c *DaemonConfig) CCENamespaceName() string {
	return c.K8sNamespace
}

// AgentNotReadyNodeTaintValue returns the value of the taint key that cce agents
// will manage on their nodes
func (c *DaemonConfig) AgentNotReadyNodeTaintValue() string {
	if c.AgentNotReadyNodeTaintKey != "" {
		return c.AgentNotReadyNodeTaintKey
	} else {
		return defaults.AgentNotReadyNodeTaint
	}
}

// K8sAPIDiscoveryEnabled returns true if API discovery of API groups and
// resources is enabled
func (c *DaemonConfig) K8sAPIDiscoveryEnabled() bool {
	return c.K8sEnableAPIDiscovery
}

// K8sLeasesFallbackDiscoveryEnabled returns true if we should fallback to direct API
// probing when checking for support of Leases in case Discovery API fails to discover
// required groups.
func (c *DaemonConfig) K8sLeasesFallbackDiscoveryEnabled() bool {
	return c.K8sEnableAPIDiscovery
}

// EnableK8sLeasesFallbackDiscovery enables using direct API probing as a fallback to check
// for the support of Leases when discovering API groups is not possible.
func (c *DaemonConfig) EnableK8sLeasesFallbackDiscovery() {
	c.K8sEnableAPIDiscovery = true
}

func (c *DaemonConfig) validateIPv6ClusterAllocCIDR() error {
	ip, cidr, err := net.ParseCIDR(c.IPv6ClusterAllocCIDR)
	if err != nil {
		return err
	}

	if cidr == nil {
		return fmt.Errorf("ParseCIDR returned nil")
	}

	if ones, _ := cidr.Mask.Size(); ones != 64 {
		return fmt.Errorf("CIDR length must be /64")
	}

	c.IPv6ClusterAllocCIDRBase = ip.Mask(cidr.Mask).String()

	return nil
}

// GetCCEEndpointGC interval of single machine recycling endpoint
func (c *DaemonConfig) GetCCEEndpointGC() time.Duration {
	return c.CCEEndpointGC
}

// GetFixedIPTimeout FixedIPTimeout Timeout for waiting for the fixed IP assignment to succeed
func (c *DaemonConfig) GetFixedIPTimeout() time.Duration {
	return c.FixedIPTimeout
}

// Validate validates the daemon configuration
func (c *DaemonConfig) Validate() error {
	if err := c.validateIPv6ClusterAllocCIDR(); err != nil {
		return fmt.Errorf("unable to parse CIDR value '%s' of option --%s: %s",
			c.IPv6ClusterAllocCIDR, IPv6ClusterAllocCIDRName, err)
	}

	if c.MTU < 0 {
		return fmt.Errorf("MTU '%d' cannot be negative", c.MTU)
	}

	if c.EnableIPv6NDP {
		if !c.EnableIPv6 {
			return fmt.Errorf("IPv6NDP cannot be enabled when IPv6 is not enabled")
		}
		if len(c.IPv6MCastDevice) == 0 && !MightAutoDetectDevices() {
			return fmt.Errorf("IPv6NDP cannot be enabled without %s", IPv6MCastDevice)
		}
	}

	if err := c.checkIPAMDelegatedPlugin(); err != nil {
		return err
	}

	return nil
}

// ReadDirConfig reads the given directory and returns a map that maps the
// filename to the contents of that file.
func ReadDirConfig(dirName string) (map[string]interface{}, error) {
	m := map[string]interface{}{}
	files, err := os.ReadDir(dirName)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("unable to read configuration directory: %s", err)
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		fName := filepath.Join(dirName, f.Name())

		// the file can still be a symlink to a directory
		if f.Type()&os.ModeSymlink == 0 {
			absFileName, err := filepath.EvalSymlinks(fName)
			if err != nil {
				log.WithError(err).Warnf("Unable to read configuration file %q", absFileName)
				continue
			}
			fName = absFileName
		}

		fi, err := os.Stat(fName)
		if err != nil {
			log.WithError(err).Warnf("Unable to read configuration file %q", fName)
			continue
		}
		if fi.Mode().IsDir() {
			continue
		}

		b, err := os.ReadFile(fName)
		if err != nil {
			log.WithError(err).Warnf("Unable to read configuration file %q", fName)
			continue
		}
		m[f.Name()] = string(bytes.TrimSpace(b))
	}
	return m, nil
}

// MergeConfig merges the given configuration map with viper's configuration.
func MergeConfig(m map[string]interface{}) error {
	err := viper.MergeConfigMap(m)
	if err != nil {
		return fmt.Errorf("unable to read merge directory configuration: %s", err)
	}
	return nil
}

// ReplaceDeprecatedFields replaces the deprecated options set with the new set
// of options that overwrite the deprecated ones.
// This function replaces the deprecated fields used by environment variables
// with a different name than the option they are setting. This also replaces
// the deprecated names used in the Kubernetes ConfigMap.
// Once we remove them from this function we also need to remove them from
// daemon_main.go and warn users about the old environment variable nor the
// option in the configuration map have any effect.
func ReplaceDeprecatedFields(m map[string]interface{}) {

}

func (c *DaemonConfig) parseExcludedLocalAddresses(s []string) error {
	for _, ipString := range s {
		_, ipnet, err := net.ParseCIDR(ipString)
		if err != nil {
			return fmt.Errorf("unable to parse excluded local address %s: %s", ipString, err)
		}

		c.ExcludeLocalAddresses = append(c.ExcludeLocalAddresses, ipnet)
	}

	return nil
}

// Populate sets all options with the values from viper
func (c *DaemonConfig) Populate() {
	c.GopsPort = viper.GetInt(GopsPort)
	c.AgentHealthPort = viper.GetInt(AgentHealthPort)
	c.AgentLabels = viper.GetStringSlice(AgentLabels)
	c.AllowLocalhost = viper.GetString(AllowLocalhost)
	c.AnnotateK8sNode = viper.GetBool(AnnotateK8sNode)
	c.AutoCreateNetResourceSetResource = viper.GetBool(AutoCreateNetResourceSetResource)
	c.ClusterID = viper.GetString(ClusterID)
	c.Debug = viper.GetBool(DebugArg)
	c.EnableUnreachableRoutes = viper.GetBool(EnableUnreachableRoutes)
	//c.DebugVerbose = viper.GetStringSlice(DebugVerbose)
	c.EnableIPv4 = viper.GetBool(EnableIPv4Name)
	c.EnableIPv6 = viper.GetBool(EnableIPv6Name)
	c.EnableIPv6NDP = viper.GetBool(EnableIPv6NDPName)
	c.IPv6MCastDevice = viper.GetString(IPv6MCastDevice)
	c.DisableCCEEndpointCRD = viper.GetBool(DisableCCEEndpointCRDName)
	c.DisableENICRD = viper.GetBool(DisableENICRDName)
	//c.EnableAutoDirectRouting = viper.GetBool(EnableAutoDirectRoutingName)
	c.EnableEndpointRoutes = viper.GetBool(EnableEndpointRoutes)
	c.EnableHealthChecking = viper.GetBool(EnableHealthChecking)
	//c.EnableEndpointHealthChecking = viper.GetBool(EnableEndpointHealthChecking)
	//c.EnableTracing = viper.GetBool(EnableTracing)
	c.IPAM = viper.GetString(IPAM)
	c.IPPoolMaxAboveWatermark = viper.GetInt(IPPoolMaxAboveWatermark)
	c.IPPoolMinAllocateIPs = viper.GetInt(IPPoolMinAllocateIPs)
	c.IPPoolPreAllocate = viper.GetInt(IPPoolPreAllocate)

	c.IPv4Range = viper.GetString(IPv4Range)
	c.IPv6ClusterAllocCIDR = viper.GetString(IPv6ClusterAllocCIDRName)
	c.IPv6Range = viper.GetString(IPv6Range)
	c.K8sAPIServer = viper.GetString(K8sAPIServer)
	c.K8sClientBurst = viper.GetInt(K8sClientBurst)
	c.K8sClientQPSLimit = viper.GetFloat64(K8sClientQPSLimit)
	c.K8sEnableAPIDiscovery = viper.GetBool(K8sEnableAPIDiscovery)
	c.K8sKubeConfigPath = viper.GetString(K8sKubeConfigPath)
	c.K8sRequireIPv4PodCIDR = viper.GetBool(K8sRequireIPv4PodCIDRName)
	c.K8sRequireIPv6PodCIDR = viper.GetBool(K8sRequireIPv6PodCIDRName)
	c.K8sServiceCacheSize = uint(viper.GetInt(K8sServiceCacheSize))
	c.K8sEventHandover = viper.GetBool(K8sEventHandover)
	c.K8sHeartbeatTimeout = viper.GetDuration(K8sHeartbeatTimeout)
	c.K8sSyncTimeout = viper.GetDuration(K8sSyncTimeoutName)
	c.K8sWatcherEndpointSelector = viper.GetString(K8sWatcherEndpointSelector)
	c.IPAllocationTimeout = viper.GetDuration(IPAllocationTimeout)
	c.LogDriver = viper.GetStringSlice(LogDriver)
	c.LogSystemLoadConfig = viper.GetBool(LogSystemLoadConfigName)
	c.Logstash = viper.GetBool(Logstash)
	c.LoopbackIPv4 = viper.GetString(LoopbackIPv4)
	c.LocalRouterIPv4 = viper.GetString(LocalRouterIPv4)
	c.LocalRouterIPv6 = viper.GetString(LocalRouterIPv6)
	c.EnableMonitor = viper.GetBool(EnableMonitorName)
	c.MonitorAggregationInterval = viper.GetDuration(MonitorAggregationInterval)
	c.MonitorQueueSize = viper.GetInt(MonitorQueueSizeName)
	c.SocketPath = viper.GetString(SocketPath)
	c.MTU = viper.GetInt(MTUName)
	c.PProf = viper.GetBool(PProf)
	c.PProfPort = viper.GetInt(PProfPort)
	c.ProcFs = viper.GetString(ProcFs)
	c.PrometheusServeAddr = viper.GetString(PrometheusServeAddr)
	c.RunDir = viper.GetString(StateDir)
	c.TracePayloadlen = viper.GetInt(TracePayloadlen)
	c.Version = viper.GetString(Version)
	c.WriteCNIConfigurationWhenReady = viper.GetString(WriteCNIConfigurationWhenReady)
	c.CRDWaitTimeout = viper.GetDuration(CRDWaitTimeout)
	c.EnableBandwidthManager = viper.GetBool(EnableBandwidthManager)
	c.EnableEgressPriority = viper.GetBool(EnableEgressPriority)
	c.EnableEgressPriorityDSCP = viper.GetBool(EnableEgressPriorityDSCP)

	// for cce
	c.CCEEndpointGC = viper.GetDuration(CCEEndpointGCInterval)
	c.FixedIPTimeout = viper.GetDuration(FixedIPTimeout)

	ipv4NativeRoutingCIDR := viper.GetString(IPv4NativeRoutingCIDR)

	if ipv4NativeRoutingCIDR != "" {
		c.IPv4NativeRoutingCIDR = cidr.MustParseCIDR(ipv4NativeRoutingCIDR)

		if len(c.IPv4NativeRoutingCIDR.IP) != net.IPv4len {
			log.Fatalf("%s must be an IPv4 CIDR", IPv4NativeRoutingCIDR)
		}
	}

	if c.EnableIPv4 && ipv4NativeRoutingCIDR == "" && c.EnableAutoDirectRouting {
		log.Warnf(",then you are recommended to also configure %s. If %s is not configured, this may lead to pod to pod traffic being masqueraded, "+
			"which can cause problems with performance, observability and policy", IPv4NativeRoutingCIDR, IPv4NativeRoutingCIDR)
	}

	ipv6NativeRoutingCIDR := viper.GetString(IPv6NativeRoutingCIDR)

	if ipv6NativeRoutingCIDR != "" {
		c.IPv6NativeRoutingCIDR = cidr.MustParseCIDR(ipv6NativeRoutingCIDR)

		if len(c.IPv6NativeRoutingCIDR.IP) != net.IPv6len {
			log.Fatalf("%s must be an IPv6 CIDR", IPv6NativeRoutingCIDR)
		}
	}

	if c.EnableIPv6 && ipv6NativeRoutingCIDR == "" && c.EnableAutoDirectRouting {
		log.Warnf("If %s is enabled, then you are recommended to also configure %s. If %s is not configured, this may lead to pod to pod traffic being masqueraded, "+
			"which can cause problems with performance, observability and policy", "EnableAutoDirectRoutingName", IPv6NativeRoutingCIDR, IPv6NativeRoutingCIDR)
	}

	// Convert IP strings into net.IPNet types
	subnets, invalid := ip.ParseCIDRs(viper.GetStringSlice(IPv4PodSubnets))
	if len(invalid) > 0 {
		log.WithFields(
			logrus.Fields{
				"Subnets": invalid,
			}).Warning("IPv4PodSubnets parameter can not be parsed.")
	}
	c.IPv4PodSubnets = subnets

	subnets, invalid = ip.ParseCIDRs(viper.GetStringSlice(IPv6PodSubnets))
	if len(invalid) > 0 {
		log.WithFields(
			logrus.Fields{
				"Subnets": invalid,
			}).Warning("IPv6PodSubnets parameter can not be parsed.")
	}
	c.IPv6PodSubnets = subnets

	monitorAggregationFlags := viper.GetStringSlice(MonitorAggregationFlags)
	var ctMonitorReportFlags uint16
	for i := 0; i < len(monitorAggregationFlags); i++ {
		value := strings.ToLower(monitorAggregationFlags[i])
		flag, exists := TCPFlags[value]
		if !exists {
			log.Fatalf("Unable to parse TCP flag %q for %s!",
				value, MonitorAggregationFlags)
		}
		ctMonitorReportFlags |= flag
	}
	c.MonitorAggregationFlags = ctMonitorReportFlags

	if m, err := command.GetStringMapStringE(viper.GetViper(), LogOpt); err != nil {
		log.Fatalf("unable to parse %s: %s", LogOpt, err)
	} else {
		c.LogOpt = m
	}

	// for API rate limit
	c.DefaultAPIBurst = viper.GetInt(DefaultAPIBurst)
	c.DefaultAPIQPSLimit = viper.GetFloat64(DefaultAPIQPSLimit)
	c.DefaultAPITimeoutLimit = viper.GetDuration(DefaultAPITimeoutLimit)
	if m, err := command.GetStringMapStringE(viper.GetViper(), APIRateLimitName); err != nil {
		log.Fatalf("unable to parse %s: %s", APIRateLimitName, err)
	} else {
		c.APIRateLimit = m
	}

	if c.MonitorQueueSize == 0 {
		c.MonitorQueueSize = getDefaultMonitorQueueSize(runtime.NumCPU())
	}

	// Metrics Setup
	defaultMetrics := metrics.DefaultMetrics()
	for _, metric := range viper.GetStringSlice(Metrics) {
		switch metric[0] {
		case '+':
			defaultMetrics[metric[1:]] = struct{}{}
		case '-':
			delete(defaultMetrics, metric[1:])
		}
	}
	var collectors []prometheus.Collector
	metricsSlice := common.MapStringStructToSlice(defaultMetrics)
	c.MetricsConfig, collectors = metrics.CreateConfiguration(metricsSlice)
	metrics.MustRegister(collectors...)

	if err := c.parseExcludedLocalAddresses(viper.GetStringSlice(ExcludeLocalAddress)); err != nil {
		log.WithError(err).Fatalf("Unable to parse excluded local addresses")
	}

	c.K8sEventHandover = false

	switch c.IPAM {
	case ipamOption.IPAMKubernetes, ipamOption.IPAMClusterPool, ipamOption.IPAMClusterPoolV2, ipamOption.IPAMVpcRoute:
		if c.EnableIPv4 {
			c.K8sRequireIPv4PodCIDR = true
		}

		if c.EnableIPv6 {
			c.K8sRequireIPv6PodCIDR = true
		}
	case ipamOption.IPAMVpcEni:
		c.ENI = &bceapi.ENISpec{
			UseMode:                     viper.GetString(ENIUseMode),
			MaxAllocateENI:              viper.GetInt(MaxAllocateENI),
			PreAllocateENI:              viper.GetInt(ENIPreAllocateENINum),
			MaxIPsPerENI:                viper.GetInt(MaxIPsPerENI),
			VpcID:                       viper.GetString(BCECloudVPCID),
			SecurityGroups:              viper.GetStringSlice(ENISecurityGroupIDs),
			EnterpriseSecurityGroupList: viper.GetStringSlice(ENIEnterpriseSecurityGroupIds),
			SubnetIDs:                   viper.GetStringSlice(ENISubnets),
			RouteTableOffset:            viper.GetInt(ENIRouteTableOffset),
			InstallSourceBasedRouting:   viper.GetBool(ENIInstallSourceBasedRouting),
		}
	}
	c.ConfigFile = viper.GetString(ConfigFile)
	c.K8sNamespace = viper.GetString(K8sNamespaceName)
	c.AgentNotReadyNodeTaintKey = viper.GetString(AgentNotReadyNodeTaintKeyName)
	c.MaxControllerInterval = viper.GetInt(MaxCtrlIntervalName)
	c.EndpointGCInterval = viper.GetDuration(EndpointGCInterval)
	c.ResourceResyncInterval = viper.GetDuration(ResourceResyncInterval)
	c.ExtCNIPluginsList = viper.GetStringSlice(ExtCNIPluginsList)
}

func (c *DaemonConfig) checkIPAMDelegatedPlugin() error {
	if c.IPAM == ipamOption.IPAMDelegatedPlugin {
		// When using IPAM delegated plugin, IP addresses are allocated by the CNI binary,
		// not the daemon. Therefore, features which require the daemon to allocate IPs for itself
		// must be disabled.
		if c.EnableIPv4 && c.LocalRouterIPv4 == "" {
			return fmt.Errorf("--%s must be provided when IPv4 is enabled with --%s=%s", LocalRouterIPv4, IPAM, ipamOption.IPAMDelegatedPlugin)
		}
		if c.EnableIPv6 && c.LocalRouterIPv6 == "" {
			return fmt.Errorf("--%s must be provided when IPv6 is enabled with --%s=%s", LocalRouterIPv6, IPAM, ipamOption.IPAMDelegatedPlugin)
		}
		if c.EnableEndpointHealthChecking {
			return fmt.Errorf("--%s must be disabled with --%s=%s", EnableEndpointHealthChecking, IPAM, ipamOption.IPAMDelegatedPlugin)
		}
	}
	return nil
}

// StoreInFile stores the configuration in a the given directory under the file
// name 'daemon-config.json'. If this file already exists, it is renamed to
// 'daemon-config-1.json', if 'daemon-config-1.json' also exists,
// 'daemon-config-1.json' is renamed to 'daemon-config-2.json'
func (c *DaemonConfig) StoreInFile(dir string) error {
	backupFileNames := []string{
		"agent-runtime-config.json",
		"agent-runtime-config-1.json",
		"agent-runtime-config-2.json",
	}
	backupFiles(dir, backupFileNames)
	f, err := os.Create(backupFileNames[0])
	if err != nil {
		return err
	}
	defer f.Close()
	e := json.NewEncoder(f)
	e.SetIndent("", " ")
	return e.Encode(c)
}

// StoreViperInFile stores viper's configuration in a the given directory under
// the file name 'viper-config.yaml'. If this file already exists, it is renamed
// to 'viper-config-1.yaml', if 'viper-config-1.yaml' also exists,
// 'viper-config-1.yaml' is renamed to 'viper-config-2.yaml'
func StoreViperInFile(dir string) error {
	backupFileNames := []string{
		"viper-agent-config.yaml",
		"viper-agent-config-1.yaml",
		"viper-agent-config-2.yaml",
	}
	backupFiles(dir, backupFileNames)
	return viper.WriteConfigAs(backupFileNames[0])
}

func backupFiles(dir string, backupFilenames []string) {
	for i := len(backupFilenames) - 1; i > 0; i-- {
		newFileName := filepath.Join(dir, backupFilenames[i-1])
		oldestFilename := filepath.Join(dir, backupFilenames[i])
		if _, err := os.Stat(newFileName); os.IsNotExist(err) {
			continue
		}
		err := os.Rename(newFileName, oldestFilename)
		if err != nil {
			log.WithError(err).WithFields(logrus.Fields{
				"old-name": oldestFilename,
				"new-name": newFileName,
			}).Error("Unable to rename configuration files")
		}
	}
}

func sanitizeIntParam(paramName string, paramDefault int) int {
	intParam := viper.GetInt(paramName)
	if intParam <= 0 {
		if viper.IsSet(paramName) {
			log.WithFields(
				logrus.Fields{
					"parameter":    paramName,
					"defaultValue": paramDefault,
				}).Warning("user-provided parameter had value <= 0 , which is invalid ; setting to default")
		}
		return paramDefault
	}
	return intParam
}

// validateConfigmap checks whether the flag exists and validate the value of flag
func validateConfigmap(cmd *cobra.Command, m map[string]interface{}) (error, string) {
	// validate the config-map
	for key, value := range m {
		if val := fmt.Sprintf("%v", value); val != "" {
			flags := cmd.Flags()
			// check whether the flag exists
			if flag := flags.Lookup(key); flag != nil {
				// validate the value of flag
				if err := flag.Value.Set(val); err != nil {
					return err, key
				}
			}
		}
	}

	return nil, ""
}

// InitConfig reads in config file and ENV variables if set.
func InitConfig(cmd *cobra.Command, programName, configName string) func() {
	return func() {
		if viper.GetBool("version") {
			fmt.Printf("%s %s\n", programName, version.Version)
			os.Exit(0)
		}

		if viper.GetString(CMDRef) != "" {
			return
		}

		Config.ConfigFile = viper.GetString(ConfigFile) // enable ability to specify config file via flag
		Config.ConfigDir = viper.GetString(ConfigDir)
		viper.SetEnvPrefix("cce")

		if Config.ConfigDir != "" {
			if _, err := os.Stat(Config.ConfigDir); os.IsNotExist(err) {
				log.Fatalf("Non-existent configuration directory %s", Config.ConfigDir)
			}

			if m, err := ReadDirConfig(Config.ConfigDir); err != nil {
				log.WithError(err).Fatalf("Unable to read configuration directory %s", Config.ConfigDir)
			} else {
				// replace deprecated fields with new fields
				ReplaceDeprecatedFields(m)

				// validate the config-map
				if err, flag := validateConfigmap(cmd, m); err != nil {
					log.WithError(err).Fatal("Incorrect config-map flag " + flag)
				}

				if err := MergeConfig(m); err != nil {
					log.WithError(err).Fatal("Unable to merge configuration")
				}
			}
		}

		if Config.ConfigFile != "" {
			viper.SetConfigFile(Config.ConfigFile)
		} else {
			viper.SetConfigName(configName) // name of config file (without extension)
			viper.AddConfigPath("$HOME")    // adding home directory as first search path
		}

		// If a config file is found, read it in.
		if err := viper.ReadInConfig(); err == nil {
			log.WithField(logfields.Path, viper.ConfigFileUsed()).
				Info("Using config from file")
		} else if Config.ConfigFile != "" {
			log.WithField(logfields.Path, Config.ConfigFile).
				Fatal("Error reading config file")
		} else {
			log.WithError(err).Debug("Skipped reading configuration file")
		}
	}
}

func getDefaultMonitorQueueSize(numCPU int) int {
	monitorQueueSize := numCPU * defaults.MonitorQueueSizePerCPU
	if monitorQueueSize > defaults.MonitorQueueSizePerCPUMaximum {
		monitorQueueSize = defaults.MonitorQueueSizePerCPUMaximum
	}
	return monitorQueueSize
}

// MightAutoDetectDevices returns true if the device auto-detection might take
// place.
func MightAutoDetectDevices() bool {
	return false
}

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
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/server/restapi"
	"github.com/go-openapi/loads"
	gops "github.com/google/gops/agent"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/common"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/components"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/flowdebug"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/pidfile"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/pprof"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/sysctl"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/version"
)

const (
	// list of supported verbose debug groups
	argDebugVerboseFlow     = "flow"
	argDebugVerboseKvstore  = "kvstore"
	argDebugVerboseDatapath = "datapath"
	argDebugVerbosePolicy   = "policy"

	apiTimeout   = 60 * time.Second
	daemonSubsys = "daemon"

	cniUpdateControllerName = "cni-config-update"

	// fatalSleep is the duration CCE should sleep before existing in case
	// of a log.Fatal is issued or a CLI flag is specified but does not exist.
	fatalSleep = 2 * time.Second
)

var (
	log = logging.DefaultLogger.WithField(logfields.LogSubsys, daemonSubsys)

	bootstrapTimestamp = time.Now()

	// RootCmd represents the base command when called without any subcommands
	RootCmd = &cobra.Command{
		Use:   "agent",
		Short: "Run the cce ipam v2 agent",
		Run: func(cmd *cobra.Command, args []string) {
			cmdRefDir := viper.GetString(option.CMDRef)
			if cmdRefDir != "" {
				genMarkdown(cmd, cmdRefDir)
				os.Exit(0)
			}

			// Open socket for using gops to get stacktraces of the agent.
			addr := fmt.Sprintf("127.0.0.1:%d", viper.GetInt(option.GopsPort))
			addrField := logrus.Fields{"address": addr}
			if err := gops.Listen(gops.Options{
				Addr:                   addr,
				ReuseSocketAddrAndPort: true,
			}); err != nil {
				log.WithError(err).WithFields(addrField).Fatal("Cannot start gops server")
			}
			log.WithFields(addrField).Info("Started gops server")

			bootstrapStats.earlyInit.Start()
			initEnv(cmd)
			bootstrapStats.earlyInit.End(true)
			runDaemon()
		},
	}

	bootstrapStats = bootstrapStatistics{}
)

// Execute sets up gops, installs the cleanup signal handler and invokes
// the root command. This function only returns when an interrupt
// signal has been received. This is intended to be called by main.main().
func Execute() {
	bootstrapStats.overall.Start()

	interruptCh := cleaner.registerSigHandler()
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	<-interruptCh
}

func skipInit(basePath string) bool {
	if strings.Contains(basePath, "agent") {
		return false
	}
	switch basePath {
	case components.CCEAgentName, components.CCEDaemonTestName:
		return false
	default:
		return true
	}
}

func init() {
	fmt.Println("start cce ipam v2")
	setupSleepBeforeFatal()
	initializeFlags()
	registerBootstrapMetrics()
}

func setupSleepBeforeFatal() {
	RootCmd.SetFlagErrorFunc(func(_ *cobra.Command, e error) error {
		time.Sleep(fatalSleep)
		return e
	})
	logrus.RegisterExitHandler(func() {
		time.Sleep(fatalSleep)
	},
	)
}

func initializeFlags() {
	if skipInit(path.Base(os.Args[0])) {
		log.Debug("Skipping preparation of cce-network-agent environment")
		return
	}

	cobra.OnInitialize(option.InitConfig(RootCmd, "agent", "cced"))

	// Reset the help function to also exit, as we block elsewhere in interrupts
	// and would not exit when called with -h.
	oldHelpFunc := RootCmd.HelpFunc()
	RootCmd.SetHelpFunc(func(c *cobra.Command, a []string) {
		oldHelpFunc(c, a)
		os.Exit(0)
	})

	flags := RootCmd.Flags()

	// Env bindings
	flags.Int(option.AgentHealthPort, defaults.AgentHealthPort, "TCP port for agent health status API")
	option.BindEnv(option.AgentHealthPort)

	flags.Int(option.ClusterHealthPort, defaults.ClusterHealthPort, "TCP port for cluster-wide network connectivity health API")
	option.BindEnv(option.ClusterHealthPort)

	flags.StringSlice(option.AgentLabels, []string{}, "Additional labels to identify this agent")
	option.BindEnv(option.AgentLabels)

	flags.String(option.AllowLocalhost, option.AllowLocalhostAuto, "Policy when to allow local stack to reach local endpoints { auto | always | policy }")
	option.BindEnv(option.AllowLocalhost)

	flags.Bool(option.AnnotateK8sNode, defaults.AnnotateK8sNode, "Annotate Kubernetes node")
	option.BindEnv(option.AnnotateK8sNode)

	flags.Bool(option.AutoCreateNetResourceSetResource, defaults.AutoCreateNetResourceSetResource, "Automatically create NetResourceSet resource for own node on startup")
	option.BindEnv(option.AutoCreateNetResourceSetResource)

	flags.String(option.ConfigFile, "", `Configuration file (default "$HOME/cced.yaml")`)
	option.BindEnv(option.ConfigFile)

	flags.String(option.ConfigDir, "", `Configuration directory that contains a file for each option`)
	option.BindEnv(option.ConfigDir)

	flags.BoolP(option.DebugArg, "D", false, "Enable debugging mode")
	option.BindEnv(option.DebugArg)

	flags.Bool(option.EnableEndpointRoutes, defaults.EnableEndpointRoutes, "Use per endpoint routes instead of routing via cce_host")
	option.BindEnv(option.EnableEndpointRoutes)

	flags.Bool(option.EnableHealthChecking, defaults.EnableHealthChecking, "Enable connectivity health checking")
	option.BindEnv(option.EnableHealthChecking)
	flags.Bool(option.EnableEndpointHealthChecking, defaults.EnableEndpointHealthChecking, "Enable connectivity health checking between virtual endpoints")
	option.BindEnv(option.EnableEndpointHealthChecking)
	flags.Bool(option.EnableIPv4Name, defaults.EnableIPv4, "Enable IPv4 support")
	option.BindEnv(option.EnableIPv4Name)

	flags.Bool(option.EnableIPv6Name, defaults.EnableIPv6, "Enable IPv6 support")
	option.BindEnv(option.EnableIPv6Name)

	flags.Bool(option.EnableIPv6NDPName, defaults.EnableIPv6NDP, "Enable IPv6 NDP support")
	option.BindEnv(option.EnableIPv6NDPName)

	flags.String(option.IPv6MCastDevice, "", "Device that joins a Solicited-Node multicast group for IPv6")
	option.BindEnv(option.IPv6MCastDevice)

	flags.StringSlice(option.IPv4PodSubnets, []string{}, "List of IPv4 pod subnets to preconfigure for encryption")
	option.BindEnv(option.IPv4PodSubnets)

	flags.StringSlice(option.IPv6PodSubnets, []string{}, "List of IPv6 pod subnets to preconfigure for encryption")
	option.BindEnv(option.IPv6PodSubnets)

	flags.String(option.EndpointInterfaceNamePrefix, "", "Prefix of interface name shared by all endpoints")
	option.BindEnv(option.EndpointInterfaceNamePrefix)
	flags.MarkDeprecated(option.EndpointInterfaceNamePrefix, "This option no longer has any effect and will be removed in v1.13.")

	flags.StringSlice(option.ExcludeLocalAddress, []string{}, "Exclude CIDR from being recognized as local address")
	option.BindEnv(option.ExcludeLocalAddress)

	flags.Bool(option.DisableCCEEndpointCRDName, false, "Disable use of CCEEndpoint CRD")
	option.BindEnv(option.DisableCCEEndpointCRDName)

	flags.Bool(option.DisableENICRDName, false, "Disable use of ENI CRD")
	option.BindEnv(option.DisableENICRDName)

	flags.Bool(option.K8sEnableAPIDiscovery, defaults.K8sEnableAPIDiscovery, "Enable discovery of Kubernetes API groups and resources with the discovery API")
	option.BindEnv(option.K8sEnableAPIDiscovery)

	flags.Bool(option.EnableUnreachableRoutes, false, "Add unreachable routes on pod deletion")
	option.BindEnv(option.EnableUnreachableRoutes)

	flags.String(option.IPAM, ipamOption.IPAMClusterPool, "Backend to use for IPAM")
	option.BindEnv(option.IPAM)

	flags.String(option.IPv4Range, AutoCIDR, "Per-node IPv4 endpoint prefix, e.g. 10.16.0.0/16")
	option.BindEnv(option.IPv4Range)

	flags.String(option.IPv6Range, AutoCIDR, "Per-node IPv6 endpoint prefix, e.g. fd02:1:1::/96")
	option.BindEnv(option.IPv6Range)

	flags.String(option.IPv6ClusterAllocCIDRName, defaults.IPv6ClusterAllocCIDR, "IPv6 /64 CIDR used to allocate per node endpoint /96 CIDR")
	option.BindEnv(option.IPv6ClusterAllocCIDRName)

	flags.String(option.IPv4ServiceRange, AutoCIDR, "Kubernetes IPv4 services CIDR if not inside cluster prefix")
	option.BindEnv(option.IPv4ServiceRange)

	flags.String(option.IPv6ServiceRange, AutoCIDR, "Kubernetes IPv6 services CIDR if not inside cluster prefix")
	option.BindEnv(option.IPv6ServiceRange)

	flags.Bool(option.K8sEventHandover, defaults.K8sEventHandover, "Enable k8s event handover to kvstore for improved scalability")
	option.BindEnv(option.K8sEventHandover)

	flags.String(option.K8sAPIServer, "", "Kubernetes API server URL")
	option.BindEnv(option.K8sAPIServer)

	flags.String(option.K8sKubeConfigPath, "", "Absolute path of the kubernetes kubeconfig file")
	option.BindEnv(option.K8sKubeConfigPath)

	flags.String(option.K8sNamespaceName, "", "Name of the Kubernetes namespace in which CCE is deployed in")
	option.BindEnv(option.K8sNamespaceName)

	flags.String(option.AgentNotReadyNodeTaintKeyName, defaults.AgentNotReadyNodeTaint, "Key of the taint indicating that CCE is not ready on the node")
	option.BindEnv(option.AgentNotReadyNodeTaintKeyName)

	flags.Bool(option.K8sRequireIPv4PodCIDRName, false, "Require IPv4 PodCIDR to be specified in node resource")
	option.BindEnv(option.K8sRequireIPv4PodCIDRName)

	flags.Bool(option.K8sRequireIPv6PodCIDRName, false, "Require IPv6 PodCIDR to be specified in node resource")
	option.BindEnv(option.K8sRequireIPv6PodCIDRName)

	flags.Uint(option.K8sServiceCacheSize, defaults.K8sServiceCacheSize, "CCE service cache size for kubernetes")
	option.BindEnv(option.K8sServiceCacheSize)
	flags.MarkHidden(option.K8sServiceCacheSize)

	flags.String(option.K8sWatcherEndpointSelector, defaults.K8sWatcherEndpointSelector, "K8s endpoint watcher will watch for these k8s endpoints")
	option.BindEnv(option.K8sWatcherEndpointSelector)

	flags.Duration(option.IPAllocationTimeout, defaults.IPAllocationTimeout, "Time after which an incomplete CIDR allocation is considered failed")
	option.BindEnv(option.IPAllocationTimeout)

	flags.Duration(option.K8sSyncTimeoutName, defaults.K8sSyncTimeout, "Timeout after last K8s event for synchronizing k8s resources before exiting")
	flags.MarkHidden(option.K8sSyncTimeoutName)
	option.BindEnv(option.K8sSyncTimeoutName)

	flags.String(option.IPv4NativeRoutingCIDR, "", "Allows to explicitly specify the IPv4 CIDR for native routing. "+
		"When specified, CCE assumes networking for this CIDR is preconfigured and hands traffic destined for that range to the Linux network stack without applying any SNAT. "+
		"Generally speaking, specifying a native routing CIDR implies that CCE can depend on the underlying networking stack to route packets to their destination. "+
		"To offer a concrete example, if CCE is configured to use direct routing and the Kubernetes CIDR is included in the native routing CIDR, the user must configure the routes to reach pods, either manually or by setting the auto-direct-node-routes flag.")
	option.BindEnv(option.IPv4NativeRoutingCIDR)

	flags.String(option.IPv6NativeRoutingCIDR, "", "Allows to explicitly specify the IPv6 CIDR for native routing. "+
		"When specified, CCE assumes networking for this CIDR is preconfigured and hands traffic destined for that range to the Linux network stack without applying any SNAT. "+
		"Generally speaking, specifying a native routing CIDR implies that CCE can depend on the underlying networking stack to route packets to their destination. "+
		"To offer a concrete example, if CCE is configured to use direct routing and the Kubernetes CIDR is included in the native routing CIDR, the user must configure the routes to reach pods, either manually or by setting the auto-direct-node-routes flag.")
	option.BindEnv(option.IPv6NativeRoutingCIDR)

	flags.String(option.LibDir, defaults.LibraryPath, "Directory path to store runtime build environment")
	option.BindEnv(option.LibDir)

	flags.StringSlice(option.LogDriver, []string{}, "Logging endpoints to use for example syslog")
	option.BindEnv(option.LogDriver)

	flags.Bool(option.LogSystemLoadConfigName, false, "Enable periodic logging of system load")
	option.BindEnv(option.LogSystemLoadConfigName)

	flags.Var(option.NewNamedMapOptions(option.LogOpt, &option.Config.LogOpt, nil),
		option.LogOpt, `Log driver options for agent, `+
			`configmap example for syslog driver: {"syslog.level":"info","syslog.facility":"local5","syslog.tag":"cce-network-agent"}`)
	flags.MarkHidden(option.LogOpt)
	option.BindEnv(option.LogOpt)

	flags.String(option.LoopbackIPv4, defaults.LoopbackIPv4, "IPv4 address for service loopback SNAT")
	option.BindEnv(option.LoopbackIPv4)

	flags.Int(option.MaxCtrlIntervalName, 0, "Maximum interval (in seconds) between controller runs. Zero is no limit.")
	flags.MarkHidden(option.MaxCtrlIntervalName)
	option.BindEnv(option.MaxCtrlIntervalName)

	flags.StringSlice(option.Metrics, []string{}, "Metrics that should be enabled or disabled from the default metric list. (+metric_foo to enable metric_foo , -metric_bar to disable metric_bar)")
	option.BindEnv(option.Metrics)

	flags.Bool(option.EnableMonitorName, true, "Enable the monitor unix domain socket server")
	option.BindEnv(option.EnableMonitorName)

	flags.Int(option.MonitorQueueSizeName, 0, "Size of the event queue when reading monitor events")
	option.BindEnv(option.MonitorQueueSizeName)

	flags.Int(option.MTUName, 0, "Overwrite auto-detected MTU of underlying network")
	option.BindEnv(option.MTUName)

	flags.String(option.ProcFs, "/proc", "Root's proc filesystem path")
	option.BindEnv(option.ProcFs)

	flags.String(option.IPv6NodeAddr, "auto", "IPv6 address of node")
	option.BindEnv(option.IPv6NodeAddr)

	flags.String(option.IPv4NodeAddr, "auto", "IPv4 address of node")
	option.BindEnv(option.IPv4NodeAddr)

	flags.String(option.ReadCNIConfiguration, "", "Read to the CNI configuration at specified path to extract per node configuration")
	option.BindEnv(option.ReadCNIConfiguration)

	flags.Bool(option.Restore, true, "Restores state, if possible, from previous daemon")
	option.BindEnv(option.Restore)

	flags.Bool(option.SingleClusterRouteName, false,
		"Use a single cluster route instead of per node routes")
	option.BindEnv(option.SingleClusterRouteName)

	flags.String(option.SocketPath, defaults.SockPath, "Sets daemon's socket path to listen for connections")
	option.BindEnv(option.SocketPath)

	flags.String(option.StateDir, defaults.RuntimePath, "Directory path to store runtime state")
	option.BindEnv(option.StateDir)

	flags.Int(option.TracePayloadlen, 128, "Length of payload to capture when tracing")
	option.BindEnv(option.TracePayloadlen)

	flags.Bool(option.Version, false, "Print version information")
	option.BindEnv(option.Version)

	flags.Bool(option.PProf, false, "Enable serving the pprof debugging API")
	option.BindEnv(option.PProf)

	flags.Int(option.PProfPort, 6060, "Port that the pprof listens on")
	option.BindEnv(option.PProfPort)

	// We expect only one of the possible variables to be filled. The evaluation order is:
	// --prometheus-serve-addr, CILIUM_PROMETHEUS_SERVE_ADDR, then PROMETHEUS_SERVE_ADDR
	// The second environment variable (without the CILIUM_ prefix) is here to
	// handle the case where someone uses a new image with an older spec, and the
	// older spec used the older variable name.
	flags.String(option.PrometheusServeAddr, ":9962", "IP:Port on which to serve prometheus metrics (pass \":Port\" to bind on all interfaces, \"\" is off)")
	option.BindEnvWithLegacyEnvFallback(option.PrometheusServeAddr, "PROMETHEUS_SERVE_ADDR")

	flags.Duration(option.MonitorAggregationInterval, 5*time.Second, "Monitor report interval when monitor aggregation is enabled")
	option.BindEnv(option.MonitorAggregationInterval)

	flags.String(option.CMDRef, "", "Path to cmdref output directory")
	flags.MarkHidden(option.CMDRef)
	option.BindEnv(option.CMDRef)

	flags.Var(option.NewNamedMapOptions(option.APIRateLimitName, &option.Config.APIRateLimit, nil),
		option.APIRateLimitName, `API rate limiting configuration (example: --rate-limit endpoint-create=rate-limit:10/m,rate-burst:2)`)
	option.BindEnv(option.APIRateLimitName)
	flags.MarkHidden(option.APIRateLimitName)

	flags.Int(option.DefaultAPIBurst, defaults.CloudAPIBurst, "Upper burst limit when accessing external APIs")
	flags.MarkHidden(option.CMDRef)
	option.BindEnv(option.DefaultAPIBurst)

	flags.Float64(option.DefaultAPIQPSLimit, defaults.CloudAPIQPSLimit, "Queries per second limit when accessing external APIs")
	flags.MarkHidden(option.CMDRef)
	option.BindEnv(option.DefaultAPIQPSLimit)

	flags.Duration(option.DefaultAPITimeoutLimit, defaults.ClientConnectTimeout, "Timeout limit when accessing external APIs")
	flags.MarkHidden(option.CMDRef)
	option.BindEnv(option.DefaultAPITimeoutLimit)

	flags.Duration(option.EndpointGCInterval, 5*time.Minute, "Periodically monitor local endpoint health via link status on this interval and garbage collect them if they become unhealthy, set to 0 to disable")
	flags.MarkHidden(option.EndpointGCInterval)
	option.BindEnv(option.EndpointGCInterval)

	flags.String(option.WriteCNIConfigurationWhenReady, "", fmt.Sprintf("Write the CNI configuration as specified via --%s to path when agent is ready", option.ReadCNIConfiguration))
	option.BindEnv(option.WriteCNIConfigurationWhenReady)

	flags.Duration(option.K8sHeartbeatTimeout, 30*time.Second, "Configures the timeout for api-server heartbeat, set to 0 to disable")
	option.BindEnv(option.K8sHeartbeatTimeout)

	flags.Float32(option.K8sClientQPSLimit, defaults.K8sClientQPSLimit, "Queries per second limit for the K8s client")
	option.BindEnv(option.K8sClientQPSLimit)
	flags.Int(option.K8sClientBurst, defaults.K8sClientBurst, "Burst value allowed for the K8s client")
	option.BindEnv(option.K8sClientBurst)

	flags.String(option.LocalRouterIPv4, "", "Link-local IPv4 used for CCE's router devices")
	option.BindEnv(option.LocalRouterIPv4)

	flags.String(option.LocalRouterIPv6, "", "Link-local IPv6 used for CCE's router devices")
	option.BindEnv(option.LocalRouterIPv6)

	flags.Duration(option.CRDWaitTimeout, 5*time.Minute, "CCE will exit if CRDs are not available within this duration upon startup")
	option.BindEnv(option.CRDWaitTimeout)

	// for cce endpoint
	flags.Duration(option.CCEEndpointGCInterval, defaults.CCEEndpointGCInterval, "interval of single machine recycling endpoint")
	option.BindEnv(option.CCEEndpointGCInterval)

	flags.Duration(option.FixedIPTimeout, defaults.CCEEndpointGCInterval, "Timeout for waiting for the fixed IP assignment to succeed")
	option.BindEnv(option.FixedIPTimeout)

	flags.Bool(option.EnableCCEEndpointSlice, false, "If set to true, CCEEndpointSlice feature is enabled and cce agent watch for CCEEndpointSlice instead of CCEEndpoint to update the IPCache.")
	option.BindEnv(option.EnableCCEEndpointSlice)

	flags.String(option.ClusterID, "", "cluster ID of CCE")
	option.BindEnv(option.ClusterID)

	flags.Duration(option.ResourceResyncInterval, defaults.CCEEndpointGCInterval, "resync interval for cloud resource, eg. subnets, ENIs.")
	option.BindEnv(option.ResourceResyncInterval)
	// eni configuration this flags only use for CCE VPC ENI
	// this flags only use for vpc-eni mode
	flags.String(option.ENIUseMode, string(ccev2.ENIUseModeSecondaryIP), "eni use mode")
	option.BindEnv(option.ENIUseMode)

	flags.Bool(option.ENIInstallSourceBasedRouting, true, "install source based routing to eni device")
	option.BindEnv(option.ENIInstallSourceBasedRouting)

	flags.Int(option.ENIPreAllocateENINum, 1, "pre allocate eni count")
	option.BindEnv(option.ENIPreAllocateENINum)

	flags.Int(option.MaxAllocateENI, 0, "max ENIs can be allocated to a BCC instance, 0 means no limit")
	option.BindEnv(option.MaxAllocateENI)

	flags.Int(option.MaxIPsPerENI, 0, "max IPs can be allocated to a ENI , 0 means no limit")
	option.BindEnv(option.MaxIPsPerENI)

	flags.StringSlice(option.ENISubnets, []string{}, "subnet ids")
	option.BindEnv(option.ENISubnets)

	flags.Int(option.ENIRouteTableOffset, 127, "route table offset")
	option.BindEnv(option.ENIRouteTableOffset)

	flags.StringSlice(option.ENISecurityGroupIDs, []string{}, "security group ids")
	option.BindEnv(option.ENISecurityGroupIDs)

	flags.StringSlice(option.ENIEnterpriseSecurityGroupIds, []string{}, "enterprise security group ids")
	option.BindEnv(option.ENIEnterpriseSecurityGroupIds)

	viper.BindPFlags(flags)
}

func initEnv(cmd *cobra.Command) {
	var debugDatapath bool

	// Prepopulate option.Config with options from CLI.
	option.Config.Populate()

	// add hooks after setting up metrics in the option.Config
	logging.DefaultLogger.Hooks.Add(metrics.NewLoggingHook(components.CCEAgentName))

	// Logging should always be bootstrapped first. Do not add any code above this!
	if err := logging.SetupLogging(option.Config.LogDriver, logging.LogOptions(option.Config.LogOpt), "cce-network-agent", option.Config.Debug); err != nil {
		log.Fatal(err)
	}

	option.LogRegisteredOptions(log)

	sysctl.SetProcfs(option.Config.ProcFs)

	// Configure k8s as soon as possible so that k8s.IsEnabled() has the right
	// behavior.
	bootstrapStats.k8sInit.Start()
	k8s.Configure(option.Config.K8sAPIServer, option.Config.K8sKubeConfigPath, float32(option.Config.K8sClientQPSLimit), option.Config.K8sClientBurst)
	bootstrapStats.k8sInit.End(true)

	for _, grp := range option.Config.DebugVerbose {
		switch grp {
		case argDebugVerboseFlow:
			log.Debugf("Enabling flow debug")
			flowdebug.Enable()
		case argDebugVerboseKvstore:
			//kvstore.EnableTracing()
		case argDebugVerboseDatapath:
			log.Debugf("Enabling datapath debug messages")
			debugDatapath = true
		case argDebugVerbosePolicy:
			option.Config.Opts.SetBool(option.DebugPolicy, true)
		default:
			log.Warningf("Unknown verbose debug group: %s", grp)
		}
	}
	common.RequireRootPrivilege("cce-network-v2-agent")

	log.Info("     _ _ _")
	log.Info(" ___|_| |_|_ _ _____")
	log.Info("|  _| | | | | |     |")
	log.Info("|___|_|_|_|___|_|_|_|")
	log.Infof("cce-network-v2 %s", version.Version)

	if option.Config.PProf {
		pprof.Enable(option.Config.PProfPort)
	}

	scopedLog := log.WithFields(logrus.Fields{
		logfields.Path + ".RunDir": option.Config.RunDir,
	})

	if option.Config.RunDir != defaults.RuntimePath {
		if err := os.MkdirAll(defaults.RuntimePath, defaults.RuntimePathRights); err != nil {
			scopedLog.WithError(err).Fatal("Could not create default runtime directory")
		}
	}

	option.Config.StateDir = filepath.Join(option.Config.RunDir, defaults.StateDir)
	scopedLog = scopedLog.WithField(logfields.Path+".StateDir", option.Config.StateDir)
	if err := os.MkdirAll(option.Config.StateDir, defaults.StateDirRights); err != nil {
		scopedLog.WithError(err).Fatal("Could not create state directory")
	}

	if option.Config.MaxControllerInterval < 0 {
		scopedLog.Fatalf("Invalid %s value %d", option.MaxCtrlIntervalName, option.Config.MaxControllerInterval)
	}

	if err := pidfile.Write(defaults.PidFilePath); err != nil {
		log.WithField(logfields.Path, defaults.PidFilePath).WithError(err).Fatal("Failed to create Pidfile")
	}

	option.Config.AllowLocalhost = strings.ToLower(option.Config.AllowLocalhost)
	scopedLog = log.WithField(logfields.Path, option.Config.SocketPath)
	socketDir := path.Dir(option.Config.SocketPath)
	if err := os.MkdirAll(socketDir, defaults.RuntimePathRights); err != nil {
		scopedLog.WithError(err).Fatal("Cannot mkdir directory for cce socket")
	}

	if err := os.Remove(option.Config.SocketPath); !os.IsNotExist(err) && err != nil {
		scopedLog.WithError(err).Fatal("Cannot remove existing CCE sock")
	}

	option.Config.Opts.SetBool(option.Debug, debugDatapath)
	option.Config.Opts.SetBool(option.DebugLB, debugDatapath)
	option.Config.Opts.SetBool(option.DropNotify, true)
	option.Config.Opts.SetBool(option.TraceNotify, true)
	option.Config.Opts.SetBool(option.ConntrackAccounting, true)
	option.Config.Opts.SetBool(option.ConntrackLocal, false)

	monitorAggregationLevel, err := option.ParseMonitorAggregationLevel("")
	if err != nil {
		log.WithError(err).Fatalf("Failed to parse %s", "")
	}
	option.Config.Opts.SetValidated(option.MonitorAggregation, monitorAggregationLevel)

	if !option.Config.EnableIPv4 && !option.Config.EnableIPv6 {
		log.Fatal("Either IPv4 or IPv6 addressing must be enabled")
	}
	// If there is one device specified, use it to derive better default
	// allocation prefixes
	node.InitDefaultPrefix("")
}

func runDaemon() {

	log.Info("Initializing daemon")

	option.Config.RunMonitorAgent = true

	if err := enableIPForwarding(); err != nil {
		log.WithError(err).Fatal("Error when enabling sysctl parameters")
	}

	if k8s.IsEnabled() {
		bootstrapStats.k8sInit.Start()
		if err := k8s.Init(option.Config); err != nil {
			log.WithError(err).Fatal("Unable to initialize Kubernetes subsystem")
		}
		bootstrapStats.k8sInit.End(true)
	}

	ctx, cancel := context.WithCancel(server.ServerCtx)
	d, err := NewDaemon(ctx, cancel)
	if err != nil {
		select {
		case <-server.ServerCtx.Done():
			log.WithError(err).Debug("Error while creating daemon")
		default:
			log.WithError(err).Fatal("Error while creating daemon")
		}
		return
	}

	if option.Config.K8sRequireIPv4PodCIDR || option.Config.K8sRequireIPv6PodCIDR {
		// This validation needs to be done outside of the agent until
		// datapath.NodeAddressing is used consistently across the code base.
		log.Info("Validating configured node address ranges")
		if err := node.ValidatePostInit(); err != nil {
			log.WithError(err).Fatal("postinit failed")
		}
	}

	bootstrapStats.k8sInit.Start()
	if k8s.IsEnabled() {
		// Wait only for certain caches, but not all!
		// (Check Daemon.InitK8sSubsystem() for more info)
		<-d.k8sCachesSynced
	}
	bootstrapStats.k8sInit.End(true)

	d.startStatusCollector()

	metricsErrs := initMetrics()

	bootstrapStats.initAPI.Start()
	srv := server.NewServer(d.instantiateAPI())
	srv.EnabledListeners = []string{"unix", "http"}
	srv.SocketPath = option.Config.SocketPath
	srv.ReadTimeout = apiTimeout
	srv.WriteTimeout = apiTimeout
	srv.Host = net.IPv4zero.String()
	srv.Port = option.Config.AgentHealthPort
	defer srv.Shutdown()

	srv.ConfigureAPI()
	bootstrapStats.initAPI.End(true)

	log.WithField("bootstrapTime", time.Since(bootstrapTimestamp)).
		Info("Daemon initialization completed")

	if option.Config.WriteCNIConfigurationWhenReady != "" {
		d.controllers.UpdateController(cniUpdateControllerName, controller.ControllerParams{
			RunInterval: option.Config.ResourceResyncInterval,
			DoFunc: func(ctx context.Context) error {
				_, err = os.Open(option.Config.WriteCNIConfigurationWhenReady)
				if err == nil {
					return nil
				}
				input, err := os.ReadFile(option.Config.ReadCNIConfiguration)
				if err != nil {
					log.WithError(err).Fatal("Unable to read CNI configuration file")
				}

				if err = os.WriteFile(option.Config.WriteCNIConfigurationWhenReady, input, 0644); err != nil {
					log.WithError(err).Fatalf("Unable to write CNI configuration file to %s", option.Config.WriteCNIConfigurationWhenReady)
				} else {
					log.Infof("Wrote CNI configuration file to %s", option.Config.WriteCNIConfigurationWhenReady)
				}

				return nil
			},
		})
		d.controllers.TriggerController(cniUpdateControllerName)
	}

	errs := make(chan error, 1)

	go func() {
		errs <- srv.Serve()
	}()

	bootstrapStats.overall.End(true)
	bootstrapStats.updateMetrics()

	err = option.Config.StoreInFile(option.Config.StateDir)
	if err != nil {
		log.WithError(err).Error("Unable to store CCE's configuration")
	}

	err = option.StoreViperInFile(option.Config.StateDir)
	if err != nil {
		log.WithError(err).Error("Unable to store Viper's configuration")
	}

	select {
	case err := <-metricsErrs:
		if err != nil {
			log.WithError(err).Fatal("Cannot start metrics server")
		}
	case err := <-errs:
		if err != nil {
			log.WithError(err).Fatal("Error returned from non-returning Serve() call")
		}
	}
}

func (d *Daemon) instantiateAPI() *restapi.CceAPIAPI {
	swaggerSpec, err := loads.Analyzed(server.SwaggerJSON, "")
	if err != nil {
		log.WithError(err).Fatal("Cannot load swagger spec")
	}

	log.Info("Initializing CCE API")
	restAPI := restapi.NewCceAPIAPI(swaggerSpec)

	restAPI.Logger = log.Infof

	restAPI.DaemonGetHealthzHandler = &daemonHealth{}

	// /ipam/{ip}/
	restAPI.IpamPostIpamHandler = NewPostIPAMHandler(d)
	restAPI.IpamPostIpamIPHandler = NewPostIPAMIPHandler(d)
	restAPI.IpamDeleteIpamIPHandler = NewDeleteIPAMIPHandler(d)

	// /eni
	restAPI.EniPostEniHandler = NewPostEniHandler(d)
	restAPI.EniDeleteEniHandler = NewDeleteENIHandler(d)

	// metrics
	restAPI.MetricsGetMetricsHandler = NewGetMetricsHandler(d)

	// endpoints
	restAPI.EndpointGetEndpointExtpluginStatusHandler = NewGetEndpointExtpluginStatusHandler(d)
	return restAPI
}

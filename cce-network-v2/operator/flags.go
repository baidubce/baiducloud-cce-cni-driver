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

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	pkgOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

func init() {
	cobra.OnInitialize(option.InitConfig(rootCmd, "operator", "cced"))

	flags := rootCmd.Flags()

	flags.Int(operatorOption.DefaultAPIBurst, defaults.CloudAPIBurst, "Upper burst limit when accessing external APIs")
	option.BindEnv(operatorOption.DefaultAPIBurst)

	flags.Float64(operatorOption.DefaultAPIQPSLimit, defaults.CloudAPIQPSLimit, "Queries per second limit when accessing external APIs")
	option.BindEnv(operatorOption.DefaultAPIQPSLimit)

	flags.Duration(operatorOption.DefaultAPITimeoutLimit, defaults.ClientConnectTimeout, "Timeout limit when accessing external APIs")
	option.BindEnv(operatorOption.DefaultAPITimeoutLimit)

	flags.StringSliceVar(&operatorOption.Config.IPAMSubnetsIDs, operatorOption.IPAMSubnetsIDs, operatorOption.Config.IPAMSubnetsIDs,
		"Subnets IDs (separated by commas)")
	option.BindEnv(operatorOption.IPAMSubnetsIDs)

	flags.Int64(operatorOption.ParallelAllocWorkers, defaults.ParallelAllocWorkers, "Maximum number of parallel IPAM workers")
	option.BindEnv(operatorOption.ParallelAllocWorkers)

	// Operator-specific flags
	flags.String(option.ConfigFile, "", `Configuration file (default "$HOME/cced.yaml")`)
	option.BindEnv(option.ConfigFile)

	flags.String(option.ConfigDir, "", `Configuration directory that contains a file for each option`)
	option.BindEnv(option.ConfigDir)

	flags.Bool(option.K8sEventHandover, defaults.K8sEventHandover, "Enable k8s event handover to kvstore for improved scalability")
	option.BindEnv(option.K8sEventHandover)

	flags.Duration(operatorOption.CNPStatusUpdateInterval, 1*time.Second, "Interval between CNP status updates sent to the k8s-apiserver per-CNP")
	option.BindEnv(operatorOption.CNPStatusUpdateInterval)

	flags.BoolP(option.DebugArg, "D", false, "Enable debugging mode")
	option.BindEnv(option.DebugArg)

	// We need to obtain from CCE ConfigMap if these options are enabled
	// or disabled. These options are marked as hidden because having it
	// being printed by operator --help could confuse users.
	flags.Bool(option.DisableCCEEndpointCRDName, false, "")
	flags.MarkHidden(option.DisableCCEEndpointCRDName)
	option.BindEnv(option.DisableCCEEndpointCRDName)

	flags.Bool(option.DisableENICRDName, false, "disable ENI CRD definition")
	flags.MarkHidden(option.DisableENICRDName)
	option.BindEnv(option.DisableENICRDName)

	flags.Duration(operatorOption.EndpointGCInterval, operatorOption.EndpointGCIntervalDefault, "GC interval for cce endpoints")
	option.BindEnv(operatorOption.EndpointGCInterval)

	flags.Bool(operatorOption.EnableMetrics, false, "Enable Prometheus metrics")
	option.BindEnv(operatorOption.EnableMetrics)

	// Logging flags
	flags.StringSlice(option.LogDriver, option.Config.LogDriver, "Logging endpoints to use for example syslog")
	option.BindEnv(option.LogDriver)

	flags.Var(option.NewNamedMapOptions(option.LogOpt, &option.Config.LogOpt, nil),
		option.LogOpt, `Log driver options for operator, `+
			`configmap example for syslog driver: {"syslog.level":"info","syslog.facility":"local5"}`)
	flags.MarkHidden(option.LogOpt)
	option.BindEnv(option.LogOpt)

	var defaultIPAM string
	switch binaryName {
	case "operator":
		defaultIPAM = ipamOption.IPAMClusterPool
	case "cce-operator-generic":
		defaultIPAM = ipamOption.IPAMClusterPool
	case "cce-operator-pcb":
		defaultIPAM = ipamOption.IPAMPrivateCloudBase
	}

	flags.String(option.IPAM, defaultIPAM, "Backend to use for IPAM")
	option.BindEnv(option.IPAM)

	rootCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		ipamFlag := cmd.Flag(option.IPAM)
		if !ipamFlag.Changed {
			return nil
		}
		ipamFlagValue := ipamFlag.Value.String()

		recommendInstead := func() string {
			switch ipamFlagValue {
			case ipamOption.IPAMVpcEni:
				return "cce-operator-eni"
			case ipamOption.IPAMVpcRoute:
				return "cce-operator-vpc-route"
			case ipamOption.IPAMKubernetes, ipamOption.IPAMClusterPool, ipamOption.IPAMClusterPoolV2, ipamOption.IPAMCRD:
				return "cce-operator-generic"
			default:
				return ""
			}
		}

		unsupporterErr := func() error {
			errMsg := fmt.Sprintf("%s doesn't support --%s=%s", binaryName, option.IPAM, ipamFlagValue)
			if recommendation := recommendInstead(); recommendation != "" {
				return fmt.Errorf("%s (use %s)", errMsg, recommendation)
			}
			return fmt.Errorf(errMsg)
		}

		switch binaryName {
		case "cce-operator":
			if recommendation := recommendInstead(); recommendation != "" {
				log.Warnf("cce-operator will be deprecated in the future, for --%s=%s use %s as it has lower memory footprint", option.IPAM, ipamFlagValue, recommendation)
			}
		case "cce-operator-eni":
			if ipamFlagValue != ipamOption.IPAMVpcEni {
				return unsupporterErr()
			}
		case "cce-operator-generic":
			switch ipamFlagValue {
			case ipamOption.IPAMVpcEni, ipamOption.IPAMPrivateCloudBase:
				return unsupporterErr()
			}
		}

		return nil
	}

	flags.Bool(option.EnableIPv4Name, defaults.EnableIPv4, "Enable IPv4 support")
	option.BindEnv(option.EnableIPv4Name)

	flags.StringSlice(operatorOption.ClusterPoolIPv4CIDR, []string{},
		fmt.Sprintf("IPv4 CIDR Range for Pods in cluster. Requires '%s=%s|%s' and '%s=%s'",
			option.IPAM, ipamOption.IPAMClusterPool, ipamOption.IPAMClusterPoolV2,
			option.EnableIPv4Name, "true"))
	option.BindEnv(operatorOption.ClusterPoolIPv4CIDR)

	flags.Int(operatorOption.NodeCIDRMaskSizeIPv4, 24,
		fmt.Sprintf("Mask size for each IPv4 podCIDR per node. Requires '%s=%s|%s' and '%s=%s'",
			option.IPAM, ipamOption.IPAMClusterPool, ipamOption.IPAMClusterPoolV2,
			option.EnableIPv4Name, "true"))
	option.BindEnv(operatorOption.NodeCIDRMaskSizeIPv4)

	flags.Bool(option.EnableIPv6Name, defaults.EnableIPv6, "Enable IPv6 support")
	option.BindEnv(option.EnableIPv6Name)

	flags.StringSlice(operatorOption.ClusterPoolIPv6CIDR, []string{},
		fmt.Sprintf("IPv6 CIDR Range for Pods in cluster. Requires '%s=%s|%s' and '%s=%s'",
			option.IPAM, ipamOption.IPAMClusterPool, ipamOption.IPAMClusterPoolV2,
			option.EnableIPv6Name, "true"))
	option.BindEnv(operatorOption.ClusterPoolIPv6CIDR)

	flags.Int(operatorOption.NodeCIDRMaskSizeIPv6, 112,
		fmt.Sprintf("Mask size for each IPv6 podCIDR per node. Requires '%s=%s|%s' and '%s=%s'",
			option.IPAM, ipamOption.IPAMClusterPool, ipamOption.IPAMClusterPoolV2,
			option.EnableIPv6Name, "true"))
	option.BindEnv(operatorOption.NodeCIDRMaskSizeIPv6)
	flags.String(option.K8sAPIServer, "", "Kubernetes API server URL")
	option.BindEnv(option.K8sAPIServer)

	flags.Float32(option.K8sClientQPSLimit, defaults.K8sClientQPSLimit, "Queries per second limit for the K8s client")
	flags.Int(option.K8sClientBurst, defaults.K8sClientBurst, "Burst value allowed for the K8s client")

	flags.Bool(option.K8sEnableAPIDiscovery, defaults.K8sEnableAPIDiscovery, "Enable discovery of Kubernetes API groups and resources with the discovery API")
	option.BindEnv(option.K8sEnableAPIDiscovery)

	flags.String(option.K8sNamespaceName, "", "Name of the Kubernetes namespace in which CCE Operator is deployed in")
	option.BindEnv(option.K8sNamespaceName)

	flags.String(option.K8sKubeConfigPath, "", "Absolute path of the kubernetes kubeconfig file")
	option.BindEnv(option.K8sKubeConfigPath)

	flags.Duration(operatorOption.NodesGCInterval, 0*time.Second, "GC interval for NetResourceSets")
	option.BindEnv(operatorOption.NodesGCInterval)

	flags.String(operatorOption.OperatorPrometheusServeAddr, operatorOption.PrometheusServeAddr, "Address to serve Prometheus metrics")
	option.BindEnv(operatorOption.OperatorPrometheusServeAddr)

	flags.String(operatorOption.OperatorAPIServeAddr, "localhost:9234", "Address to serve API requests")
	option.BindEnv(operatorOption.OperatorAPIServeAddr)

	flags.Bool(operatorOption.PProf, false, "Enable pprof debugging endpoint")
	option.BindEnv(operatorOption.PProf)

	flags.Int(operatorOption.PProfPort, 6061, "Port that the pprof listens on")
	option.BindEnv(operatorOption.PProfPort)

	flags.Bool(operatorOption.SyncK8sServices, true, "Synchronize Kubernetes services to kvstore")
	option.BindEnv(operatorOption.SyncK8sServices)

	flags.Bool(operatorOption.SyncK8sNodes, true, "Synchronize Kubernetes nodes to kvstore and perform CNP GC")
	option.BindEnv(operatorOption.SyncK8sNodes)

	flags.Int(operatorOption.UnmanagedPodWatcherInterval, 15, "Interval to check for unmanaged kube-dns pods (0 to disable)")
	option.BindEnv(operatorOption.UnmanagedPodWatcherInterval)

	flags.Bool(option.Version, false, "Print version information")
	option.BindEnv(option.Version)

	flags.String(option.CMDRef, "", "Path to cmdref output directory")
	flags.MarkHidden(option.CMDRef)
	option.BindEnv(option.CMDRef)

	flags.Int(option.GopsPort, defaults.GopsPortOperator, "Port for gops server to listen on")
	option.BindEnv(option.GopsPort)

	flags.Duration(option.K8sHeartbeatTimeout, 30*time.Second, "Configures the timeout for api-server heartbeat, set to 0 to disable")
	option.BindEnv(option.K8sHeartbeatTimeout)

	flags.Duration(operatorOption.LeaderElectionLeaseDuration, 15*time.Second,
		"Duration that non-leader operator candidates will wait before forcing to acquire leadership")
	option.BindEnv(operatorOption.LeaderElectionLeaseDuration)

	flags.Duration(operatorOption.LeaderElectionRenewDeadline, 10*time.Second,
		"Duration that current acting master will retry refreshing leadership in before giving up the lock")
	option.BindEnv(operatorOption.LeaderElectionRenewDeadline)

	flags.Duration(operatorOption.LeaderElectionRetryPeriod, 2*time.Second,
		"Duration that LeaderElector clients should wait between retries of the actions")
	option.BindEnv(operatorOption.LeaderElectionRetryPeriod)

	flags.Bool(option.SkipCRDCreation, false, "When true, Kubernetes Custom Resource Definitions will not be created")
	option.BindEnv(option.SkipCRDCreation)

	flags.Bool(option.EnableCCEEndpointSlice, false, "If set to true, the CCEEndpointSlice feature is enabled. If any CCEEndpoints resources are created, updated, or deleted in the cluster, all those changes are broadcast as CCEEndpointSlice updates to all of the CCE agents.")
	option.BindEnv(option.EnableCCEEndpointSlice)

	flags.String(operatorOption.CCEK8sNamespace, "kube-system", fmt.Sprintf("Name of the Kubernetes namespace in which CCE is deployed in. Defaults to the same namespace defined in %s", option.K8sNamespaceName))
	option.BindEnv(operatorOption.CCEK8sNamespace)

	flags.String(operatorOption.CCEPodLabels, "app.cce.baidubce.com=cce-cni-v2-agent", "CCE Pod's labels. Used to detect if a CCE pod is running to remove the node taints where its running and set NetworkUnavailable to false")
	option.BindEnv(operatorOption.CCEPodLabels)

	flags.Bool(operatorOption.RemoveNetResourceSetTaints, true, fmt.Sprintf("Remove node taint %q from Kubernetes nodes once CCE is up and running", pkgOption.Config.AgentNotReadyNodeTaintValue()))
	option.BindEnv(operatorOption.RemoveNetResourceSetTaints)

	flags.Bool(operatorOption.SetCCEIsUpCondition, true, "Set CCEIsUp Node condition to mark a Kubernetes Node that a CCE pod is up and running in that node")
	option.BindEnv(operatorOption.SetCCEIsUpCondition)

	flags.Duration(operatorOption.FixedIPTTL, operatorOption.FixedIPTTLDefault, "ttl for fixed endpoints when pod deleted, 0 means don't gc")
	option.BindEnv(operatorOption.FixedIPTTL)

	flags.Duration(pkgOption.FixedIPTimeout, defaults.CCEEndpointGCInterval, "Timeout for waiting for the fixed IP assignment to succeed")
	option.BindEnv(pkgOption.FixedIPTimeout)


	flags.Bool(operatorOption.EnableRemoteFixedIPGC, true, "gc remote fixed ip when endpoint have been deleted")
	option.BindEnv(operatorOption.EnableRemoteFixedIPGC)

	flags.Int(operatorOption.PSTSSubnetReversedIPNum, 1, "the number of reversed IP in subnet, this flag is useful for psts mode")
	option.BindEnv(operatorOption.PSTSSubnetReversedIPNum)

	flags.Int64(operatorOption.ResourceResyncWorkers, defaults.DefaultResourceResyncWorkers, "Number of workers to process resource event")
	option.BindEnv(operatorOption.ResourceResyncWorkers)

	viper.BindPFlags(flags)
}

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
	"time"

	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/command"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

var log = logging.DefaultLogger.WithField(logfields.LogSubsys, "option")

const (
	// EndpointGCIntervalDefault is the default time for the CEP GC
	EndpointGCIntervalDefault = 5 * time.Minute

	// PrometheusServeAddr is the default server address for operator metrics
	PrometheusServeAddr = ":19963"

	// CESMaxCEPsInCESDefault is the maximum number of cce endpoints allowed in a CES
	CESMaxCEPsInCESDefault = 100

	// CESSlicingModeDefault is default method for grouping CEP in a CES.
	CESSlicingModeDefault = "cesSliceModeIdentity"

	// FixedIPTTLDefault is the default time for the fixed endpoint
	FixedIPTTLDefault = 7 * 24 * time.Hour

	// DefaultResourceResyncInterval is the default time for the resource resync
	DefaultResourceResyncInterval = 30 * time.Second
)

const (

	// SkipCRDCreation specifies whether the CustomResourceDefinition will be
	// disabled for the operator
	SkipCRDCreation = "skip-crd-creation"

	// CNPStatusUpdateInterval is the interval between status updates
	// being sent to the K8s apiserver for a given CNP.
	CNPStatusUpdateInterval = "cnp-status-update-interval"

	// EnableMetrics enables prometheus metrics.
	EnableMetrics = "enable-metrics"

	// EndpointGCInterval is the interval between attempts of the CEP GC
	// controller.
	// Note that only one node per cluster should run this, and most iterations
	// will simply return.
	EndpointGCInterval = "cce-endpoint-gc-interval"

	// NodesGCInterval is the duration for which the cce nodes are GC.
	NodesGCInterval = "nodes-gc-interval"

	// OperatorAPIServeAddr IP:Port on which to serve api requests in
	// operator (pass ":Port" to bind on all interfaces, "" is off)
	OperatorAPIServeAddr = "operator-api-serve-addr"

	// OperatorPrometheusServeAddr IP:Port on which to serve prometheus
	// metrics (pass ":Port" to bind on all interfaces, "" is off).
	OperatorPrometheusServeAddr = "operator-prometheus-serve-addr"

	// PProf enabled pprof debugging endpoint
	PProf = "pprof"

	// PProfPort is the port that the pprof listens on
	PProfPort = "pprof-port"

	// SyncK8sServices synchronizes k8s services into the kvstore
	SyncK8sServices = "synchronize-k8s-services"

	// SyncK8sNodes synchronizes k8s nodes into the kvstore
	SyncK8sNodes = "synchronize-k8s-nodes"

	// UnmanagedPodWatcherInterval is the interval to check for unmanaged kube-dns pods (0 to disable)
	UnmanagedPodWatcherInterval = "unmanaged-pod-watcher-interval"

	// API Rate Limiting

	// APIRateLimitName enables configuration of the API rate limits
	APIRateLimitName = "api-rate-limit"

	// DefaultAPIBurst is the burst value allowed when accessing external Cloud APIs
	DefaultAPIBurst = "default-api-burst"

	// DefaultAPIQPSLimit is the queries per second limit when accessing external Cloud APIs
	DefaultAPIQPSLimit = "default-api-qps"

	// DefaultAPITimeoutLimit is the timeout limit when accessing external Cloud APIs
	DefaultAPITimeoutLimit = "default-api-timeout"

	// IPAM options

	// IPAMSubnetsIDs are optional subnets IDs used to filter subnets and interfaces listing
	IPAMSubnetsIDs = "subnet-ids-filter"

	// IPAMSubnetsTags are optional tags used to filter subnets, and interfaces within those subnets
	IPAMSubnetsTags = "subnet-tags-filter"

	// IPAMInstanceTags are optional tags used to filter instances for ENI discovery ; only used with AWS IPAM mode for now
	IPAMInstanceTags = "instance-tags-filter"

	// ClusterPoolIPv4CIDR is the cluster's IPv4 CIDR to allocate
	// individual PodCIDR ranges from when using the ClusterPool ipam mode.
	ClusterPoolIPv4CIDR = "cluster-pool-ipv4-cidr"

	// ClusterPoolIPv6CIDR is the cluster's IPv6 CIDR to allocate
	// individual PodCIDR ranges from when using the ClusterPool ipam mode.
	ClusterPoolIPv6CIDR = "cluster-pool-ipv6-cidr"

	// NodeCIDRMaskSizeIPv4 is the IPv4 podCIDR mask size that will be used
	// per node.
	NodeCIDRMaskSizeIPv4 = "cluster-pool-ipv4-mask-size"

	// NodeCIDRMaskSizeIPv6 is the IPv6 podCIDR mask size that will be used
	// per node.
	NodeCIDRMaskSizeIPv6 = "cluster-pool-ipv6-mask-size"

	// ExcessIPReleaseDelay controls how long operator would wait before an IP previously marked as excess is released.
	// Defaults to 180 secs
	ExcessIPReleaseDelay = "excess-ip-release-delay"

	// ParallelAllocWorkers specifies the number of parallel workers to be used for IPAM allocation
	ParallelAllocWorkers = "parallel-alloc-workers"

	// LeaderElectionLeaseDuration is the duration that non-leader candidates will wait to
	// force acquire leadership
	LeaderElectionLeaseDuration = "leader-election-lease-duration"

	// LeaderElectionRenewDeadline is the duration that the current acting master in HA deployment
	// will retry refreshing leadership before giving up the lock.
	LeaderElectionRenewDeadline = "leader-election-renew-deadline"

	// LeaderElectionRetryPeriod is the duration the LeaderElector clients should wait between
	// tries of the actions in operator HA deployment.
	LeaderElectionRetryPeriod = "leader-election-retry-period"

	// BCE options

	// BCECloudVPCID allows user to specific vpc
	BCECloudVPCID = "bce-cloud-vpc-id"
	// BCECloudHost host of iaas api
	BCECloudHost              = "bce-cloud-host"
	BCECloudRegion            = "bce-cloud-region"
	BCECloudContry            = "bce-cloud-country"
	BCECloudAccessKey         = "bce-cloud-access-key"
	BCECloudSecureKey         = "bce-cloud-secure-key"

	ResourceENIResyncInterval = "resource-eni-resync-interval"
	ResourceResyncWorkers = "resource-resync-workers"

	// BCECustomerMaxENI is the max eni number of customer
	BCECustomerMaxENI = "bce-customer-max-eni"
	// BCECustomerMaxIP is the max ip number of customer
	BCECustomerMaxIP = "bce-customer-max-ip"

	// CCEK8sNamespace is the namespace where CCE pods are running.
	CCEK8sNamespace = "cce-pod-namespace"

	// CCEPodLabels specifies the pod labels that CCE pods is running
	// with.
	CCEPodLabels = "cce-pod-labels"

	// RemoveNetResourceSetTaints is the flag to define if the CCE node taint
	// should be removed in Kubernetes nodes.
	RemoveNetResourceSetTaints = "remove-network-resource-set-taints"

	// SetCCEIsUpCondition sets the CCEIsUp node condition in Kubernetes
	// nodes.
	SetCCEIsUpCondition = "set-cce-is-up-condition"

	// SkipManagerNodeLabelsName do not enable health checks for certain nodes
	SkipManagerNodeLabelsName = "skip-manager-node-labels"

	// FixedIPTTL ttl for fixed endpoint
	FixedIPTTL = "fixed-ip-ttl-duration"
	// gc remote fixed ip when endpoint have been deleted
	EnableRemoteFixedIPGC = "enable-remote-fixed-ip-gc"

	// cce options
	CCEClusterID = "cce-cluster-id"

	// SubnetReversedIPNum is the number of reversed IP in subnet, this flag is useful for psts mode
	PSTSSubnetReversedIPNum = "psts-subnet-reversed-ip-num"
)

// OperatorConfig is the configuration used by the operator.
type OperatorConfig struct {
	// CNPNodeStatusGCInterval is the GC interval for nodes which have been
	// removed from the cluster in CCENetworkPolicy and
	// CCEClusterwideNetworkPolicy Status.
	CNPNodeStatusGCInterval time.Duration

	// CNPStatusUpdateInterval is the interval between status updates
	// being sent to the K8s apiserver for a given CNP.
	CNPStatusUpdateInterval time.Duration

	// NodeGCInterval is the GC interval for NetResourceSets
	NodeGCInterval time.Duration

	// EnableMetrics enables prometheus metrics.
	EnableMetrics bool

	// EndpointGCInterval is the interval between attempts of the CEP GC
	// controller.
	// Note that only one node per cluster should run this, and most iterations
	// will simply return.
	EndpointGCInterval time.Duration

	OperatorAPIServeAddr        string
	OperatorPrometheusServeAddr string

	// PProf enables pprof debugging endpoint
	PProf bool

	// PProfPort is the port that the pprof listens on
	PProfPort int

	// SyncK8sServices synchronizes k8s services into the kvstore
	SyncK8sServices bool

	// SyncK8sNodes synchronizes k8s nodes into the kvstore
	SyncK8sNodes bool

	// UnmanagedPodWatcherInterval is the interval to check for unmanaged kube-dns pods (0 to disable)
	UnmanagedPodWatcherInterval int

	// LeaderElectionLeaseDuration is the duration that non-leader candidates will wait to
	// force acquire leadership in CCE Operator HA deployment.
	LeaderElectionLeaseDuration time.Duration

	// LeaderElectionRenewDeadline is the duration that the current acting master in HA deployment
	// will retry refreshing leadership in before giving up the lock.
	LeaderElectionRenewDeadline time.Duration

	// LeaderElectionRetryPeriod is the duration that LeaderElector clients should wait between
	// retries of the actions in operator HA deployment.
	LeaderElectionRetryPeriod time.Duration

	// SkipCRDCreation disables creation of the CustomResourceDefinition
	// for the operator
	SkipCRDCreation bool

	// API options

	// DefaultAPIBurst is the burst value allowed when accessing external Cloud APIs
	DefaultAPIBurst int

	// DefaultAPIQPSLimit is the queries per second limit when accessing external Cloud APIs
	DefaultAPIQPSLimit float64

	// DefaultAPITimeoutLimit is the timeout limit when accessing external Cloud APIs
	DefaultAPITimeoutLimit time.Duration

	// APIRateLimitName enables configuration of the API rate limits
	APIRateLimit map[string]string

	// IPAM options

	// IPAMSubnetsIDs are optional subnets IDs used to filter subnets and interfaces listing
	IPAMSubnetsIDs []string

	// IPAMSubnetsTags are optional tags used to filter subnets, and interfaces within those subnets
	IPAMSubnetsTags map[string]string

	// IPAMUInstanceTags are optional tags used to filter AWS EC2 instances, and interfaces (ENI) attached to them
	IPAMInstanceTags map[string]string

	// IPAM Operator options

	// ClusterPoolIPv4CIDR is the cluster IPv4 podCIDR that should be used to
	// allocate pods in the node.
	ClusterPoolIPv4CIDR []string

	// ClusterPoolIPv6CIDR is the cluster IPv6 podCIDR that should be used to
	// allocate pods in the node.
	ClusterPoolIPv6CIDR []string

	// NodeCIDRMaskSizeIPv4 is the IPv4 podCIDR mask size that will be used
	// per node.
	NodeCIDRMaskSizeIPv4 int

	// NodeCIDRMaskSizeIPv6 is the IPv6 podCIDR mask size that will be used
	// per node.
	NodeCIDRMaskSizeIPv6 int

	// ParallelAllocWorkers specifies the number of parallel workers to be used in ENI mode.
	ParallelAllocWorkers int64

	// ExcessIPReleaseDelay controls how long operator would wait before an IP previously marked as excess is released.
	// Defaults to 180 secs
	ExcessIPReleaseDelay int

	// BCE options

	// AlibabaCloudVPCID allow user to specific vpc
	BCECloudVPCID     string
	BCECloudAccessKey string
	BCECloudSecureKey string

	// ResourceResyncInterval is the interval between attempts of the sync between Cloud and k8s
	// like ENIs,Subnets
	ResourceResyncInterval    time.Duration
	ResourceENIResyncInterval time.Duration

	// ResourceResyncWorkers specifies the number of parallel workers to be used in resource handler.
	ResourceResyncWorkers int64

	// BCECustomerMaxENI is the max eni number of customer
	BCECustomerMaxENI int
	// BCECustomerMaxIP is the max ip number of customer
	BCECustomerMaxIP int

	// CCEK8sNamespace is the namespace where CCE pods are running.
	CCEK8sNamespace string

	// CCEPodLabels specifies the pod labels that CCE pods is running
	// with.
	CCEPodLabels string

	// RemoveNetResourceSetTaints is the flag to define if the CCE node taint
	// should be removed in Kubernetes nodes.
	RemoveNetResourceSetTaints bool

	// SetCCEIsUpCondition sets the CCEIsUp node condition in Kubernetes
	// nodes.
	SetCCEIsUpCondition bool

	// SkipManagerNodeLabels do not enable health checks for certain nodes
	// There is an OR relationship between multiple labels
	SkipManagerNodeLabels map[string]string

	// PrivateCloudBaseHost host name of baidu base private cloud
	BCECloudBaseHost string

	BCECloudRegion string

	BCECloudContry string

	// FixedIPTTL
	FixedIPTTL time.Duration

	// FixedIPTimeout Timeout for waiting for the fixed IP assignment to succeed
	FixedIPTimeout time.Duration

	// EnableRemoteFixedIPGC gc remote fixed ip when endpoint have been deleted
	EnableRemoteFixedIPGC bool

	// cce options
	CCEClusterID string

	// SubnetReversedIPNum is the number of IPs to reserve in the subnet
	PSTSSubnetReversedIPNum int

	// EnableIPv4 enables IPv4 support
	EnableIPv4 bool

	// EnableIPv6 enables IPv6 support
	EnableIPv6 bool
}

// Populate sets all options with the values from viper.
func (c *OperatorConfig) Populate() {
	c.CNPStatusUpdateInterval = viper.GetDuration(CNPStatusUpdateInterval)
	c.NodeGCInterval = viper.GetDuration(NodesGCInterval)
	c.EnableMetrics = viper.GetBool(EnableMetrics)
	c.EndpointGCInterval = viper.GetDuration(EndpointGCInterval)
	c.OperatorAPIServeAddr = viper.GetString(OperatorAPIServeAddr)
	c.OperatorPrometheusServeAddr = viper.GetString(OperatorPrometheusServeAddr)
	c.PProf = viper.GetBool(PProf)
	c.PProfPort = viper.GetInt(PProfPort)
	c.SyncK8sServices = viper.GetBool(SyncK8sServices)
	c.SyncK8sNodes = viper.GetBool(SyncK8sNodes)
	c.UnmanagedPodWatcherInterval = viper.GetInt(UnmanagedPodWatcherInterval)
	c.NodeCIDRMaskSizeIPv4 = viper.GetInt(NodeCIDRMaskSizeIPv4)
	c.NodeCIDRMaskSizeIPv6 = viper.GetInt(NodeCIDRMaskSizeIPv6)
	c.ClusterPoolIPv4CIDR = viper.GetStringSlice(ClusterPoolIPv4CIDR)
	c.ClusterPoolIPv6CIDR = viper.GetStringSlice(ClusterPoolIPv6CIDR)
	c.LeaderElectionLeaseDuration = viper.GetDuration(LeaderElectionLeaseDuration)
	c.LeaderElectionRenewDeadline = viper.GetDuration(LeaderElectionRenewDeadline)
	c.LeaderElectionRetryPeriod = viper.GetDuration(LeaderElectionRetryPeriod)
	c.SkipCRDCreation = viper.GetBool(SkipCRDCreation)

	c.CCEPodLabels = viper.GetString(CCEPodLabels)
	c.RemoveNetResourceSetTaints = viper.GetBool(RemoveNetResourceSetTaints)
	c.SetCCEIsUpCondition = viper.GetBool(SetCCEIsUpCondition)
	if m, err := command.GetStringMapStringE(viper.GetViper(), SkipManagerNodeLabelsName); err != nil {
		log.Fatalf("unable to parse %s: %s", SkipManagerNodeLabelsName, err)
	} else {
		c.SkipManagerNodeLabels = m
	}

	c.ParallelAllocWorkers = viper.GetInt64(ParallelAllocWorkers)
	c.ExcessIPReleaseDelay = viper.GetInt(ExcessIPReleaseDelay)
	c.PSTSSubnetReversedIPNum = viper.GetInt(PSTSSubnetReversedIPNum)
	c.EnableIPv4 = viper.GetBool(option.EnableIPv4Name)
	c.EnableIPv6 = viper.GetBool(option.EnableIPv6Name)
	c.CCEK8sNamespace = viper.GetString(CCEK8sNamespace)
	if c.CCEK8sNamespace == "" {
		if option.Config.K8sNamespace == "" {
			c.CCEK8sNamespace = metav1.NamespaceDefault
		} else {
			c.CCEK8sNamespace = option.Config.K8sNamespace
		}
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
	// BCECloud options

	c.BCECloudVPCID = viper.GetString(BCECloudVPCID)
	c.BCECloudBaseHost = viper.GetString(BCECloudHost)
	c.BCECloudRegion = viper.GetString(BCECloudRegion)
	c.BCECloudContry = viper.GetString(BCECloudContry)
	c.BCECloudAccessKey = viper.GetString(BCECloudAccessKey)
	c.BCECloudSecureKey = viper.GetString(BCECloudSecureKey)
	c.ResourceResyncInterval = viper.GetDuration(option.ResourceResyncInterval)
	c.ResourceENIResyncInterval = viper.GetDuration(ResourceENIResyncInterval)
	c.ResourceResyncWorkers = viper.GetInt64(ResourceResyncWorkers)
	c.BCECustomerMaxENI = viper.GetInt(BCECustomerMaxENI)
	c.BCECustomerMaxIP = viper.GetInt(BCECustomerMaxIP)

	c.FixedIPTTL = viper.GetDuration(FixedIPTTL)
	c.FixedIPTimeout = viper.GetDuration(option.FixedIPTimeout)
	c.EnableRemoteFixedIPGC = viper.GetBool(EnableRemoteFixedIPGC)

	c.CCEClusterID = viper.GetString(CCEClusterID)

	// Option maps and slices

	if m := viper.GetStringSlice(IPAMSubnetsIDs); len(m) != 0 {
		c.IPAMSubnetsIDs = m
	}

	if m, err := command.GetStringMapStringE(viper.GetViper(), IPAMSubnetsTags); err != nil {
		log.Fatalf("unable to parse %s: %s", IPAMSubnetsTags, err)
	} else {
		c.IPAMSubnetsTags = m
	}

	if m, err := command.GetStringMapStringE(viper.GetViper(), IPAMInstanceTags); err != nil {
		log.Fatalf("unable to parse %s: %s", IPAMInstanceTags, err)
	} else {
		c.IPAMInstanceTags = m
	}

}

// Config represents the operator configuration.
var Config = &OperatorConfig{
	IPAMSubnetsIDs:        make([]string, 0),
	IPAMSubnetsTags:       make(map[string]string),
	IPAMInstanceTags:      make(map[string]string),
	APIRateLimit:          make(map[string]string),
	SkipManagerNodeLabels: make(map[string]string),
}

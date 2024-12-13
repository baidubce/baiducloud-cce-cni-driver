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
	"runtime"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	cnitypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cni/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/linux"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/debug"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/enim"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/eventqueue"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/mtu"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node"
	nodemanager "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/manager"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/nodediscovery"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/status"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
	grate "golang.org/x/time/rate"
)

const (
	// AutoCIDR indicates that a CIDR should be allocated
	AutoCIDR = "auto"

	// ConfigModifyQueueSize is the size of the event queue for serializing
	// configuration updates to the daemon
	ConfigModifyQueueSize = 10

	syncEndpointsAndHostIPsController = "sync-endpoints-and-host-ips"
)

// Daemon is the cce daemon that is in charge of perform all necessary plumbing,
// monitoring when a LXC starts.
type Daemon struct {
	ctx              context.Context
	cancel           context.CancelFunc
	buildEndpointSem *semaphore.Weighted

	statusCollectMutex lock.RWMutex
	statusResponse     models.StatusResponse
	statusCollector    *status.Collector

	// Used to synchronize generation of daemon's BPF programs and endpoint BPF
	// programs.
	compilationMutex *lock.RWMutex
	mtuConfig        mtu.Configuration

	// nodeDiscovery defines the node discovery logic of the agent
	nodeDiscovery *nodediscovery.NodeDiscovery

	// nodeRDMADiscovery defines the node RDMADiscovery logic of the agent
	rdmaDiscovery *nodediscovery.RdmaDiscovery

	// ipam is the IP address manager of the agent
	ipam ipam.CNIIPAMServer

	rdmaIpam map[string]ipam.CNIIPAMServer

	// enim is the eni manager of the agent
	enim enim.ENIManagerServer

	// endpointManager is the endpoint manager of the agent
	endpointAPIHandler *endpoint.EndpointAPIHandler

	netConf *cnitypes.NetConf

	k8sWatcher *watchers.K8sWatcher

	// k8sCachesSynced is closed when all essential Kubernetes caches have
	// been fully synchronized
	k8sCachesSynced <-chan struct{}

	// event queue for serializing configuration updates to the daemon.
	configModifyQueue *eventqueue.EventQueue

	// CIDRs for which identities were restored during bootstrap
	restoredCIDRs []*net.IPNet

	// address for node
	// this field add by cce
	nodeAddressing types.NodeAddressing

	// Controllers owned by the daemon
	controllers *controller.Manager

	// apiLimiterSet is the set of rate limiters for the API.
	apiLimiterSet rate.ServiceLimiterManager
}

// DebugEnabled returns if debug mode is enabled.
func (d *Daemon) DebugEnabled() bool {
	return option.Config.Opts.IsEnabled(option.Debug)
}

// GetOptions returns the datapath configuration options of the daemon.
func (d *Daemon) GetOptions() *option.IntOptions {
	return option.Config.Opts
}

// GetCompilationLock returns the mutex responsible for synchronizing compilation
// of BPF programs.
func (d *Daemon) GetCompilationLock() *lock.RWMutex {
	return d.compilationMutex
}

func (d *Daemon) init() error {
	globalsDir := option.Config.GetGlobalsDir()
	if err := os.MkdirAll(globalsDir, defaults.RuntimePathRights); err != nil {
		log.WithError(err).WithField(logfields.Path, globalsDir).Fatal("Could not create runtime directory")
	}

	if err := os.Chdir(option.Config.StateDir); err != nil {
		log.WithError(err).WithField(logfields.Path, option.Config.StateDir).Fatal("Could not change to runtime directory")
	}

	return nil
}

// NewDaemon creates and returns a new Daemon with the parameters set in c.
func NewDaemon(ctx context.Context, cancel context.CancelFunc) (*Daemon, error) {

	// Pass the cancel to our signal handler directly so that it's canceled
	// before we run the cleanup functions (see `cleanup.go` for implementation).
	cleaner.SetCancelFunc(cancel)

	var (
		err           error
		netConf       *cnitypes.NetConf
		configuredMTU = option.Config.MTU
	)

	bootstrapStats.daemonInit.Start()

	// Validate the daemon-specific global options.
	if err := option.Config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid daemon configuration: %s", err)
	}

	set, err := rate.NewAPILimiterSet(option.Config.APIRateLimit, apiRateLimitDefaults, rate.SimpleMetricsObserver)
	if err != nil {
		log.WithError(err).Error("unable to configure API rate limiting")
		return nil, fmt.Errorf("unable to configure API rate limiting: %w", err)
	}
	apiLimiterSet := rate.APILimiterSetWrapDefault(set, &rate.APILimiterParameters{
		RateLimit:       grate.Limit(option.Config.DefaultAPIQPSLimit),
		RateBurst:       option.Config.DefaultAPIBurst,
		MaxWaitDuration: option.Config.DefaultAPITimeoutLimit,
	})

	externalIP := node.GetIPv4()
	if externalIP == nil {
		externalIP = node.GetIPv6()
	}

	var (
		mtuConfig mtu.Configuration
	)
	// ExternalIP could be nil but we are covering that case inside NewConfiguration
	mtuConfig = mtu.NewConfiguration(
		0,
		false,
		false,
		false,
		configuredMTU,
		externalIP,
	)

	// linux datapath node, this customer by cce
	addr := linux.NewNodeAddressing()
	nodeMngr, err := nodemanager.NewManager("all", linux.NewNodeHandler(addr))
	if err != nil {
		return nil, err
	}

	nd := nodediscovery.NewNodeDiscovery(nodeMngr, mtuConfig, netConf)
	rd := nodediscovery.NewRdmaDiscovery(nodeMngr, mtuConfig, netConf)

	d := Daemon{
		ctx:              ctx,
		cancel:           cancel,
		buildEndpointSem: semaphore.NewWeighted(int64(numWorkerThreads())),
		compilationMutex: new(lock.RWMutex),
		netConf:          netConf,
		mtuConfig:        mtuConfig,
		nodeDiscovery:    nd,
		rdmaDiscovery:    rd,
		apiLimiterSet:    apiLimiterSet,
		controllers:      controller.NewManager(),
		nodeAddressing:   addr,
	}

	d.configModifyQueue = eventqueue.NewEventQueueBuffered("config-modify-queue", ConfigModifyQueueSize)
	d.configModifyQueue.Run()

	d.k8sWatcher = watchers.NewK8sWatcher()

	// register the node discovery with the k8s watcher
	d.nodeDiscovery.RegisterK8sNodeGetter(d.k8sWatcher)
	d.nodeDiscovery.RegisterNrcsNodeGetter(d.k8sWatcher)

	d.rdmaDiscovery.RegisterK8sNodeGetter(d.k8sWatcher)
	d.rdmaDiscovery.RegisterNrcsNodeGetter(d.k8sWatcher)

	d.k8sWatcher.NodeChain.Register(d.nodeDiscovery)

	bootstrapStats.daemonInit.End(true)

	debug.RegisterStatusObject("ipam", d.ipam)

	if k8s.IsEnabled() {
		bootstrapStats.k8sInit.Start()
		// Errors are handled inside WaitForCRDsToRegister. It will fatal on a
		// context deadline or if the context has been cancelled, the context's
		// error will be returned. Otherwise, it succeeded.
		if err := d.k8sWatcher.WaitForCRDsToRegister(d.ctx); err != nil {
			return nil, err
		}

		// Launch the K8s node watcher so we can start receiving node events.
		// Launching the k8s node watcher at this stage will prevent all agents
		// from performing Gets directly into kube-apiserver to get the most up
		// to date version of the k8s node. This allows for better scalability
		// in large clusters.
		d.k8sWatcher.NodesInit(k8s.Client())

		// Initialize d.k8sCachesSynced before any k8s watchers are alive, as they may
		// access it to check the status of k8s initialization
		cachesSynced := make(chan struct{})
		d.k8sCachesSynced = cachesSynced

		// Launch the K8s watchers in parallel as we continue to process other
		// daemon options.
		d.k8sWatcher.InitK8sSubsystem(d.ctx, cachesSynced)

		if option.Config.IPAM == ipamOption.IPAMClusterPool ||
			option.Config.IPAM == ipamOption.IPAMClusterPoolV2 ||
			option.Config.IPAM == ipamOption.IPAMVpcRoute {
			// Create the NetResourceSet custom resource. This call will block until
			// the custom resource has been created
			d.nodeDiscovery.UpdateNetResourceSetResource()
		}

		if err := k8s.WaitForNodeInformation(d.ctx, d.k8sWatcher); err != nil {
			log.WithError(err).Error("unable to connect to get node spec from apiserver")
			return nil, fmt.Errorf("unable to connect to get node spec from apiserver: %w", err)
		}

		// Kubernetes demands that the localhost can always reach local
		// pods. Therefore unless the AllowLocalhost policy is set to a
		// specific mode, always allow localhost to reach local
		// endpoints.
		if option.Config.AllowLocalhost == option.AllowLocalhostAuto {
			option.Config.AllowLocalhost = option.AllowLocalhostAlways
			log.Info("k8s mode: Allowing localhost to reach local endpoints")
		}

		<-cachesSynced
		bootstrapStats.k8sInit.End(true)
	}

	// confugure and start ENIM
	d.configureENIM()
	d.startENIM()

	// Configure IPAM without using the configuration yet.
	d.configureIPAM()
	d.startIPAM()

	d.configureRDMAIPAM()
	d.startRDMAIPAM()

	for key, ri := range d.rdmaIpam {
		debug.RegisterStatusObject("rdmaIpam["+key+"]", ri)
	}

	// endpoints handler
	d.startEndpointHanler()

	// Must occur after d.allocateIPs(), see GH-14245 and its fix.
	d.nodeDiscovery.StartDiscovery()
	d.rdmaDiscovery.StartDiscovery()

	// Annotation of the k8s node must happen after discovery of the
	// PodCIDR range and allocation of the health IPs.
	if k8s.IsEnabled() && option.Config.AnnotateK8sNode {
		bootstrapStats.k8sInit.Start()
		log.WithFields(logrus.Fields{
			logfields.V4Prefix:    node.GetIPv4AllocRange(),
			logfields.V6Prefix:    node.GetIPv6AllocRange(),
			logfields.V4HealthIP:  node.GetEndpointHealthIPv4(),
			logfields.V6HealthIP:  node.GetEndpointHealthIPv6(),
			logfields.V4IngressIP: node.GetIngressIPv4(),
			logfields.V6IngressIP: node.GetIngressIPv6(),
			logfields.V4CCEHostIP: node.GetInternalIPv4Router(),
			logfields.V6CCEHostIP: node.GetIPv6Router(),
		}).Info("Annotating k8s node")

		err := k8s.Client().AnnotateNode(nodeTypes.GetName(),
			0,
			node.GetIPv4AllocRange(), node.GetIPv6AllocRange(),
			node.GetEndpointHealthIPv4(), node.GetEndpointHealthIPv6(),
			node.GetIngressIPv4(), node.GetIngressIPv6(),
			node.GetInternalIPv4Router(), node.GetIPv6Router())
		if err != nil {
			log.WithError(err).Warning("Cannot annotate k8s node with CIDR range")
		}
		bootstrapStats.k8sInit.End(true)
	} else if !option.Config.AnnotateK8sNode {
		log.Debug("Annotate k8s node is disabled.")
	}

	// Trigger refresh and update custom resource in the apiserver with all restored endpoints.
	// Trigger after nodeDiscovery.StartDiscovery to avoid custom resource update conflict.
	if option.Config.IPAM == ipamOption.IPAMCRD ||
		option.Config.IPAM == ipamOption.IPAMVpcEni ||
		option.Config.IPAM == ipamOption.IPAMVpcRoute ||
		option.Config.IPAM == ipamOption.IPAMClusterPoolV2 ||
		option.Config.IPAM == ipamOption.IPAMPrivateCloudBase {
		d.ipam.RestoreFinished()
		for _, ri := range d.rdmaIpam {
			ri.RestoreFinished()
		}
	}
	return &d, nil
}

// Close shuts down a daemon
func (d *Daemon) Close() {

}

func changedOption(key string, value option.OptionSetting, data interface{}) {
	d := data.(*Daemon)
	if key == option.Debug {
		// Set the debug toggle (this can be a no-op)
		if d.DebugEnabled() {
			logging.SetLogLevelToDebug()
		}
	}
}

// numWorkerThreads returns the number of worker threads with a minimum of 4.
func numWorkerThreads() int {
	ncpu := runtime.NumCPU()
	minWorkerThreads := 2

	if ncpu < minWorkerThreads {
		return minWorkerThreads
	}
	return ncpu
}

// GetNodeSuffix returns the suffix to be appended to kvstore keys of this
// agent
func (d *Daemon) GetNodeSuffix() string {
	var ip net.IP

	switch {
	case option.Config.EnableIPv4:
		ip = node.GetIPv4()
	case option.Config.EnableIPv6:
		ip = node.GetIPv6()
	}

	if ip == nil {
		log.Fatal("Node IP not available yet")
	}

	return ip.String()
}

// K8sCacheIsSynced returns true if the agent has fully synced its k8s cache
// with the API server
func (d *Daemon) K8sCacheIsSynced() bool {
	if !k8s.IsEnabled() {
		return true
	}
	select {
	case <-d.k8sCachesSynced:
		return true
	default:
		return false
	}
}

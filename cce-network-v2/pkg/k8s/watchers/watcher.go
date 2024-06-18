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

package watchers

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	k8s_metrics "k8s.io/client-go/tools/metrics"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	k8smetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/synced"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/resources"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	corev1 "k8s.io/api/core/v1"
	slim_discover_v1 "k8s.io/api/discovery/v1"
	slim_discover_v1beta1 "k8s.io/api/discovery/v1beta1"
)

const (
	k8sAPIGroupNodeV1Core       = "core/v1::Node"
	k8sAPIGroupNamespaceV1Core  = "core/v1::Namespace"
	K8sAPIGroupServiceV1Core    = "core/v1::Service"
	k8sAPIGroupNetResourceSetV2 = "cce.baidubce.com/v2::NetResourceSet"
	k8sAPIGroupCCEEndpointV2    = "cce.baidubce.com/v2::CCEEndpoint"
	k8sAPIGroupCCEENIV2         = "cce.baidubce.com/v2::ENI"
	k8sAPIGroupCCESubnetV1      = "cce.baidubce.com/v1::Subnet"

	metricKNP            = "NetworkPolicy"
	metricNS             = "Namespace"
	metricSecret         = "Secret"
	metricNetResourceSet = "NetResourceSet"
	metricCCEEndpoint    = "CCEEndpoint"
	metricENI            = "ENI"
	metricPod            = "Pod"
	metricNode           = "Node"
)

func init() {
	// Replace error handler with our own
	runtime.ErrorHandlers = []func(error){
		k8s.K8sErrorHandler,
	}

	k8s_metrics.Register(k8s_metrics.RegisterOpts{
		ClientCertExpiry:      nil,
		ClientCertRotationAge: nil,
		RequestLatency:        &k8sMetrics{},
		RateLimiterLatency:    nil,
		RequestResult:         &k8sMetrics{},
	})
}

var (
	log = logging.NewSubysLogger("k8s-watcher")
)

type nodeDiscoverManager interface {
	WaitForLocalNodeInit()
	NodeDeleted(n nodeTypes.Node)
	NodeUpdated(n nodeTypes.Node)
	ClusterSizeDependantInterval(baseInterval time.Duration) time.Duration
}
type bgpSpeakerManager interface {
	OnUpdateService(svc *corev1.Service) error
	OnDeleteService(svc *corev1.Service) error

	OnUpdateEndpoints(eps *corev1.Endpoints) error
	OnUpdateEndpointSliceV1(eps *slim_discover_v1.EndpointSlice) error
	OnUpdateEndpointSliceV1Beta1(eps *slim_discover_v1beta1.EndpointSlice) error
}
type K8sWatcher struct {
	// k8sResourceSynced maps a resource name to a channel. Once the given
	// resource name is synchronized with k8s, the channel for which that
	// resource name maps to is closed.
	k8sResourceSynced synced.Resources

	// k8sAPIGroups is a set of k8s API in use. They are setup in watchers,
	// and may be disabled while the agent runs.
	k8sAPIGroups synced.APIGroups

	// NodeChain is the root of a notification chain for k8s Node events.
	// This NodeChain allows registration of subscriber.Node implementations.
	// On k8s Node events all registered subscriber.Node implementations will
	// have their event handling methods called in order of registration.
	NodeChain *subscriber.NodeChain

	// NetResourceSetChain is the root of a notification chain for NetResourceSet events.
	// This NetResourceSetChain allows registration of subscriber.NetResourceSet implementations.
	// On NetResourceSet events all registered subscriber.NetResourceSet implementations will
	// have their event handling methods called in order of registration.
	NetResourceSetChain *subscriber.NetResourceSetChain

	// CCEEndpointChain is the root of a notification chain for NetResourceSet events.
	// This CCEEndpointChain allows registration of subscriber.CCEEndpoint implementations.
	// On NetResourceSet events all registered subscriber.CCEEndpoint implementations will
	// have their event handling methods called in order of registration.
	CCEEndpointChain *subscriber.CCEEndpointChain

	// controllersStarted is a channel that is closed when all watchers that do not depend on
	// local node configuration have been started
	controllersStarted chan struct{}

	stop chan struct{}

	podStoreMU lock.RWMutex
	podStore   cache.Store
	// podStoreSet is a channel that is closed when the podStore cache is
	// variable is written for the first time.
	podStoreSet  chan struct{}
	podStoreOnce sync.Once

	nodeStore cache.Store

	netResourceSetStoreMU lock.RWMutex
	netResourceSetStore   cache.Store

	namespaceStore cache.Store

	cceEndpointStore cache.Store

	// ENIChain is the root of a notification chain for ENI events.
	// This ENIChain allows registration of subscriber.CCEEndpointChain implementations.
	// On NetResourceSet events all registered subscriber.CCEEndpointChain implementations will
	// have their event handling methods called in order of registration.
	ENIChain *subscriber.ENIChain
	eniStore cache.Store
}

func NewK8sWatcher() *K8sWatcher {
	return &K8sWatcher{
		controllersStarted:  make(chan struct{}),
		stop:                make(chan struct{}),
		podStoreSet:         make(chan struct{}),
		NodeChain:           subscriber.NewNodeChain(),
		NetResourceSetChain: subscriber.NewNetResourceSetChain(),
		CCEEndpointChain:    subscriber.NewCCEEndpointChain(),
		ENIChain:            subscriber.NewENIChain(),
	}
}

// k8sMetrics implements the LatencyMetric and ResultMetric interface from
// k8s client-go package
type k8sMetrics struct{}

func (*k8sMetrics) Observe(_ context.Context, verb string, u url.URL, latency time.Duration) {
	metrics.KubernetesAPIInteractions.WithLabelValues(u.Path, verb).Observe(latency.Seconds())
}

func (*k8sMetrics) Increment(_ context.Context, code string, method string, host string) {
	metrics.KubernetesAPICallsTotal.WithLabelValues(host, method, code).Inc()
	// The 'code' is set to '<error>' in case an error is returned from k8s
	// more info:
	// https://github.com/kubernetes/client-go/blob/v0.18.0-rc.1/rest/request.go#L700-L703
	if code != "<error>" {
		// Consider success if status code is 2xx or 4xx
		if strings.HasPrefix(code, "2") ||
			strings.HasPrefix(code, "4") {
			k8smetrics.LastSuccessInteraction.Reset()
		}
	}
	k8smetrics.LastInteraction.Reset()
}

// WaitForCacheSync blocks until the given resources have been synchronized from k8s.  Note that if
// the controller for a resource has not been started, the wait for that resource returns
// immediately. If it is required that the resource exists and is actually synchronized, the caller
// must ensure the controller for that resource has been started before calling
// WaitForCacheSync. For most resources this can be done by receiving from controllersStarted
// channel (<-k.controllersStarted), which is closed after most watchers have been started.
func (k *K8sWatcher) WaitForCacheSync(resourceNames ...string) {
	k.k8sResourceSynced.WaitForCacheSync(resourceNames...)
}

// WaitForCacheSyncWithTimeout calls WaitForCacheSync to block until given resources have had their caches
// synced from K8s. This will wait up to the timeout duration after starting or since the last K8s
// registered watcher event (i.e. each event causes the timeout to be pushed back). Events are recorded
// using K8sResourcesSynced.Event function. If the timeout is exceeded, an error is returned.
func (k *K8sWatcher) WaitForCacheSyncWithTimeout(timeout time.Duration, resourceNames ...string) error {
	return k.k8sResourceSynced.WaitForCacheSyncWithTimeout(timeout, resourceNames...)
}

func (k *K8sWatcher) cancelWaitGroupToSyncResources(resourceName string) {
	k.k8sResourceSynced.CancelWaitGroupToSyncResources(resourceName)
}

func (k *K8sWatcher) blockWaitGroupToSyncResources(
	stop <-chan struct{},
	swg *lock.StoppableWaitGroup,
	hasSyncedFunc cache.InformerSynced,
	resourceName string,
) {
	k.k8sResourceSynced.BlockWaitGroupToSyncResources(stop, swg, hasSyncedFunc, resourceName)
}

func (k *K8sWatcher) GetAPIGroups() []string {
	return k.k8sAPIGroups.GetGroups()
}

// WaitForCRDsToRegister will wait for the CCE Operator to register the CRDs
// with the apiserver. This step is required before launching the full K8s
// watcher, as those resource controllers need the resources to be registered
// with K8s first.
func (k *K8sWatcher) WaitForCRDsToRegister(ctx context.Context) error {
	return synced.SyncCRDs(ctx, synced.AgentCRDResourceNames(), &k.k8sResourceSynced, &k.k8sAPIGroups)
}

type watcherKind int

const (
	// skip causes watcher to not be started.
	skip watcherKind = iota

	// start causes watcher to be started as soon as possible.
	start

	// afterNodeInit causes watcher to be started after local node has been initialized
	// so that e.g., local node addressing info is available.
	afterNodeInit
)

type watcherInfo struct {
	kind  watcherKind
	group string
}

var cceResourceToGroupMapping = map[string]watcherInfo{
	synced.CRDResourceName(v2.CEPName):       {start, k8sAPIGroupCCEEndpointV2},
	synced.CRDResourceName(v2.NRSName):       {start, k8sAPIGroupNetResourceSetV2},
	synced.CRDResourceName(v2.ENIName):       {start, k8sAPIGroupCCEENIV2},
	synced.CRDResourceName(ccev1.SubnetName): {start, k8sAPIGroupCCESubnetV1},
}

// resourceGroups are all of the core Kubernetes and CCE resource groups
// which the CCE agent watches to implement CNI functionality.
func (k *K8sWatcher) resourceGroups() (beforeNodeInitGroups, afterNodeInitGroups []string) {
	k8sGroups := []string{
		// Pods can contain labels which are essential for endpoints
		// being restored to have the right identity.
		resources.K8sAPIGroupPodV1Core,
		// We need to know the node labels to populate the host
		// endpoint labels.
		k8sAPIGroupNodeV1Core,
	}

	// To pe
	cceResources := synced.AgentCRDResourceNames()
	cceGroups := make([]string, 0, len(cceResources))
	for _, r := range cceResources {
		groupInfo, ok := cceResourceToGroupMapping[r]
		if !ok {
			log.Fatalf("Unknown resource %s. Please update pkg/k8s/watchers to understand this type.", r)
		}
		switch groupInfo.kind {
		case skip:
			continue
		case start:
			cceGroups = append(cceGroups, groupInfo.group)
		case afterNodeInit:
			afterNodeInitGroups = append(afterNodeInitGroups, groupInfo.group)
		}
	}

	return append(k8sGroups, cceGroups...), afterNodeInitGroups
}

// InitK8sSubsystem takes a channel for which it will be closed when all
// caches essential for daemon are synchronized.
// To be called after WaitForCRDsToRegister() so that all needed CRDs have
// already been registered.
func (k *K8sWatcher) InitK8sSubsystem(ctx context.Context, cachesSynced chan struct{}) {
	log.Info("Enabling k8s event listener")
	resources, afterNodeInitResources := k.resourceGroups()
	if err := k.enableK8sWatchers(ctx, resources); err != nil {
		if !errors.Is(err, context.Canceled) {
			log.WithError(err).Fatal("Unable to start K8s watchers for CCE")
		}
		// If the context was canceled it means the daemon is being stopped
		return
	}
	close(k.controllersStarted)

	go func() {
		log.Info("Waiting until local node addressing before starting watchers depending on it")
		if err := k.enableK8sWatchers(ctx, afterNodeInitResources); err != nil {
			if !errors.Is(err, context.Canceled) {
				log.WithError(err).Fatal("Unable to start K8s watchers for CCE")
			}
			// If the context was canceled it means the daemon is being stopped
			return
		}
		log.Info("Waiting until all pre-existing resources have been received")
		if err := k.WaitForCacheSyncWithTimeout(option.Config.K8sSyncTimeout, append(resources, afterNodeInitResources...)...); err != nil {
			log.WithError(err).Fatal("Timed out waiting for pre-existing resources to be received; exiting")
		}
		close(cachesSynced)
	}()
}

// WatcherConfiguration is the required configuration for enableK8sWatchers
type WatcherConfiguration interface {
	utils.ServiceConfiguration
	utils.IngressConfiguration
}

// enableK8sWatchers starts watchers for given resources.
func (k *K8sWatcher) enableK8sWatchers(ctx context.Context, resourceNames []string) error {
	if !k8s.IsEnabled() {
		log.Debug("Not enabling k8s event listener because k8s is not enabled")
		return nil
	}
	cceClient := k8s.CCEClient()
	asyncControllers := &sync.WaitGroup{}

	for _, r := range resourceNames {
		log.Infof("Enabling watcher for %s", r)
		switch r {
		// Core CCE
		case resources.K8sAPIGroupPodV1Core:
			asyncControllers.Add(1)
			go k.podsInit(k8s.WatcherClient(), asyncControllers)
		case k8sAPIGroupNodeV1Core:
			k.NodesInit(k8s.Client())
		case k8sAPIGroupNetResourceSetV2:
			asyncControllers.Add(1)
			go k.netResourceSetInit(cceClient, asyncControllers)
		case k8sAPIGroupCCEEndpointV2:
			asyncControllers.Add(1)
			go k.cceEndpointInit(cceClient, asyncControllers)
		case k8sAPIGroupCCEENIV2:
			asyncControllers.Add(1)
			go k.eniInit(cceClient, asyncControllers)
		case k8sAPIGroupCCESubnetV1:
			asyncControllers.Add(1)
			go k.subnetInit(cceClient, asyncControllers)
		default:
			log.WithFields(logrus.Fields{
				logfields.Resource: r,
			}).Fatal("Not listening for Kubernetes resource updates for unhandled type")
		}
	}

	asyncControllers.Wait()
	return nil
}

// K8sEventProcessed is called to do metrics accounting for each processed
// Kubernetes event
func (k *K8sWatcher) K8sEventProcessed(scope, action string, status bool) {
	result := "success"
	if status == false {
		result = "failed"
	}

	metrics.KubernetesEventProcessed.WithLabelValues(scope, action, result).Inc()
}

// K8sEventReceived does metric accounting for each received Kubernetes event, as well
// as notifying of events for k8s resources synced.
func (k *K8sWatcher) K8sEventReceived(apiResourceName, scope, action string, valid, equal bool) {
	k8smetrics.LastInteraction.Reset()

	metrics.EventTS.WithLabelValues(metrics.LabelEventSourceK8s, scope, action).SetToCurrentTime()
	validStr := strconv.FormatBool(valid)
	equalStr := strconv.FormatBool(equal)
	metrics.KubernetesEventReceived.WithLabelValues(scope, action, validStr, equalStr).Inc()

	k.k8sResourceSynced.SetEventTimestamp(apiResourceName)
}

// GetStore returns the k8s cache store for the given resource name.
func (k *K8sWatcher) GetStore(name string) cache.Store {
	switch name {
	case "namespace":
		return k.namespaceStore
	case "pod":
		// Wait for podStore to get initialized.
		<-k.podStoreSet
		// Access to podStore is protected by podStoreMU.
		k.podStoreMU.RLock()
		defer k.podStoreMU.RUnlock()
		return k.podStore

	default:
		return nil
	}
}

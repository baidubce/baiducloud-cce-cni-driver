/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package route

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/baidubce/bce-sdk-go/services/vpc"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	k8snet "k8s.io/utils/net"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	utilippool "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/util/ippool"
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8swatcher"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/slice"
)

const (
	// NodeAnnotationPrefix is the annotation prefix of Node
	NodeAnnotationPrefix = "node.alpha.kubernetes.io/"
	// NodeAnnotationAdvertiseRoute indicates whether to advertise route to vpc route table
	NodeAnnotationAdvertiseRoute = NodeAnnotationPrefix + "advertise-route"

	ensureVPCRoutePossibilityEnvKey = "ENSURE_VPC_ROUTE_POSSIBILITY"
)

var (
	// ErrNoPodCIDR it may happen when kube-controller-manager not update pod CIDR for a new node that in time.
	ErrNoPodCIDR = errors.New("no pod CIDR found")
	// ErrNoNodeAddress if a node does not have a address
	ErrNoNodeAddress = errors.New("no node address found")
	// ErrInvalidAddress if an IP Address/CIDR not valid
	ErrInvalidAddress = errors.New("invalid IP addr/cidr")
)

// RouteController manages both vpc and static routes for local host
type RouteController struct {
	cache         *StaticRouteCache
	kubeClient    kubernetes.Interface
	cloudClient   cloud.Interface
	crdClient     clientset.Interface
	metaClient    metadata.Interface
	eventRecorder record.EventRecorder

	// properties from bce
	vpcID             string
	hostInstanceID    string
	clusterID         string
	enableStaticRoute bool
	enableVPCRoute    bool

	containerNetworkCIDRIPv4 string
	containerNetworkCIDRIPv6 string

	// properties from k8s
	nodeName string
	nodeRef  *v1.ObjectReference
	podCIDRs []string
}

var _ k8swatcher.NodeHandler = &RouteController{}

func NewRouteController(
	kubeClient kubernetes.Interface,
	eventRecorder record.EventRecorder,
	cloudClient cloud.Interface,
	crdClient clientset.Interface,
	hostName string,
	instanceID string,
	clusterID string,
	enableVPCRoute bool,
	enableStaticRoute bool,
	containerNetworkCIDRIPv4 string,
	containerNetworkCIDRIPv6 string,
) (*RouteController, error) {
	var (
		nodeRef = &v1.ObjectReference{
			Kind:      "Node",
			Name:      string(hostName),
			UID:       types.UID(hostName),
			Namespace: "",
		}
	)

	rc := &RouteController{
		cache:                    &StaticRouteCache{routeMap: make(map[string]*cachedStaticRoute)},
		kubeClient:               kubeClient,
		cloudClient:              cloudClient,
		crdClient:                crdClient,
		metaClient:               metadata.NewClient(),
		eventRecorder:            eventRecorder,
		nodeName:                 hostName,
		nodeRef:                  nodeRef,
		clusterID:                clusterID,
		hostInstanceID:           instanceID,
		enableStaticRoute:        enableStaticRoute,
		enableVPCRoute:           enableVPCRoute,
		containerNetworkCIDRIPv4: containerNetworkCIDRIPv4,
		containerNetworkCIDRIPv6: containerNetworkCIDRIPv6,
	}
	if err := rc.init(); err != nil {
		return nil, err
	}

	return rc, nil
}

func (rc *RouteController) SyncNode(nodeKey string, nodeLister corelisters.NodeLister) error {
	ctx := log.NewContext()
	return rc.syncRoute(ctx, nodeKey)
}

func (rc *RouteController) init() error {
	ctx := log.NewContext()

	if rc.enableVPCRoute {
		vpcID, err := rc.getVPCID(ctx)
		if err != nil {
			log.Errorf(ctx, "init RouteController error: %v", err)
			return err
		}
		log.Infof(ctx, "init: cluster is in VPC %s", vpcID)

		instanceID, err := rc.getHostInstanceID(ctx)
		if err != nil {
			log.Errorf(ctx, "init RouteController error: %v", err)
			return err
		}
		log.Infof(ctx, "init: node %v has instanceID %s", rc.nodeName, instanceID)
	}

	return nil
}

func (rc *RouteController) getVPCID(ctx context.Context) (string, error) {
	if rc.vpcID == "" {
		vpcID, err := rc.metaClient.GetVPCID()
		if err != nil {
			return "", fmt.Errorf("failed to get vpcID from metadata api: %v", err)
		}
		rc.vpcID = vpcID
	}

	return rc.vpcID, nil
}

func (rc *RouteController) listVPCRouteTable(ctx context.Context) ([]vpc.RouteRule, error) {
	vpcID, err := rc.getVPCID(ctx)
	if err != nil {
		return nil, err
	}
	rules, err := rc.cloudClient.ListRouteTable(ctx, vpcID, "")
	if err != nil {
		return nil, err
	}
	if len(rules) < 1 {
		return nil, fmt.Errorf("VPC route length error: length is : %d", len(rules))
	}

	return rules, nil
}

func (rc *RouteController) getHostInstanceID(ctx context.Context) (string, error) {
	if rc.hostInstanceID == "" {
		instanceID, err := rc.metaClient.GetInstanceID()
		if err != nil {
			return "", fmt.Errorf("failed to get instanceID from metadata api: %v", err)
		}
		rc.hostInstanceID = instanceID
	}

	return rc.hostInstanceID, nil
}

func (rc *RouteController) getPodCIDRs(ctx context.Context) ([]string, error) {
	if rc.podCIDRs != nil {
		return rc.podCIDRs, nil
	}

	ippoolName := utilippool.GetNodeIPPoolName(rc.nodeName)
	ippool, err := rc.crdClient.CceV1alpha1().IPPools(v1.NamespaceDefault).Get(ctx, ippoolName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	podCIDRs := make([]string, 0)
	for _, iprange := range ippool.Spec.IPv4Ranges {
		podCIDRs = append(podCIDRs, iprange.CIDR)
	}
	for _, iprange := range ippool.Spec.IPv6Ranges {
		podCIDRs = append(podCIDRs, iprange.CIDR)
	}

	if len(podCIDRs) == 0 {
		return nil, fmt.Errorf("node.spec.podCIDRs is empty")
	}

	rc.podCIDRs = podCIDRs
	return rc.podCIDRs, nil
}

// ensureVPCRoute ensures <0.0.0.0/0	nodePodCIDR	-> HostInstanceID> vpc route
func (rc *RouteController) ensureVPCRoute(ctx context.Context, thisNode *v1.Node) error {
	routes, err := rc.listVPCRouteTable(ctx)
	if err != nil {
		return err
	}

	shouldAdvertiseRoute, err := rc.advertiseRoute(thisNode)
	if err != nil {
		return err
	}
	log.V(6).Infof(ctx, "advertiseRoute for node %s: %v", rc.nodeName, shouldAdvertiseRoute)

	return rc.reconcileVPCRoute(ctx, routes, shouldAdvertiseRoute)
}

// reconcileVPCRoute ensures essential route and cleanup old/useless routes
func (rc *RouteController) reconcileVPCRoute(ctx context.Context, routes []vpc.RouteRule, shouldAdvertiseRoute bool) error {
	isRouteExist := make(map[string]bool)
	for _, route := range routes {
		isResponsible, err := rc.isResponsibleForRoute(route)
		if err != nil {
			log.Errorf(ctx, "failed to check a route is responsible by route controller +v: %v", route, err)
			return err
		}
		if isResponsible {
			// ignore pod cidr empty
			if len(rc.podCIDRs) == 0 {
				continue
			}
			// this is the target route
			if slice.ContainsString(rc.podCIDRs, route.DestinationAddress, nil) && shouldAdvertiseRoute {
				isRouteExist[route.DestinationAddress] = true
				continue
			}
			// this is the old route
			if err := rc.cloudClient.DeleteRouteRule(ctx, route.RouteRuleId); err != nil && !isNotFoundError(err) {
				log.Errorf(ctx, "failed to remove old route %+v: %v", route, err)
				return err
			}
			log.Warningf(ctx, "remove old route: %+v", route)
			continue
		}
		log.V(6).Infof(ctx, "keep other route: %+v", route)
	}

	if shouldAdvertiseRoute {
		for _, cidr := range rc.podCIDRs {
			if _, ok := isRouteExist[cidr]; ok {
				log.V(6).Infof(ctx, "skip adding target route for pod cidr: %s", cidr)
				continue
			}
			createRouteArg := &vpc.CreateRouteRuleArgs{
				RouteTableId:       routes[0].RouteTableId, // len(routes) >= 1
				SourceAddress:      "0.0.0.0/0",
				DestinationAddress: cidr,
				NexthopId:          rc.hostInstanceID,
				NexthopType:        "custom",
				Description:        fmt.Sprintf("auto generated by cce:%s", rc.clusterID),
			}
			if k8snet.IsIPv4CIDRString(cidr) {
				createRouteArg.SourceAddress = "0.0.0.0/0"
			} else if k8snet.IsIPv6CIDRString(cidr) {
				// TODO CreateRouteRuleArgs.IPVersion
				log.Infof(ctx, "ipv6 routes are not implemented, skip")
				continue
			} else {
				err := fmt.Errorf("illegal dst pod cidr %s", cidr)
				log.Errorf(ctx, "failed to create target route: %v", err)
				return err
			}
			// create target route
			if _, err := rc.cloudClient.CreateRouteRule(ctx, createRouteArg); err != nil {
				if !cloud.IsErrorRouteRuleRepeated(err) {
					log.Errorf(ctx, "failed to create target route <%s %s -> %s>: %v", createRouteArg.SourceAddress, createRouteArg.DestinationAddress, createRouteArg.NexthopId, err)
					return err
				}
			}

			log.Infof(ctx, "create target route successfully: <%s %s -> %s>", createRouteArg.SourceAddress, createRouteArg.DestinationAddress, createRouteArg.NexthopId)
		}
	}

	return nil
}

// advertiseRoute indicates whether this node should be routable
func (rc *RouteController) advertiseRoute(node *v1.Node) (bool, error) {
	// no annotations
	if node.Annotations == nil {
		return false, nil
	}

	advertiseRoute, ok := node.Annotations[NodeAnnotationAdvertiseRoute]
	if ok {
		advertise, err := strconv.ParseBool(advertiseRoute)
		if err != nil {
			return true, fmt.Errorf("NodeAnnotationAdvertiseRoute syntex error: %v", err)
		}
		return advertise, nil
	}

	// create route by default
	return true, nil
}

// syncRoute syncs vpc route and static routes
func (rc *RouteController) syncRoute(ctx context.Context, nodeName string) error {
	var (
		startTime = time.Now()
		patchErr  error
	)

	log.V(6).Infof(ctx, "sync route for node %v starts", nodeName)

	defer func() {
		if patchErr != nil {
			log.Errorf(ctx, "update networking condition for node %v error: %v", nodeName, patchErr)
		}
		log.V(6).Infof(ctx, "sync route for node %v ends (%v)", nodeName, time.Since(startTime))
	}()

	// only create vpc route for our own node
	if rc.enableVPCRoute && nodeName == rc.nodeName {
		thisNode, err := rc.kubeClient.CoreV1().Nodes().Get(ctx, rc.nodeName, metav1.GetOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to get k8s node %v: %v", rc.nodeName, err)
			return err
		}

		podCIDRs, err := rc.getPodCIDRs(ctx)
		if err != nil {
			log.Errorf(ctx, "failed to get pod cidrs for node %s: %v", rc.nodeName, err)
			return err
		}
		log.Infof(ctx, "node %v has podCIDRs %s", rc.nodeName, podCIDRs)

		if !rc.shouldEnsuringRoute(ctx) {
			log.Infof(ctx, "node %v skipped ensuring vpc route", rc.nodeName)
			return nil
		}

		rc.eventRecorder.Eventf(rc.nodeRef, v1.EventTypeNormal, "EnsuringVPCRoute", "Ensuring VPC route for node %v", rc.nodeName)

		err = rc.ensureVPCRoute(ctx, thisNode)
		// we only taint node if:
		// 1. create route failed due to quota
		// 2. NetworkUnavailable not exists or true.
		if err != nil && cloud.IsErrorQuotaLimitExceeded(err) &&
			k8sutil.GetNetworkingCondition(ctx, rc.kubeClient, rc.nodeName) != v1.ConditionFalse {
			patchErr = k8sutil.UpdateNetworkingCondition(
				ctx,
				rc.kubeClient,
				rc.nodeName,
				false,
				"RouteCreated",
				"NoRouteCreated",
				"CCE RouteController created a route",
				"CCE RouteController failed to create a route",
			)
		}

		if err == nil {
			patchErr = k8sutil.UpdateNetworkingCondition(
				ctx,
				rc.kubeClient,
				rc.nodeName,
				true,
				"RouteCreated",
				"NoRouteCreated",
				"CCE RouteController created a route",
				"CCE RouteController failed to create a route",
			)
		}

		if err != nil {
			log.Errorf(ctx, "sync route for node %v error: %v", nodeName, err)
			rc.eventRecorder.Eventf(rc.nodeRef, v1.EventTypeWarning, "EnsuringVPCRoute", "Error ensure VPC route for node %v: %v", rc.nodeName, err)
			return err
		}

		rc.eventRecorder.Eventf(rc.nodeRef, v1.EventTypeNormal, "EnsuringVPCRoute", "Ensuring VPC route for node %v succeed", rc.nodeName)
	}
	return nil
}

func (rc *RouteController) shouldEnsuringRoute(ctx context.Context) bool {
	if k8sutil.GetNetworkingCondition(ctx, rc.kubeClient, rc.nodeName) == v1.ConditionFalse {
		p, err := strconv.ParseFloat(os.Getenv(ensureVPCRoutePossibilityEnvKey), 64)
		if err != nil {
			p = 0.05
		}
		if !diceWithPossibility(p) {
			return false
		}
	}

	return true
}

func diceWithPossibility(p float64) bool {
	if p < 0 || p > 1 {
		p = 1
	}

	const (
		n = 1000
	)
	return rand.Intn(n) < (int)(n*p)
}

func isNotFoundError(err error) bool {
	return strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "not exist")
}

func (rc *RouteController) isResponsibleForRoute(route vpc.RouteRule) (bool, error) {
	_, cidr, err := net.ParseCIDR(route.DestinationAddress)
	if err != nil {
		return false, err
	}

	// check route cidr is in cluster container network cidr
	lastIP := make([]byte, len(cidr.IP))
	for i := range lastIP {
		lastIP[i] = cidr.IP[i] | ^cidr.Mask[i]
	}
	isIPv4ResponsibleFor := false
	isIPv6ResponsibleFor := false
	if rc.containerNetworkCIDRIPv4 != "" {
		_, ipv4CIDR, err := net.ParseCIDR(rc.containerNetworkCIDRIPv4)
		if err != nil {
			return false, err
		}
		if ipv4CIDR.Contains(cidr.IP) || ipv4CIDR.Contains(lastIP) {
			isIPv4ResponsibleFor = true
		}
	}
	if rc.containerNetworkCIDRIPv6 != "" {
		_, ipv6CIDR, err := net.ParseCIDR(rc.containerNetworkCIDRIPv6)
		if err != nil {
			return false, err
		}
		if ipv6CIDR.Contains(cidr.IP) || ipv6CIDR.Contains(lastIP) {
			isIPv6ResponsibleFor = true
		}
	}
	if !isIPv4ResponsibleFor && !isIPv6ResponsibleFor {
		return false, nil
	}

	if isIPv4ResponsibleFor && (route.NexthopType == "custom" && route.SourceAddress == "0.0.0.0/0" && route.NexthopId == rc.hostInstanceID) {
		return true, nil
	}

	if isIPv6ResponsibleFor && (route.NexthopType == "custom" && route.SourceAddress == "::/0" && route.NexthopId == rc.hostInstanceID) {
		return true, nil
	}

	return false, nil
}

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

package linux

import (
	"context"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/counter"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/vishvananda/netlink"
)

const (
	wildcardIPv4 = "0.0.0.0"
	wildcardIPv6 = "0::0"
)

const (
	neighFileName = "neigh-link.json"
)

// NeighLink contains the details of a NeighLink
type NeighLink struct {
	Name string `json:"link-name"`
}

type noOPNodeHandler struct {
	mutex                lock.Mutex
	isInitialized        bool
	nodeConfig           datapath.LocalNodeConfiguration
	nodeAddressing       types.NodeAddressing
	nodes                map[nodeTypes.Identity]*nodeTypes.Node
	enableNeighDiscovery bool
	neighLock            lock.Mutex // protects neigh* fields below
	neighDiscoveryLinks  []netlink.Link
	neighNextHopByNode4  map[nodeTypes.Identity]map[string]string // val = (key=link, value=string(net.IP))
	neighNextHopByNode6  map[nodeTypes.Identity]map[string]string // val = (key=link, value=string(net.IP))
	// All three mappings below hold both IPv4 and IPv6 entries.
	neighNextHopRefCount   counter.StringCounter
	neighByNextHop         map[string]*netlink.Neigh // key = string(net.IP)
	neighLastPingByNextHop map[string]time.Time      // key = string(net.IP)

	ipsecMetricCollector prometheus.Collector
}

func (n *noOPNodeHandler) NodeConfigurationChanged(config datapath.LocalNodeConfiguration) error {
	return nil
}

// NewNodeHandler returns a new node handler to handle node events and
// implement the implications in the Linux datapath
func NewNodeHandler(nodeAddressing types.NodeAddressing) datapath.NodeHandler {
	return &noOPNodeHandler{
		nodeAddressing:         nodeAddressing,
		nodes:                  map[nodeTypes.Identity]*nodeTypes.Node{},
		neighNextHopByNode4:    map[nodeTypes.Identity]map[string]string{},
		neighNextHopByNode6:    map[nodeTypes.Identity]map[string]string{},
		neighNextHopRefCount:   counter.StringCounter{},
		neighByNextHop:         map[string]*netlink.Neigh{},
		neighLastPingByNextHop: map[string]time.Time{},
	}
}

func (n *noOPNodeHandler) NodeAdd(newNode nodeTypes.Node) error {
	return nil
}

func (n *noOPNodeHandler) NodeUpdate(oldNode, newNode nodeTypes.Node) error {

	return nil
}

func (n *noOPNodeHandler) NodeDelete(oldNode nodeTypes.Node) error {

	return nil
}

// NodeValidateImplementation is called to validate the implementation of the
// node in the datapath
func (n *noOPNodeHandler) NodeValidateImplementation(nodeToValidate nodeTypes.Node) error {
	return nil
}

// NodeNeighDiscoveryEnabled returns whether node neighbor discovery is enabled
func (n *noOPNodeHandler) NodeNeighDiscoveryEnabled() bool {
	return n.enableNeighDiscovery
}

// NodeNeighborRefresh is called to refresh node neighbor table.
// This is currently triggered by controller neighbor-table-refresh
func (n *noOPNodeHandler) NodeNeighborRefresh(ctx context.Context, nodeToRefresh nodeTypes.Node) {

}

func (n *noOPNodeHandler) NodeCleanNeighborsLink(l netlink.Link, migrateOnly bool) bool {
	return true
}

// NodeCleanNeighbors cleans all neighbor entries of previously used neighbor
// discovery link interfaces. If migrateOnly is true, then NodeCleanNeighbors
// cleans old entries by trying to convert PERMANENT to dynamic, externally
// learned ones. If set to false, then it removes all PERMANENT or externally
// learned ones, e.g. when the agent got restarted and changed the state from
// `n.enableNeighDiscovery = true` to `n.enableNeighDiscovery = false`.
//
// Also, NodeCleanNeighbors is called after kubeapi server resync, so we have
// the full picture of all nodes. If there are any externally learned neighbors
// not in neighLastPingByNextHop, then we delete them as they could be stale
// neighbors from a previous agent run where in the meantime the given node was
// deleted (and the new agent instance did not see the delete event during the
// down/up cycle).
func (n *noOPNodeHandler) NodeCleanNeighbors(migrateOnly bool) {

}

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
	"os"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath"

	"github.com/go-openapi/strfmt"
	versionapi "k8s.io/apimachinery/pkg/version"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/backoff"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	k8smetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	nodeTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/node/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rand"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/status"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/version"
)

const (
	// k8sVersionCheckInterval is the interval in which the Kubernetes
	// version is verified even if connectivity is given
	k8sVersionCheckInterval = 15 * time.Minute

	// k8sMinimumEventHearbeat is the time interval in which any received
	// event will be considered proof that the apiserver connectivity is
	// healthty
	k8sMinimumEventHearbeat = time.Minute
)

var randGen = rand.NewSafeRand(time.Now().UnixNano())

type k8sVersion struct {
	version          string
	lastVersionCheck time.Time
	lock             lock.Mutex
}

func (k *k8sVersion) cachedVersion() (string, bool) {
	k.lock.Lock()
	defer k.lock.Unlock()

	if time.Since(k8smetrics.LastInteraction.Time()) > k8sMinimumEventHearbeat {
		return "", false
	}

	if k.version == "" || time.Since(k.lastVersionCheck) > k8sVersionCheckInterval {
		return "", false
	}

	return k.version, true
}

func (k *k8sVersion) update(version *versionapi.Info) string {
	k.lock.Lock()
	defer k.lock.Unlock()

	k.version = fmt.Sprintf("%s.%s (%s) [%s]", version.Major, version.Minor, version.GitVersion, version.Platform)
	k.lastVersionCheck = time.Now()
	return k.version
}

var k8sVersionCache k8sVersion

func (d *Daemon) getK8sStatus() *models.K8sStatus {
	if !k8s.IsEnabled() {
		return &models.K8sStatus{State: models.StatusStateDisabled}
	}

	version, valid := k8sVersionCache.cachedVersion()
	if !valid {
		k8sVersion, err := k8s.Client().Discovery().ServerVersion()
		if err != nil {
			return &models.K8sStatus{State: models.StatusStateFailure, Msg: err.Error()}
		}

		version = k8sVersionCache.update(k8sVersion)
	}

	k8sStatus := &models.K8sStatus{
		State:          models.StatusStateOk,
		Msg:            version,
		K8sAPIVersions: d.k8sWatcher.GetAPIGroups(),
	}

	return k8sStatus
}

func (d *Daemon) getCNIChainingStatus() *models.CNIChainingStatus {
	// CNI chaining is enabled only from CILIUM_CNI_CHAINING_MODE env
	mode := os.Getenv("CILIUM_CNI_CHAINING_MODE")
	if len(mode) == 0 {
		mode = models.CNIChainingStatusModeNone
	}
	return &models.CNIChainingStatus{
		Mode: mode,
	}
}

type getHealthz struct {
	daemon *Daemon
}

func (d *Daemon) getNodeStatus() *models.ClusterStatus {
	clusterStatus := models.ClusterStatus{
		Self: nodeTypes.GetAbsoluteNodeName(),
	}
	for _, node := range d.nodeDiscovery.Manager.GetNodes() {
		clusterStatus.Nodes = append(clusterStatus.Nodes, node.GetModel())
	}
	return &clusterStatus
}

// clientGCTimeout is the time for which the clients are kept. After timeout
// is reached, clients will be cleaned up.
const clientGCTimeout = 15 * time.Minute

type clusterNodesClient struct {
	// mutex to protect the client against concurrent access
	lock.RWMutex
	lastSync time.Time
	*models.ClusterNodeStatus
}

func (c *clusterNodesClient) NodeConfigurationChanged(config datapath.LocalNodeConfiguration) error {
	return nil
}

func (c *clusterNodesClient) NodeAdd(newNode nodeTypes.Node) error {
	c.Lock()
	c.NodesAdded = append(c.NodesAdded, newNode.GetModel())
	c.Unlock()
	return nil
}

func (c *clusterNodesClient) NodeUpdate(oldNode, newNode nodeTypes.Node) error {
	c.Lock()
	defer c.Unlock()

	// If the node is on the added list, just update it
	for i, added := range c.NodesAdded {
		if added.Name == newNode.Fullname() {
			c.NodesAdded[i] = newNode.GetModel()
			return nil
		}
	}

	// otherwise, add the new node and remove the old one
	c.NodesAdded = append(c.NodesAdded, newNode.GetModel())
	c.NodesRemoved = append(c.NodesRemoved, oldNode.GetModel())
	return nil
}

func (c *clusterNodesClient) NodeDelete(node nodeTypes.Node) error {
	c.Lock()
	// If the node was added/updated and removed before the clusterNodesClient
	// was aware of it then we can safely remove it from the list of added
	// nodes and not set it in the list of removed nodes.
	found := -1
	for i, added := range c.NodesAdded {
		if added.Name == node.Fullname() {
			found = i
		}
	}
	if found != -1 {
		c.NodesAdded = append(c.NodesAdded[:found], c.NodesAdded[found+1:]...)
	} else {
		c.NodesRemoved = append(c.NodesRemoved, node.GetModel())
	}
	c.Unlock()
	return nil
}

func (c *clusterNodesClient) NodeValidateImplementation(node nodeTypes.Node) error {
	// no-op
	return nil
}

func (c *clusterNodesClient) NodeNeighDiscoveryEnabled() bool {
	// no-op
	return false
}

func (c *clusterNodesClient) NodeNeighborRefresh(ctx context.Context, node nodeTypes.Node) {
	// no-op
	return
}

func (c *clusterNodesClient) NodeCleanNeighbors(migrateOnly bool) {
	// no-op
	return
}

// getStatus returns the daemon status. If brief is provided a minimal version
// of the StatusResponse is provided.
func (d *Daemon) getStatus(brief bool) models.StatusResponse {
	staleProbes := d.statusCollector.GetStaleProbes()
	stale := make(map[string]strfmt.DateTime, len(staleProbes))
	for probe, startTime := range staleProbes {
		stale[probe] = strfmt.DateTime(startTime)
	}

	d.statusCollectMutex.RLock()
	defer d.statusCollectMutex.RUnlock()

	var sr models.StatusResponse
	if brief {
		var minimalControllers models.ControllerStatuses
		if d.statusResponse.Controllers != nil {
			for _, c := range d.statusResponse.Controllers {
				if c.Status == nil {
					continue
				}
				// With brief, the client should only care if a single controller
				// is failing and its status so we don't need to continuing
				// checking for failure messages for the remaining controllers.
				if c.Status.LastFailureMsg != "" {
					minimalControllers = append(minimalControllers, c.DeepCopy())
					break
				}
			}
		}
		sr = models.StatusResponse{
			Controllers: minimalControllers,
		}
	} else {
		// d.statusResponse contains references, so we do a deep copy to be able to
		// safely use sr after the method has returned
		sr = *d.statusResponse.DeepCopy()
	}

	sr.Stale = stale

	// CCEVersion definition
	ver := version.GetCCEVersion()
	cceVer := fmt.Sprintf("%s (v%s-%s)", ver.Version, ver.Version, ver.Revision)

	switch {
	case len(sr.Stale) > 0:
		msg := "Stale status data"
		sr.Cce = &models.Status{
			State: models.StatusStateWarning,
			Msg:   fmt.Sprintf("%s    %s", cceVer, msg),
		}
	case d.statusResponse.ContainerRuntime != nil && d.statusResponse.ContainerRuntime.State != models.StatusStateOk:
		msg := "Container runtime is not ready"
		if d.statusResponse.ContainerRuntime.State == models.StatusStateDisabled {
			msg = "Container runtime is disabled"
		}
		sr.Cce = &models.Status{
			State: d.statusResponse.ContainerRuntime.State,
			Msg:   fmt.Sprintf("%s    %s", cceVer, msg),
		}

	default:
		sr.Cce = &models.Status{
			State: models.StatusStateOk,
			Msg:   cceVer,
		}
	}

	return sr
}

func (d *Daemon) startStatusCollector() {
	probes := []status.Probe{
		{
			Name: "check-locks",
			Probe: func(ctx context.Context) (interface{}, error) {
				// Try to acquire a couple of global locks to have the status API fail
				// in case of a deadlock on these locks
				option.Config.ConfigPatchMutex.Lock()
				option.Config.ConfigPatchMutex.Unlock()
				return nil, nil
			},
			OnStatusUpdate: func(status status.Status) {
				d.statusCollectMutex.Lock()
				defer d.statusCollectMutex.Unlock()
				// FIXME we have no field for the lock status
			},
		},
		{
			Name: "kubernetes",
			Interval: func(failures int) time.Duration {
				if failures > 0 {
					// While failing, we want an initial
					// quick retry with exponential backoff
					// to avoid continuous load on the
					// apiserver
					return backoff.CalculateDuration(5*time.Second, 2*time.Minute, 2.0, false, failures)
				}

				// The base interval is dependant on the
				// cluster size. One status interval does not
				// automatically translate to an apiserver
				// interaction as any regular apiserver
				// interaction is also used as an indication of
				// successful connectivity so we can continue
				// to be fairly aggressive.
				//
				// 1     |    7s
				// 2     |   12s
				// 4     |   15s
				// 64    |   42s
				// 512   | 1m02s
				// 2048  | 1m15s
				// 8192  | 1m30s
				// 16384 | 1m32s
				return d.nodeDiscovery.Manager.ClusterSizeDependantInterval(10 * time.Second)
			},
			Probe: func(ctx context.Context) (interface{}, error) {
				return d.getK8sStatus(), nil
			},
			OnStatusUpdate: func(status status.Status) {
				d.statusCollectMutex.Lock()
				defer d.statusCollectMutex.Unlock()
			},
		},
		{
			Name: "controllers",
			Probe: func(ctx context.Context) (interface{}, error) {
				return controller.GetGlobalStatus(), nil
			},
			OnStatusUpdate: func(status status.Status) {
				d.statusCollectMutex.Lock()
				defer d.statusCollectMutex.Unlock()

				// ControllerStatuses has no way to report errors
				if status.Err == nil {
					if s, ok := status.Data.(models.ControllerStatuses); ok {
						d.statusResponse.Controllers = s
					}
				}
			},
		},
	}

	d.statusCollector = status.NewCollector(probes, status.Config{})
	return
}

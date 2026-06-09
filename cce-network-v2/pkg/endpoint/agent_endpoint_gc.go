package endpoint

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	bceutils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/utils"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	netlinkwrapper "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/netlinkwrapper"
	nswrapper "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/nswrapper"
)

// init netlinkImpl from netlinkwrapper.
var netlinkImpl = netlinkwrapper.NewNetLink()

// init nsImpl from nswrapper.
var nsImpl = nswrapper.NewNS()

type agentGCer struct {
	ea       *EndpointAllocator
	logEntry *logrus.Entry

	expiredIPMap  map[string]*ipOwnerTime
	expiredCepMap map[string]time.Time
}

func newAgentGcer(ea *EndpointAllocator) *agentGCer {
	return &agentGCer{
		ea:            ea,
		logEntry:      allocatorLog.WithField("module", "gcer"),
		expiredIPMap:  make(map[string]*ipOwnerTime),
		expiredCepMap: make(map[string]time.Time),
	}
}

// Periodically run garbage collection. When the pod on the node has been killed,
// the IP occupied by the pod should be released
// warning 1. do not allocate any ips while running this function
func (gcer *agentGCer) gc(ctx context.Context) error {
	gcer.dynamicEndpointsGC()
	gcer.dynamicUsedIPsGC()
	gcer.pstsFixedIPEndpointsGC()
	return nil
}

func (gcer *agentGCer) dynamicEndpointsGC() {
	gcer.logEntry.WithFields(logrus.Fields{
		"expiredCepMap": logfields.Json(gcer.expiredCepMap),
	}).Debug("dump local expiredCepMap result")

	eps, err := gcer.ea.cceEndpointClient.List()
	if err != nil {
		gcer.logEntry.WithError(err).Error("list endpoint error")
		return
	}

	now := time.Now()
	for _, ep := range eps {
		// Skip PSTS endpoints (handled by pstsFixedIPEndpointsGC) and RDMA endpoints
		if IsPSTSEndpoint(ep) || bceutils.IsCCERdmaEndpointName(ep.Name) {
			continue
		}
		_, podName := GetPodNameFromCEP(ep)
		_, err := gcer.ea.podClient.Get(ep.Namespace, podName)
		key := fmt.Sprintf("%s/%s", ep.Namespace, ep.Name)

		if kerrors.IsNotFound(err) {
			lastTime, ok := gcer.expiredCepMap[key]
			if ok && lastTime.Add(gcer.ea.c.GetCCEEndpointGC()*4).Before(now) {
				gcer.ea.tryDeleteEndpointAfterPodDeleted(ep, true, gcer.logEntry)
				delete(gcer.expiredCepMap, key)
			}
			if !ok && isThisCEPReadyForDelete(gcer.logEntry, ep, false) {
				gcer.expiredCepMap[key] = now
			}
		}
	}

	for k := range gcer.expiredCepMap {
		namespace, name, _ := cache.SplitMetaNamespaceKey(k)
		cep, _ := gcer.ea.cceEndpointClient.Get(namespace, name)
		if cep == nil {
			delete(gcer.expiredCepMap, k)
		}
	}
}

type ipOwnerTime struct {
	owner      string
	time       time.Time
	lastVisted time.Time
}

func (gcer *agentGCer) dynamicUsedIPsGC() {
	now := time.Now()
	allocv4, allocv6, _ := gcer.ea.dynamicIPAM.Dump()
	gcer.logEntry.WithFields(logrus.Fields{
		"allocv4":      logfields.Json(allocv4),
		"allocv6":      logfields.Json(allocv6),
		"expiredIPMap": logfields.Json(gcer.expiredIPMap),
	}).Debug("dump local ipam result")

	releaseExpiredIPs := func(alloc map[string]string) {
		if len(alloc) == 0 {
			return
		}
		for addr, owner := range alloc {
			var (
				cep *ccev2.CCEEndpoint
				err error
			)

			scopedLog := gcer.logEntry.WithFields(logrus.Fields{
				"ip":    addr,
				"owner": owner,
				"step":  "dynamicUsedIPsGC",
			})

			// skip the router ip used by cce and cilium
			if strings.HasSuffix(owner, ipamOption.IPAMVpcRoute) {
				delete(gcer.expiredIPMap, addr)
				continue
			}
			namespace, name, err := cache.SplitMetaNamespaceKey(owner)
			if err != nil {
				scopedLog.WithError(err).Error("split owner error")
				continue
			}

			// check ip be realloed to another pod
			lastTime, ok := gcer.expiredIPMap[addr]
			if ok && lastTime.owner != owner {
				delete(gcer.expiredIPMap, addr)
				continue
			}

			// Unified CEP checking logic: determine correct CEP name and log type based on network type
			var cepName, networkType string
			if gcer.ea.isRDMAMode() {
				// RDMA mode: name is podName, need to generate RDMA CEP name
				cepName = bceutils.GetCEPNameFromPodName(true, name, gcer.ea.rdmaAttr.primaryMacAddress)
				networkType = "rdma"
			} else {
				// Ethernet mode: name is CEP name
				cepName = name
				networkType = "ethernet"
			}

			cep, err = gcer.ea.cceEndpointClient.Get(namespace, cepName)
			if err == nil && cep != nil {
				containersIPs := false
				if cep.Status.Networking != nil && len(cep.Status.Networking.Addressing) != 0 {
					for _, pair := range cep.Status.Networking.Addressing {
						if pair.IP == addr {
							containersIPs = true
							break
						}
					}
				}
				if containersIPs {
					scopedLog.Debugf("FROM GC dynamicUsedIPsGC: skip gc %s ip %s, because it belongs to cep: %s", networkType, addr, cep.Name)
					delete(gcer.expiredIPMap, addr)
					continue
				}
				// if cep exists but not contain this ip, this ip will be considered for gc
				scopedLog.Debugf("FROM GC dynamicUsedIPsGC: %s ip %s does not belong to cep: %s, will be considered for gc", networkType, addr, cep.Name)
			}
			if err != nil && !kerrors.IsNotFound(err) {
				scopedLog.WithError(err).Errorf("get %s cep error", networkType)
				delete(gcer.expiredIPMap, addr)
				continue
			}

			if !ok && cep == nil {
				gcer.expiredIPMap[addr] = &ipOwnerTime{
					owner:      owner,
					time:       now,
					lastVisted: now,
				}
				scopedLog.Info("add expired ip")
			} else {
				lastTime.lastVisted = now
			}
		}
	}
	releaseExpiredIPs(allocv4)
	releaseExpiredIPs(allocv6)

	for addr, onwerTime := range gcer.expiredIPMap {
		if onwerTime.time.Add(gcer.ea.c.GetCCEEndpointGC() * 6).After(now) {
			continue
		}

		// ip may be used
		if onwerTime.lastVisted != now {
			delete(gcer.expiredIPMap, addr)
			continue
		}

		logger := gcer.logEntry.WithFields(logrus.Fields{
			"ip":           addr,
			"reason":       "dynamicUsedIPsGC",
			"onwer":        onwerTime.owner,
			"lastUsedTime": onwerTime.time.Format(time.RFC3339),
		})
		err := gcer.ea.dynamicIPAM.ReleaseIPString(addr)
		if err != nil {
			logger.WithError(err).Info("release ip after cep deleted")
		} else {
			logger.Info("release ip after cep deleted")
		}
	}
}

// pstsFixedIPEndpointsGC handles GC for PSTS Fixed mode endpoints with TTL
// When TTL expires, delete the CEP (which triggers Operator to unreserve single IP if needed)
func (gcer *agentGCer) pstsFixedIPEndpointsGC() {
	eps, err := gcer.ea.cceEndpointClient.List()
	if err != nil {
		gcer.logEntry.WithError(err).Error("list endpoint error in pstsFixedIPEndpointsGC")
		return
	}

	now := time.Now()
	for _, ep := range eps {
		// Only process PSTS endpoints
		if !IsPSTSEndpoint(ep) {
			continue
		}

		// Only process Fixed mode endpoints
		if ep.Spec.Network.IPAllocation.Type != ccev2.IPAllocTypeFixed {
			continue
		}

		// Only process endpoints with TTL set
		if ep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted == nil {
			continue
		}

		// Only process endpoints in PodDeleted state
		if ep.Status.State != string(models.EndpointStatePodDeleted) {
			continue
		}

		scopedLog := gcer.logEntry.WithFields(logrus.Fields{
			"namespace": ep.Namespace,
			"name":      ep.Name,
			"step":      "pstsFixedIPEndpointsGC",
		})

		// Find the PodDeleted timestamp from status log
		var podDeletedTime time.Time
		found := false
		if ep.Status.Log != nil && len(ep.Status.Log) > 0 {
			for i := len(ep.Status.Log) - 1; i >= 0; i-- {
				if ep.Status.Log[i].State != models.EndpointStatePodDeleted {
					continue
				}
				parsedTime, parseErr := time.Parse(time.RFC3339, ep.Status.Log[i].Timestamp)
				if parseErr != nil {
					scopedLog.WithError(parseErr).Error("failed to parse PodDeleted timestamp")
					continue
				}
				podDeletedTime = parsedTime
				found = true
				break
			}
		}

		if !found || podDeletedTime.IsZero() {
			scopedLog.Warn("PodDeleted timestamp not found, skip GC")
			continue
		}

		// Check if TTL has expired
		ttlSeconds := *ep.Spec.Network.IPAllocation.TTLSecondsAfterDeleted
		expireTime := podDeletedTime.Add(time.Duration(ttlSeconds) * time.Second)

		if now.After(expireTime) {
			scopedLog.WithFields(logrus.Fields{
				"podDeletedTime": podDeletedTime.Format(time.RFC3339),
				"ttlSeconds":     ttlSeconds,
				"expireTime":     expireTime.Format(time.RFC3339),
			}).Info("PSTS Fixed mode CEP TTL expired, deleting CEP")

			// Delete CEP - this will trigger Operator's Delete() method
			// which will unreserve single IP if needed (for Fixed mode without range)
			err := gcer.ea.deleteEndpointSafely(ep)
			if err != nil {
				scopedLog.WithError(err).Error("failed to delete expired PSTS Fixed mode CEP")
			} else {
				scopedLog.Info("successfully deleted expired PSTS Fixed mode CEP")
			}
		}
	}
}

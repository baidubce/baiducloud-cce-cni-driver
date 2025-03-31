package endpoint

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"

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
		if IsFixedIPEndpoint(ep) || IsPSTSEndpoint(ep) {
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
			if !ok && isThisCEPReadyForDelete(gcer.logEntry, ep) {
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

			if gcer.ea.isRDMAMode() {
				// rdma mode skip gc ips if cep is exsit
				cep, err = gcer.ea.cceEndpointClient.Get(namespace, name)
				if !kerrors.IsNotFound(err) {
					delete(gcer.expiredIPMap, addr)
					continue
				}
			} else {
				// ethernet skip if cep do not have containers ip
				cep, err = gcer.ea.cceEndpointClient.Get(namespace, name)
				if err == nil && cep != nil {
					containersIPs := false
					if cep.Status.Networking != nil && len(cep.Status.Networking.Addressing) != 0 {
						for _, pair := range cep.Status.Networking.Addressing {
							if pair.IP == addr {
								containersIPs = true
							}
						}
					}
					if containersIPs {
						delete(gcer.expiredIPMap, addr)
						continue
					}
				}
				if err != nil && !kerrors.IsNotFound(err) {
					scopedLog.WithError(err).Error("get cep error")
					delete(gcer.expiredIPMap, addr)
					continue
				}
			}

			if !ok && (cep == nil || isThisCEPReadyForDelete(scopedLog, cep)) {
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

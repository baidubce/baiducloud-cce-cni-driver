package eri

import (
	"context"
	"fmt"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"time"
)

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	log.Infof(context.TODO(), "start gc by eri ipam, gcPeriod is %v", ipam.gcPeriod)
	err := wait.PollImmediateUntil(wait.Jitter(ipam.gcPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()
		log.Infof(ctx, "gc by eri ipam start")

		// release mwep if pod not found
		podErr := ipam.gcLeakedPod(ctx)
		if podErr != nil {
			return false, nil
		}

		nodeErr := ipam.gcDeletedNode(ctx)
		if nodeErr != nil {
			return false, nil
		}

		ipErr := ipam.gcLeakedIP(ctx)
		if ipErr != nil {
			return false, nil
		}

		log.Infof(ctx, "gc by eri ipam end")
		return false, nil
	}, stopCh)

	if err != nil {
		return err
	}
	return nil
}

// release ip and mwep when pod not found
func (ipam *IPAM) gcLeakedPod(ctx context.Context) error {
	mwepSelector, selectorErr := mwepListerSelector()
	if selectorErr != nil {
		log.Errorf(ctx, "make mwep lister selector has error: %v", selectorErr)
		return selectorErr
	}

	mwepList, mwepErr := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().List(mwepSelector)
	if mwepErr != nil {
		log.Errorf(ctx, "gc: error list mwep in cluster: %v", mwepErr)
		return mwepErr
	}
	log.Infof(ctx, "list mwepList count is %d ", len(mwepList))

	for _, mwep := range mwepList {
		_, podErr := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(mwep.Namespace).Get(mwep.Name)
		if podErr == nil {
			// pod exist
			continue
		}
		if !errors.IsNotFound(podErr) {
			// get pod failed
			log.Errorf(ctx, "gc: get pod (%s/%s) failed: %v", mwep.Namespace, mwep.Name, podErr)
			continue
		}
		// pod not found. delete mwep
		msg := fmt.Sprintf("gc: pod not found, try to release leaked mwep (%s/%s)", mwep.Namespace, mwep.Name)
		log.Info(ctx, msg)

		// add event
		ipam.eventRecorder.Event(&v1.ObjectReference{
			Kind: "mwep",
			Name: fmt.Sprintf("%v %v", mwep.Namespace, mwep.Name),
		}, v1.EventTypeWarning, "PodLeaked", msg)

		//  delete ip, delete mwep crd
		releaseErr := ipam.releaseIPByMwep(ctx, mwep)
		if releaseErr != nil {
			log.Errorf(ctx, "gc: release mwep (%s/%s) and ip failed: %s", mwep.Namespace, mwep.Name, releaseErr)
		}
	}
	return nil
}

// delete node from cache when node not found
func (ipam *IPAM) gcDeletedNode(ctx context.Context) error {
	for _, node := range ipam.nodeCache {
		_, nodeErr := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(node.Name)
		if nodeErr == nil {
			// node exist
			continue
		}
		if !errors.IsNotFound(nodeErr) {
			// get node failed
			log.Errorf(ctx, "gc: get node (%s) failed: %v", node.Name, nodeErr)
			continue
		}

		log.Infof(ctx, "detect node %v has been deleted, clean up datastore", node.Name)

		// clean up cache
		delErr := ipam.deleteNodeFromCache(node)
		if delErr != nil {
			log.Errorf(ctx, "gc: delete node %s from datastore failed: %v", node.Name, delErr)
		}
	}
	return nil
}

func (ipam *IPAM) deleteNodeFromCache(node *v1.Node) error {
	instanceID, insIDErr := util.GetInstanceIDFromNode(node)
	if insIDErr != nil {
		return fmt.Errorf("get instanceID for node (%s) error: %v", node.Name, insIDErr)
	}

	if _, exist := ipam.nodeCache[instanceID]; exist {
		delete(ipam.nodeCache, instanceID)
	}
	return nil
}

// release ip when ip not in mwep
func (ipam *IPAM) gcLeakedIP(ctx context.Context) error {
	for instanceID := range ipam.nodeCache {
		listArgs := enisdk.ListEniArgs{
			InstanceId: instanceID,
			VpcId:      ipam.vpcID,
		}
		eris, listErr := ipam.cloud.ListERIs(ctx, listArgs)
		if listErr != nil {
			log.Infof(ctx, "list eri for %s failed: %s", instanceID, listErr)
			if cloud.IsErrorRateLimit(listErr) {
				// wait for rate limit
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			continue
		}
		// get mwep for instanceID
		ipSet, ipErr := ipam.getIPSetForNode(ctx, instanceID)
		if ipErr != nil {
			log.Infof(ctx, "get ip set for %s failed: %s", instanceID, ipErr)
			continue
		}
		// delete eri ip when ip not in mwep
		ipam.gcOneNodeLeakedIP(ctx, instanceID, eris, ipSet)
	}

	return nil
}

func (ipam *IPAM) getIPSetForNode(ctx context.Context, instanceID string) (map[string]struct{}, error) {
	// list mwep
	mwepSelector, selectorErr := mwepListerSelector()
	if selectorErr != nil {
		log.Errorf(ctx, "make mwep lister selector has error: %v", selectorErr)
		return nil, selectorErr
	}
	mwepList, mwepErr := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().List(mwepSelector)
	if mwepErr != nil {
		log.Errorf(ctx, "gc: error list mwep in cluster: %v", mwepErr)
		return nil, mwepErr
	}
	log.Infof(ctx, "list mwepList count is %d ", len(mwepList))

	// collect ip for instanceID
	ipSet := make(map[string]struct{})
	for _, mwep := range mwepList {
		if mwep.Type != ipamgeneric.MwepTypeERI {
			continue
		}
		if mwep.InstanceID != instanceID {
			continue
		}
		for _, spec := range mwep.Spec {
			if _, exist := ipSet[spec.IP]; exist {
				continue
			}
			ipSet[spec.IP] = struct{}{}
		}
	}
	return ipSet, nil
}

func (ipam *IPAM) gcOneNodeLeakedIP(ctx context.Context, instanceID string, eriList []enisdk.Eni,
	ipSet map[string]struct{}) {
	for _, eriInfo := range eriList {
		log.Infof(ctx, "gc: try to gc leaked ip for eri %s of node %s", eriInfo.EniId, instanceID)
		for _, privateIP := range eriInfo.PrivateIpSet {
			if privateIP.Primary {
				continue
			}
			if _, exist := ipSet[privateIP.PrivateIpAddress]; exist {
				continue
			}
			log.Infof(ctx, "gc: privateIP %s not found in mwep, try to delete privateIP",
				privateIP.PrivateIpAddress)

			// delete ip
			deleteErr := ipam.cloud.DeletePrivateIP(ctx, privateIP.PrivateIpAddress, eriInfo.EniId)
			if deleteErr != nil {
				log.Errorf(ctx, "delete ip %s for eni %s failed: %s",
					privateIP.PrivateIpAddress, eriInfo.EniId, deleteErr)
			}
		}
	}
}

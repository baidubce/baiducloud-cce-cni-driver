package roce

import (
	"context"
	"fmt"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	log.Infof(context.TODO(), "start gc by roce ipam, gcPeriod is %v", ipam.gcPeriod)
	err := wait.PollImmediateUntil(wait.Jitter(ipam.gcPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()

		mwepSelector, err := mwepListerSelector()
		if err != nil {
			log.Errorf(ctx, "make mwep lister selector has error: %v", err)
			return false, nil
		}

		mwepList, err := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().List(mwepSelector)
		if err != nil {
			log.Errorf(ctx, "gc: error list mwep in cluster: %v", err)
			return false, nil
		}
		log.Infof(context.TODO(), "list mwepList count is %d ", len(mwepList))

		// release mwep if pod not found
		err = ipam.gcLeakedPod(ctx, mwepList)
		if err != nil {
			return false, nil
		}

		err = ipam.gcDeletedNode(ctx)
		if err != nil {
			return false, nil
		}

		// release ip when ip not in mwep
		err = ipam.gcLeakedIP(ctx)
		if err != nil {
			return false, nil
		}

		return false, nil
	}, stopCh)

	if err != nil {
		return err
	}
	return nil
}

// release ip when ip not in mwep
func (ipam *IPAM) gcLeakedIP(ctx context.Context) error {
	for instanceID := range ipam.nodeCache {
		result, err := ipam.cloud.GetHPCEniID(ctx, instanceID)
		if err != nil {
			log.Errorf(ctx, "gc getHPCEniID : get hPCEni privateIP has error %v", err)
			return err
		}
		// get mwep by instanceID
		ipSet, ipErr := ipam.getIPSetForNode(ctx, instanceID)
		if ipErr != nil {
			log.Infof(ctx, "get ip set for %s failed: %s", instanceID, ipErr)
			continue
		}
		// delete eri ip when ip not in mwep
		ipam.gcOneNodeLeakedIP(ctx, result.Result, ipSet)
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
		if mwep.Type != ipamgeneric.MwepTypeRoce {
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

func (ipam *IPAM) gcOneNodeLeakedIP(ctx context.Context, resultList []hpc.Result, ipSet map[string]struct{}) {
	for _, hpcResult := range resultList {
		for _, privateIP := range hpcResult.PrivateIPSet {
			if privateIP.Primary {
				continue
			}
			if _, exist := ipSet[privateIP.PrivateIPAddress]; exist {
				continue
			}
			log.Infof(ctx, "gc: privateIP %s not found in mwep, try to delete privateIP",
				privateIP.PrivateIPAddress)

			// delete ip
			if err := ipam.deleteRocePrivateIP(ctx, hpcResult.EniID, privateIP.PrivateIPAddress); err != nil {
				log.Errorf(ctx, "gc: failed to delete privateIP %s on %s for not found in mwep: %s",
					privateIP.PrivateIPAddress, hpcResult.EniID, err)
			}
		}
	}
}

func (ipam *IPAM) gcLeakedPod(ctx context.Context, mwepList []*v1alpha1.MultiIPWorkloadEndpoint) error {
	for _, mwep := range mwepList {
		_, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(mwep.Namespace).Get(mwep.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("gc: not found pod, try to release leaked mwep (%v %v)", mwep.Namespace, mwep.Name)
				log.Info(ctx, msg)

				ipam.eventRecorder.Event(&v1.ObjectReference{
					Kind: "mwep",
					Name: fmt.Sprintf("%v %v", mwep.Namespace, mwep.Name),
				}, v1.EventTypeWarning, "PodLeaked", msg)

				err = ipam.releasePrivateIP(context.Background(), mwep)
				if err != nil {
					log.Errorf(ctx, "gc: failed to release private IP for leaked pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
				}
				if err = ipam.gcMwepAndCache(ctx, mwep); err != nil {
					log.Errorf(ctx, "gc: failed to delete private IP for leaked pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
				}
			} else {
				log.Errorf(ctx, "gc: failed to get pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
			}
		}
	}
	return nil
}

func (ipam *IPAM) releasePrivateIP(ctx context.Context, mwep *v1alpha1.MultiIPWorkloadEndpoint) error {
	for _, mwepSpec := range mwep.Spec {
		if err := ipam.deleteRocePrivateIP(ctx, mwepSpec.EniID, mwepSpec.IP); err != nil {
			log.Errorf(ctx, "gc: failed to delete private IP %v on %v for leaked pod (%v %v): %v", mwepSpec.IP, mwepSpec.EniID, mwep.Namespace, mwep.Name, err)
			return err
		} else {
			log.Infof(ctx, "gc: delete private IP %v on %v for leaked pod (%v %v) successfully", mwepSpec.IP, mwepSpec.EniID, mwep.Namespace, mwep.Name)
		}
	}
	return nil
}

func (ipam *IPAM) gcMwepAndCache(ctx context.Context, mwep *v1alpha1.MultiIPWorkloadEndpoint) (err error) {
	err = ipam.tryDeleteMwep(ctx, mwep)
	if err != nil {
		err = fmt.Errorf("gc: error delete mwep form cluster (%v %v): %v", mwep.Namespace, mwep.Name, err)
		log.Errorf(ctx, err.Error())
		return
	}

	log.Infof(ctx, "gc: deleted mwep for pod (%v/%v) successfully", mwep.Namespace, mwep.Name)

	for _, spec := range mwep.Spec {
		ipam.removeIPFromCache(spec.IP, false)
	}

	ipam.removeEniFromLeakedCache(mwep.InstanceID)
	return err
}

func (ipam *IPAM) removeIPFromCache(ipAddr string, lockless bool) {
	if !lockless {
		ipam.lock.Lock()
		defer ipam.lock.Unlock()
	}
	delete(ipam.allocated, ipAddr)
}

func (ipam *IPAM) removeEniFromLeakedCache(instanceID string) {
	ipam.lock.Lock()
	defer ipam.lock.Unlock()

	delete(ipam.hpcEniCache, instanceID)
}

// Delete workload objects from the k8s cluster
func (ipam *IPAM) tryDeleteMwep(ctx context.Context, mwep *v1alpha1.MultiIPWorkloadEndpoint) (err error) {
	// remove finalizers
	mwep.Finalizers = nil
	_, err = ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(mwep.Namespace).Update(ctx, mwep, metav1.UpdateOptions{})
	if err != nil {
		log.Errorf(ctx, "tryDeleteWep failed to update wep for pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
		return err
	}
	// delete mwep
	if err := ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(mwep.Namespace).Delete(ctx, mwep.Name, *metav1.NewDeleteOptions(0)); err != nil {
		log.Errorf(ctx, "tryDeleteMwep failed to delete wep for orphaned pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
	} else {
		log.Infof(ctx, "tryDeleteMwep delete wep for orphaned pod (%v %v) successfully", mwep.Namespace, mwep.Name)
	}
	return nil
}

func (ipam *IPAM) gcDeletedNode(ctx context.Context) error {
	for _, node := range ipam.nodeCache {
		_, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(node.Name)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Infof(ctx, "detect node %v has been deleted, clean up datastore", node.Name)

				// clean up cache
				delErr := ipam.DeleteNodeFromCache(node)
				if delErr != nil {
					log.Errorf(ctx, "error delete node %v from datastore: %v", node.Name, delErr)
				}

				ipam.lock.Lock()
				if ch, ok := ipam.increasePoolEventChan[node.Name]; ok {
					delete(ipam.increasePoolEventChan, node.Name)
					close(ch)
					log.Infof(ctx, "clean up increase pool goroutine for node %v", node.Name)
				}
				ipam.lock.Unlock()

				continue
			}
			return err
		}
	}
	return nil
}

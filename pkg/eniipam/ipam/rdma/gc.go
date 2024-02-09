package rdma

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/rdma/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func (ipam *IPAM) gc(stopCh <-chan struct{}) error {
	log.Infof(context.TODO(), "start gc by roce ipam, gcPeriod is %v", ipam.gcPeriod)
	err := wait.PollImmediateUntil(wait.Jitter(ipam.gcPeriod, 0.5), func() (bool, error) {
		ctx := log.NewContext()

		// release mwep if pod not found
		err := ipam.gcLeakedPod(ctx)
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
	nodeList, nodeErr := ipam.listRDMANode()
	if nodeErr != nil {
		log.Errorf(ctx, "get node list fail, err %w", nodeErr)
		return nodeErr
	}
	for _, node := range nodeList {
		instanceID, idErr := util.GetInstanceIDFromNode(node)
		if idErr != nil {
			log.Errorf(ctx, "get instance id from node %s fail, err %w", node, idErr)
			continue
		}

		eriList, roceList, listErr := ipam.listENI(ctx, instanceID)
		if listErr != nil {
			log.Infof(ctx, "list eni for %s failed: %s", instanceID, listErr)
			if cloud.IsErrorRateLimit(listErr) {
				// wait for rate limit
				time.Sleep(wait.Jitter(rateLimitErrorSleepPeriod, rateLimitErrorJitterFactor))
			}
			continue
		}
		// get mwep by instanceID
		eriIPSet, roceIPSet, ipErr := ipam.getIPSetForNode(ctx, instanceID)
		if ipErr != nil {
			log.Infof(ctx, "get ip set for %s failed: %s", instanceID, ipErr)
			continue
		}
		// delete eni ip when ip not in mwep
		ipam.gcOneNodeLeakedIP(ctx, ipam.eriClient, eriList, eriIPSet)
		ipam.gcOneNodeLeakedIP(ctx, ipam.roceClient, roceList, roceIPSet)
	}

	return nil
}

func (ipam *IPAM) listRDMANode() ([]*v1.Node, error) {
	nodeSelector, selectorErr := ipam.nodeListerSelector()
	if selectorErr != nil {
		return nil, selectorErr
	}
	return ipam.kubeInformer.Core().V1().Nodes().Lister().List(nodeSelector)
}

func (ipam *IPAM) listENI(ctx context.Context, instanceID string) ([]client.EniResult, []client.EniResult, error) {
	eriList, eriErr := ipam.eriClient.ListEnis(ctx, ipam.vpcID, instanceID)
	if eriErr != nil {
		return nil, nil, eriErr
	}
	roceList, roceErr := ipam.roceClient.ListEnis(ctx, ipam.vpcID, instanceID)
	if roceErr != nil {
		return nil, nil, roceErr
	}
	return eriList, roceList, nil
}

func (ipam *IPAM) getIPSetForNode(ctx context.Context, instanceID string) (
	map[string]struct{}, map[string]struct{}, error) {
	// list mwep
	mwepList, mwepErr := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().List(labels.Everything())
	if mwepErr != nil {
		log.Errorf(ctx, "gc: error list mwep in cluster: %v", mwepErr)
		return nil, nil, mwepErr
	}
	log.Infof(ctx, "list mwepList count is %d ", len(mwepList))

	// collect ip for instanceID
	eriIPSet := make(map[string]struct{})
	roceIPSet := make(map[string]struct{})

	for _, mwep := range mwepList {
		if mwep.InstanceID != instanceID {
			continue
		}
		defaultType := mwep.Type
		for _, spec := range mwep.Spec {
			var ipSet map[string]struct{}
			ipType := defaultType
			if len(spec.Type) > 0 {
				ipType = spec.Type
			}
			if strings.EqualFold(ipType, ipam.eriClient.GetMwepType()) {
				ipSet = eriIPSet
			} else {
				ipSet = roceIPSet
			}

			if _, exist := ipSet[spec.IP]; exist {
				continue
			}
			ipSet[spec.IP] = struct{}{}
		}
	}
	return eriIPSet, roceIPSet, nil
}

func (ipam *IPAM) gcOneNodeLeakedIP(ctx context.Context, iaasClient client.IaaSClient, resultList []client.EniResult,
	ipSet map[string]struct{}) {
	for _, eniResult := range resultList {
		for _, privateIP := range eniResult.PrivateIPSet {
			if privateIP.Primary {
				continue
			}
			if _, exist := ipSet[privateIP.PrivateIPAddress]; exist {
				continue
			}
			log.Infof(ctx, "gc: privateIP %s not found in mwep, try to delete privateIP",
				privateIP.PrivateIPAddress)

			// delete ip
			if err := iaasClient.DeletePrivateIP(ctx, eniResult.EniID, privateIP.PrivateIPAddress); err != nil {
				log.Errorf(ctx, "gc: failed to delete privateIP %s on %s for not found in mwep: %s",
					privateIP.PrivateIPAddress, eniResult.EniID, err)
			}
		}
	}
}

func (ipam *IPAM) gcLeakedPod(ctx context.Context) error {
	mwepList, mwepErr := ipam.crdInformer.Cce().V1alpha1().MultiIPWorkloadEndpoints().Lister().List(labels.Everything())
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
	if err := ipam.crdClient.CceV1alpha1().MultiIPWorkloadEndpoints(mwep.Namespace).
		Delete(ctx, mwep.Name, *metav1.NewDeleteOptions(0)); err != nil {
		log.Errorf(ctx, "tryDeleteMwep failed to delete wep for orphaned pod (%v %v): %v", mwep.Namespace, mwep.Name, err)
	} else {
		log.Infof(ctx, "tryDeleteMwep delete wep for orphaned pod (%v %v) successfully", mwep.Namespace, mwep.Name)
	}
	return nil
}

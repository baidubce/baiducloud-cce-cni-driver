package bcc

import (
	"context"
	goerrors "errors"
	"fmt"
	"time"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/util"
	k8sutil "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	var ipResult = ""
	var addIPErrors []error
	var ipAddedENI *enisdk.Eni
	pod, err := ipam.kubeInformer.Core().V1().Pods().Lister().Pods(namespace).Get(name)
	if err != nil {
		return nil, err
	}

	// get node
	node, err := ipam.kubeInformer.Core().V1().Nodes().Lister().Get(pod.Spec.NodeName)
	if err != nil {
		return nil, err
	}

	node = node.DeepCopy()

	// check datastore node status
	err = wait.ExponentialBackoff(retry.DefaultRetry, func() (done bool, err error) {
		if !ipam.datastore.NodeExistsInStore(node.Name) {
			var (
				evt = &event{
					node: node,
					ctx:  ctx,
				}
				ch = make(chan *event)
			)

			ipam.lock.Lock()
			_, ok := ipam.buildDataStoreEventChan[evt.node.Name]
			if !ok {
				ipam.buildDataStoreEventChan[node.Name] = ch
				go ipam.handleRebuildNodeDatastoreEvent(ctx, evt.node, ch)
			}
			ch = ipam.buildDataStoreEventChan[evt.node.Name]
			ipam.lock.Unlock()

			ch <- evt
			return false, nil
		} else {
			return true, nil
		}
	})
	if err == wait.ErrWaitTimeout {
		return nil, fmt.Errorf("init node %v datastore error", node.Name)
	}

	// get node stats from store, to further check if pool is corrupted
	total, used, err := ipam.datastore.GetNodeStats(node.Name)
	if err != nil {
		msg := fmt.Sprintf("get node %v stats in datastore failed: %v", node, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}

	log.Infof(ctx, "total, used before allocate for node %v: %v %v", node.Name, total, used)

	// find out which enis are suitable to bind
	enis, err := ipam.findSuitableENIs(ctx, pod)
	if err != nil {
		log.Errorf(ctx, "failed to find a suitable eni for pod (%v %v): %v", namespace, name, err)
		return nil, err
	}
	suitableENINum := len(enis)

	wep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(namespace).Get(name)
	if err != nil && !errors.IsNotFound(err) {
		log.Errorf(ctx, "failed to get wep of pod (%v %v): %v", pod.Namespace, pod.Name, err)
		return nil, err
	}

	// IP needs to be allocated across subnets
	// In this case, the local IP address cache is not used
	if networking.IsSubnetTopologySpreadPod(pod) {
		return ipam.subnetTopologyAllocates(ctx, pod, enis, wep, node.Name, containerID)
	}

	// not a subnet topology spread pod, use node ippool to allocate ip address
	// use old wep
	if err == nil {
		ipToAllocate := wep.Spec.IP
		if !isFixIPStatefulSetPod(pod) {
			log.Warningf(ctx, "pod (%v %v) still has wep, but is not a fix-ip sts pod", namespace, name)
			ipToAllocate = ""
		}
		if ipToAllocate != "" {
			log.Infof(ctx, "try to reuse fix IP %v for pod (%v %v)", ipToAllocate, namespace, name)
		}
		for _, eni := range enis {
			if ipToAllocate == "" {
				ipResult, err = ipam.datastore.AllocatePodPrivateIPByENI(node.Name, eni.EniId)
				if err == nil {
					ipAddedENI = eni
					break
				}
			}

			ipResult, err = ipam.tryAllocateIPForFixIPPod(ctx, eni, wep, ipToAllocate, node, ipamgeneric.CCECniTimeout/time.Duration(suitableENINum))
			if err == nil {
				ipAddedENI = eni
				break
			} else {
				addErr := fmt.Errorf("error eni: %v, %v", eni.EniId, err.Error())
				addIPErrors = append(addIPErrors, addErr)
			}
		}

		if ipAddedENI == nil {
			return nil, fmt.Errorf("all %d enis binded cannot add IP %v: %v", len(enis), wep.Spec.IP, utilerrors.NewAggregate(addIPErrors))
		}

		if wep.Labels == nil {
			wep.Labels = make(map[string]string)
		}
		wep.Spec.ContainerID = containerID
		wep.Spec.IP = ipResult
		wep.Spec.ENIID = ipAddedENI.EniId
		wep.Spec.Mac = ipAddedENI.MacAddress
		wep.Spec.Node = pod.Spec.NodeName
		wep.Spec.SubnetID = ipAddedENI.SubnetId
		wep.Spec.UpdateAt = metav1.Time{Time: time.Unix(0, 0)}
		wep.Labels[ipamgeneric.WepLabelSubnetIDKey] = ipAddedENI.SubnetId
		wep.Labels[ipamgeneric.WepLabelInstanceTypeKey] = string(metadata.InstanceTypeExBCC)
		if k8sutil.IsStatefulSetPod(pod) {
			wep.Labels[ipamgeneric.WepLabelStsOwnerKey] = util.GetStsName(wep)
		}
		if pod.Annotations != nil {
			wep.Spec.EnableFixIP = pod.Annotations[StsPodAnnotationEnableFixIP]
			wep.Spec.FixIPDeletePolicy = pod.Annotations[StsPodAnnotationFixIPDeletePolicy]
		}
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(ctx, wep, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to update wep for pod (%v %v): %v", namespace, name, err)
			time.Sleep(minPrivateIPLifeTime)
			ipam.tryDeleteSubnetIPRetainAllocateCache(ctx, wep)
			return nil, err
		}
		ipam.lock.Lock()
		ipam.allocated[ipResult] = wep
		ipam.lock.Unlock()
		return wep, nil
	}

	// create a new wep
	log.Infof(ctx, "try to allocate IP and create wep for pod (%v %v)", pod.Namespace, pod.Name)

	idleIPs, _ := ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	log.Infof(ctx, "idle ip in datastore before allocate for node %v: %v", node.Name, idleIPs)

	ipResult, ipAddedENI, err = ipam.allocateIPFromLocalPool(ctx, node, enis)
	if err != nil {
		msg := fmt.Sprintf("error allocate private IP for pod(%s/%s): %v", namespace, name, err)
		log.Error(ctx, msg)
		return nil, goerrors.New(msg)
	}
	wep = &v1alpha1.WorkloadEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels: map[string]string{
				ipamgeneric.WepLabelSubnetIDKey:     ipAddedENI.SubnetId,
				ipamgeneric.WepLabelInstanceTypeKey: string(metadata.InstanceTypeExBCC),
			},
			Finalizers: []string{ipamgeneric.WepFinalizer},
		},
		Spec: v1alpha1.WorkloadEndpointSpec{
			ContainerID: containerID,
			IP:          ipResult,
			Type:        ipamgeneric.WepTypePod,
			Mac:         ipAddedENI.MacAddress,
			ENIID:       ipAddedENI.EniId,
			Node:        pod.Spec.NodeName,
			SubnetID:    ipAddedENI.SubnetId,
			UpdateAt:    metav1.Time{Time: time.Unix(0, 0)},
		},
	}

	if k8sutil.IsStatefulSetPod(pod) {
		wep.Spec.Type = ipamgeneric.WepTypeSts
		wep.Labels[ipamgeneric.WepLabelStsOwnerKey] = util.GetStsName(wep)
	}

	if pod.Annotations != nil {
		wep.Spec.EnableFixIP = pod.Annotations[StsPodAnnotationEnableFixIP]
		wep.Spec.FixIPDeletePolicy = pod.Annotations[StsPodAnnotationFixIPDeletePolicy]
	}

	_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Create(ctx, wep, metav1.CreateOptions{})
	if err != nil {
		log.Errorf(ctx, "failed to create wep for pod (%v %v): %v", namespace, name, err)
		ipam.tryDeleteIPByWep(ctx, wep)
		return nil, err
	}
	log.Infof(ctx, "create wep with spec %+v for pod (%v %v) successfully", wep.Spec, namespace, name)

	// update allocated pod cache
	ipam.lock.Lock()
	if ipam.removeAddIPBackoffCache(wep.Spec.ENIID, true) {
		log.Infof(ctx, "remove backoff for eni %v when handling pod (%v %v) due to successful ip allocate", wep.Spec.ENIID, namespace, name)
	}
	ipam.allocated[ipResult] = wep
	ipam.lock.Unlock()

	total, used, err = ipam.datastore.GetNodeStats(node.Name)
	if err == nil {
		log.Infof(ctx, "total, used after allocate for node %v: %v %v", node.Name, total, used)
	}
	idleIPs, err = ipam.datastore.GetUnassignedPrivateIPByNode(node.Name)
	if err == nil {
		log.Infof(ctx, "idle ip in datastore after allocate for node %v: %v", node.Name, idleIPs)
	}

	return wep, nil
}

func (ipam *IPAM) Release(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Release] releasing IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Release] releasing IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	// new a wep, avoid data racing
	tmpWep, err := ipam.crdInformer.Cce().V1alpha1().WorkloadEndpoints().Lister().WorkloadEndpoints(namespace).Get(name)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Infof(ctx, "wep of pod (%v %v) not found", namespace, name)
			return nil, nil
		}
		log.Errorf(ctx, "failed to get wep of pod (%v %v): %v", namespace, name, err)
		return nil, err
	}

	wep := tmpWep.DeepCopy()

	// this may be due to a pod migrate to another node
	if wep.Spec.ContainerID != containerID {
		log.Warningf(ctx, "pod (%v %v) may have switched to another node, ignore old cleanup", name, namespace)
		return nil, nil
	}

	if isFixIPStatefulSetPodWep(wep) {
		log.Infof(ctx, "release: sts pod (%v %v) will update wep but private IP won't release", namespace, name)
		wep.Spec.UpdateAt = metav1.Time{Time: time.Now()}
		_, err = ipam.crdClient.CceV1alpha1().WorkloadEndpoints(namespace).Update(ctx, wep, metav1.UpdateOptions{})
		if err != nil {
			log.Errorf(ctx, "failed to update sts pod (%v %v) status: %v", namespace, name, err)
		}
		log.Infof(ctx, "release: update wep for sts pod (%v %v) successfully", namespace, name)
		return wep, nil
	}

	err = ipam.gcIPAndDeleteWep(ctx, wep)
	if err != nil {
		return wep, err
	}
	idleIPs, err := ipam.datastore.GetUnassignedPrivateIPByNode(wep.Spec.Node)
	if err == nil {
		log.Infof(ctx, "idle ip in datastore after release for node %v: %v", wep.Spec.Node, idleIPs)
	}

	return wep, nil
}

// allocateIPFromLocalPool get ip from local cache
// increase the poll if datastore has no available ip
func (ipam *IPAM) allocateIPFromLocalPool(ctx context.Context, node *corev1.Node, enis []*enisdk.Eni) (ipresult string, eni *enisdk.Eni, err error) {
	err = wait.ExponentialBackoff(retry.DefaultRetry, func() (done bool, err error) {
		if ipam.canAllocateIP(ctx, node.Name, enis) {
			return true, nil
		}

		ipam.sendIncreasePoolEvent(ctx, node, enis, true)
		return false, nil
	})
	if err == wait.ErrWaitTimeout {
		return "", nil, fmt.Errorf("allocate ip for node %v error", node.Name)
	}

	return ipam.tryAllocateIPByENIs(ctx, node.Name, enis)
}

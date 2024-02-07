package bcc

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apimachinery/networking"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

const (
	allocateIPTimeout       = 30 * time.Second
	errorMsgAllocateTimeout = "allocate ip timeout"
)

func (ipam *IPAM) Allocate(ctx context.Context, name, namespace, containerID string) (*v1alpha1.WorkloadEndpoint, error) {
	log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) starts", namespace, name)
	defer log.Infof(ctx, "[Allocate] allocating IP for pod (%v %v) ends", namespace, name)

	if !ipam.Ready(ctx) {
		log.Warningf(ctx, "ipam has not synced cache yet")
		return nil, fmt.Errorf("ipam has not synced cache yet")
	}

	// The allocateIPTimeout is not set in the context because the context will be automatically disconnected when timeout.
	deadline := time.Now().Add(allocateIPTimeout)

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

	// ensure node in datastore
	nodeStoreErr := ipam.ensureNodeInStore(ctx, node)
	if nodeStoreErr != nil {
		return nil, err
	}

	// get node stats from store, to further check if pool is corrupted
	statsErr := ipam.statsNodeIP(ctx, "before allocated", node.Name)
	if statsErr != nil {
		log.Errorf(ctx, "stat node %s ip failed: %s", node.Name, statsErr)
		return nil, statsErr
	}

	// find out which enis are suitable to bind
	enis, err := ipam.findSuitableENIs(ctx, pod)
	if err != nil {
		log.Errorf(ctx, "failed to find a suitable eni for pod (%v %v): %v", namespace, name, err)
		return nil, err
	}

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

	var allocatedErr error

	if isStsPodReuseIP(wep, pod) {
		// sts pod reuse ip
		wep, allocatedErr = ipam.allocateIPForFixedIPPod(ctx, node, pod, containerID, enis, wep, deadline)
	} else {
		// other pod allocate ip
		wep, allocatedErr = ipam.allocateIPForOrdinaryPod(ctx, node, pod, containerID, enis, wep, deadline)
	}
	if allocatedErr != nil {
		return nil, allocatedErr
	}

	statsErr = ipam.statsNodeIP(ctx, "after allocated", node.Name)
	if statsErr != nil {
		log.Errorf(ctx, "stat node %s ip failed: %s", node.Name, statsErr)
	}

	return wep, nil
}

func (ipam *IPAM) ensureNodeInStore(ctx context.Context, node *corev1.Node) error {
	err := wait.ExponentialBackoff(retry.DefaultRetry, func() (done bool, err error) {
		if ipam.datastore.NodeExistsInStore(node.Name) {
			return true, nil
		}

		evt := &event{
			node: node,
			ctx:  ctx,
		}

		ipam.lock.Lock()
		ch, ok := ipam.buildDataStoreEventChan[node.Name]
		if !ok {
			ch = make(chan *event)
			ipam.buildDataStoreEventChan[node.Name] = ch
			go func() {
				err := ipam.handleRebuildNodeDatastoreEvent(ctx, node, ch)
				if err != nil {
					log.Warningf(ctx, "rebuild node %s failed, wait to retry, err: %s", node.Name, err)
				}
			}()
		}
		ipam.lock.Unlock()

		ch <- evt
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("init node %v datastore error: %s", node.Name, err)
	}
	return nil
}

func (ipam *IPAM) statsNodeIP(ctx context.Context, stage, nodeName string) error {
	total, used, err := ipam.datastore.GetNodeStats(nodeName)
	if err != nil {
		return err
	}
	log.Infof(ctx, "%s: total, used ip for node %s: %d %d", stage, nodeName, total, used)

	idleIPs, err := ipam.datastore.GetUnassignedPrivateIPByNode(nodeName)
	if err != nil {
		return err
	}
	log.Infof(ctx, "%s: idle ip in datastore before allocate for node %s: %v", stage, nodeName, idleIPs)
	return nil
}

func isStsPodReuseIP(wep *v1alpha1.WorkloadEndpoint, pod *corev1.Pod) bool {
	if wep == nil {
		return false
	}

	return wep.Spec.IP != "" && IsFixIPStatefulSetPod(pod)
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

	if networking.IsFixIPStatefulSetPodWep(wep) {
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

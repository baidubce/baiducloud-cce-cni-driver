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

package watchers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	k8sUtils "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/utils"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	pkgOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

const (

	// netResourceSetConditionReason is the condition name used by CCE to set
	// when the Network is setup in the node.
	netResourceSetConditionReason        = "NetworkAgentIsUp"
	netResourceSetConditionUnreadyReason = "NetworkAgentNotReady"

	messageRuningAndReady    = "cce-network-agent is running and ready on this node"
	messageNotRuningAndReady = "cce-network-agent is not running or ready on this node"

	CCENetworkUnavailable corev1.NodeConditionType = "CCENetworkUnavailable"
)

var (
	queueKeyFunc = cache.DeletionHandlingMetaNamespaceKeyFunc

	ctrlMgr = controller.NewManager()

	mno markNodeOptions
)

func checkTaintForNextNodeItem(workQueue workqueue.RateLimitingInterface) bool {
	// Get the next 'key' from the queue.
	key, quit := workQueue.Get()
	if quit {
		return false
	}
	// Done marks item as done processing, and if it has been marked as dirty
	// again while it was being processed, it will be re-added to the queue for
	// re-processing.
	defer workQueue.Done(key)

	success := checkAndMarkNode(key.(string), mno)
	if !success {
		workQueue.Forget(key)
		return true
	}

	// If the event was processed correctly then forget it from the queue.
	// If we don't do this, the next ".Get()" will always return this 'key'.
	// It also depends on if the queue has a rate-limiter (not used in this
	// program)
	workQueue.Forget(key)
	return true
}

// checkAndMarkNode checks if the node contains a CCE pod in running state
// so that it can remove the toleration and taints of that node.
func checkAndMarkNode(nodeName string, options markNodeOptions) bool {
	node, err := NodeClient.Lister().Get(nodeName)
	if err != nil && !k8sErrors.IsNotFound(err) {
		return false
	}
	if node == nil {
		return false
	}
	if options.shouldSkip(node) {
		return false
	}

	if isCCEPodRunning(node.GetName()) {
		markNode(node.GetName(), options, true)
	} else {
		log.WithFields(logrus.Fields{logfields.NodeName: node.GetName()}).Debug("CCE pod not running for node")
		markNode(node.GetName(), options, false)
	}
	return true
}

// ccePodsWatcher starts up a pod watcher to handle pod events.
func ccePodsWatcher(stopCh <-chan struct{}) {
	cceQueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cce-pod-queue")

	eventHandler := &cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			pod, ok := obj.(*corev1.Pod)
			if ok {
				if pod.Namespace != option.Config.CCEK8sNamespace {
					return false
				}
				selector, err := labels.Parse(option.Config.CCEPodLabels)
				if err == nil {
					return selector.Matches(labels.Set(pod.Labels))
				}
			}
			return false
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				key, _ := queueKeyFunc(obj)
				cceQueue.Add(key)
			},
			UpdateFunc: func(_, newObj interface{}) {
				key, _ := queueKeyFunc(newObj)
				cceQueue.Add(key)
			},
		},
	}
	k8s.WatcherClient().Informers.Core().V1().Pods().Informer().AddEventHandler(eventHandler)

	go func() {
		// Do not use the k8sClient provided by the nodesInit function since we
		// need a k8s client that can update node structures and not simply
		// watch for node events.
		for processNextCCEPodItem(cceQueue) {
		}
	}()
}

func processNextCCEPodItem(workQueue workqueue.RateLimitingInterface) bool {
	// Get the next 'key' from the queue.
	key, quit := workQueue.Get()
	if quit {
		return false
	}
	// Done marks item as done processing, and if it has been marked as dirty
	// again while it was being processed, it will be re-added to the queue for
	// re-processing.
	defer workQueue.Done(key)
	namespace, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		workQueue.Forget(key)
		return true
	}

	pod, err := PodClient.Lister().Pods(namespace).Get(name)
	if err != nil {
		return true
	}

	nodeName := pod.Spec.NodeName

	success := checkAndMarkNode(nodeName, mno)
	if !success {
		workQueue.Forget(key)
		return true
	}

	// If the event was processed correctly then forget it from the queue.
	// If we don't do this, the next ".Get()" will always return this 'key'.
	// It also depends on if the queue has a rate-limiter (not used in this
	// program)
	workQueue.Forget(key)
	return true
}

// isCCEPodRunning returns true if there is a CCE pod Ready on the given
// nodeName.
func isCCEPodRunning(nodeName string) bool {
	var ccePodsInNode []*corev1.Pod

	selector, err := labels.Parse(option.Config.CCEPodLabels)
	if err != nil {
		return false
	}
	values, err := PodStore.(cache.Indexer).ByIndex(PodNodeNameIndex, nodeName)
	if err != nil {
		return false
	}
	for _, pod := range values {
		p := pod.(*corev1.Pod)
		if p.Namespace == option.Config.CCEK8sNamespace && selector.Matches(labels.Set(p.Labels)) {
			ccePodsInNode = append(ccePodsInNode, p)
		}
	}

	if len(ccePodsInNode) == 0 {
		return false
	}
	for _, ccePod := range ccePodsInNode {
		if k8sUtils.GetLatestPodReadiness(ccePod.Status) == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// hasAgentNotReadyTaint returns true if the given node has the CCE Agent
// Not Ready Node Taint.
func hasAgentNotReadyTaint(k8sNode *corev1.Node) bool {
	for _, taint := range k8sNode.Spec.Taints {
		if taint.Key == pkgOption.Config.AgentNotReadyNodeTaintValue() {
			return true
		}
	}
	return false
}

// setNodeNetworkUnavailable sets Kubernetes NodeNetworkUnavailable to
// false as CCE is managing the network connectivity.
// https://kubernetes.io/docs/concepts/architecture/nodes/#condition
func setNodeNetworkUnavailable(ctx context.Context, nodeName, reason, message string, status corev1.ConditionStatus) error {
	n, err := NodeClient.Lister().Get(nodeName)
	if err != nil {
		return err
	}

	if HasCCEIsUpCondition(n, reason, status) {
		return nil
	}

	now := metav1.Now()
	netCondition := corev1.NodeCondition{
		Type:               corev1.NodeNetworkUnavailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		LastHeartbeatTime:  now,
	}
	cceNetCondition := corev1.NodeCondition{
		Type:               CCENetworkUnavailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: now,
		LastHeartbeatTime:  now,
	}
	raw, err := json.Marshal(&[]corev1.NodeCondition{netCondition, cceNetCondition})
	if err != nil {
		return err
	}
	patch := []byte(fmt.Sprintf(`{"status":{"conditions":%s}}`, raw))

	_, err = NodeClient.NodeInterface().PatchStatus(ctx, nodeName, patch)
	log.Infof("Setting NodeNetworkUnavailable for node %s", nodeName)
	return err
}

// HasCCEIsUpCondition returns true if the given k8s node has the cce node
// condition set.
// warning: we cannot judge the reason why the node is not ready,
// because many external network plug-ins, such as Cilium, also modify this field
func HasCCEIsUpCondition(n *corev1.Node, reason string, status corev1.ConditionStatus) bool {
	for _, condition := range n.Status.Conditions {
		if condition.Type == corev1.NodeNetworkUnavailable &&
			condition.Status != status {
			return false
		}
		if condition.Type == CCENetworkUnavailable &&
			condition.Status == status &&
			condition.Reason == reason {
			return true
		}
	}
	return false
}

func addNodeTaint(ctx context.Context, nodeName string) error {
	k8sNode, err := NodeClient.Lister().Get(nodeName)
	if err != nil {
		return err
	}

	var taintFound bool

	var taints []corev1.Taint
	for _, taint := range k8sNode.Spec.Taints {
		if taint.Key == pkgOption.Config.AgentNotReadyNodeTaintValue() {
			taintFound = true
		}
	}

	// No cce taints found
	if !taintFound {
		return nil
	}
	log.WithFields(logrus.Fields{
		logfields.NodeName: nodeName,
		"taint":            pkgOption.Config.AgentNotReadyNodeTaintValue(),
	}).Debug("adding Node Taint")

	taints = append(k8sNode.Spec.Taints, corev1.Taint{
		Key:    pkgOption.Config.AgentNotReadyNodeTaintValue(),
		Effect: corev1.TaintEffectNoSchedule,
	})

	createStatusAndNodePatch := []k8s.JSONPatch{
		{
			OP:    "test",
			Path:  "/spec/taints",
			Value: k8sNode.Spec.Taints,
		},
		{
			OP:    "replace",
			Path:  "/spec/taints",
			Value: taints,
		},
	}

	patch, err := json.Marshal(createStatusAndNodePatch)
	if err != nil {
		return err
	}

	_, err = NodeClient.NodeInterface().Patch(ctx, nodeName, k8sTypes.JSONPatchType, patch, metav1.PatchOptions{})
	log.Infof("Adding Node Taint for node %s", nodeName)
	return err
}

// removeNodeTaint removes the AgentNotReadyNodeTaint allowing for pods to be
// scheduled once CCE is setup. Mostly used in cloud providers to prevent
// existing CNI plugins from managing pods.
func removeNodeTaint(ctx context.Context, nodeName string) error {
	k8sNode, err := NodeClient.Lister().Get(nodeName)
	if err != nil {
		return err
	}

	var taintFound bool

	var taints []corev1.Taint
	for _, taint := range k8sNode.Spec.Taints {
		if taint.Key != pkgOption.Config.AgentNotReadyNodeTaintValue() {
			taints = append(taints, taint)
		} else {
			taintFound = true
		}
	}

	// No cce taints found
	if !taintFound {
		return nil
	}
	log.WithFields(logrus.Fields{
		logfields.NodeName: nodeName,
		"taint":            pkgOption.Config.AgentNotReadyNodeTaintValue(),
	}).Debug("Removing Node Taint")

	createStatusAndNodePatch := []k8s.JSONPatch{
		{
			OP:    "test",
			Path:  "/spec/taints",
			Value: k8sNode.Spec.Taints,
		},
		{
			OP:    "replace",
			Path:  "/spec/taints",
			Value: taints,
		},
	}

	patch, err := json.Marshal(createStatusAndNodePatch)
	if err != nil {
		return err
	}

	_, err = NodeClient.NodeInterface().Patch(ctx, nodeName, k8sTypes.JSONPatchType, patch, metav1.PatchOptions{})
	log.Infof("Removing Node Taint for node %s", nodeName)
	return err
}

type markNodeOptions struct {
	RemoveNodeTaint     bool
	SetCCEIsUpCondition bool

	// SkipManagerNodeLabels do not enable health checks for certain nodes
	// There is an OR relationship between multiple labels
	SkipManagerNodeLabels map[string]string
}

func (option *markNodeOptions) shouldSkip(node *corev1.Node) bool {
	if node.Labels == nil {
		return false
	}
	for key, value := range option.SkipManagerNodeLabels {
		if node.Labels[key] == value {
			return true
		}
	}
	return false
}

// markNode marks the Kubernetes node depending on the modes that it is passed
// on.
func markNode(nodeName string, options markNodeOptions, ready bool) {
	if ready {
		ctrlName := fmt.Sprintf("mark-k8s-node-%s-as-available", nodeName)
		ctrlMgr.UpdateController(ctrlName,
			controller.ControllerParams{
				DisableDebugLog: true,
				DoFunc: func(ctx context.Context) error {
					if options.RemoveNodeTaint {
						err := removeNodeTaint(ctx, nodeName)
						if err != nil {
							return err
						}
					}
					if options.SetCCEIsUpCondition {
						err := setNodeNetworkUnavailable(ctx, nodeName, netResourceSetConditionReason, messageRuningAndReady, corev1.ConditionFalse)
						if err != nil {
							return err
						}
					}
					return nil
				},
			})
	} else {
		ctrlName := fmt.Sprintf("mark-k8s-node-%s-as-unavailable", nodeName)
		ctrlMgr.UpdateController(ctrlName,
			controller.ControllerParams{
				DisableDebugLog: true,
				DoFunc: func(ctx context.Context) error {
					if options.RemoveNodeTaint {
						err := addNodeTaint(ctx, nodeName)
						if err != nil {
							return err
						}
					}
					if options.SetCCEIsUpCondition {
						err := setNodeNetworkUnavailable(ctx, nodeName, netResourceSetConditionUnreadyReason, messageNotRuningAndReady, corev1.ConditionTrue)
						if err != nil {
							return err
						}
					}
					return nil
				},
			})
	}
}

// HandleNodeTolerationAndTaints remove node
func HandleNodeTolerationAndTaints(stopCh <-chan struct{}) {
	mno = markNodeOptions{
		RemoveNodeTaint:       option.Config.RemoveNetResourceSetTaints,
		SetCCEIsUpCondition:   option.Config.SetCCEIsUpCondition,
		SkipManagerNodeLabels: option.Config.SkipManagerNodeLabels,
	}
	nodesInit()

	go func() {
		// Do not use the k8sClient provided by the nodesInit function since we
		// need a k8s client that can update node structures and not simply
		// watch for node events.
		for checkTaintForNextNodeItem(nodeQueue) {
		}
	}()

	ccePodsWatcher(stopCh)
}

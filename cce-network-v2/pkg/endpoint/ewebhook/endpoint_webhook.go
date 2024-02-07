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
package ewebhook

import (
	"fmt"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/pststrategy"
)

type EndpointWebhook struct {
}

func (e *EndpointWebhook) AddNodeAffinity(pod *corev1.Pod) (add bool) {
	if !e.addEndpointAffinity(pod) {
		return e.addPstsAffinity(pod)
	}
	return true
}

func (e *EndpointWebhook) addPstsAffinity(pod *corev1.Pod) (add bool) {
	var (
		log = logrus.WithField("namespace", pod.Namespace).WithField("name", pod.Name)
	)

	psts, err := pststrategy.SelectPSTS(log, k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Lister(), pod)
	if err != nil {
		log.WithError(err).Error("list psts failed")
		return false
	}
	if psts == nil {
		return false
	}

	// 1. find all zone
	availableZoneSet, err := getAvailableZone(psts)
	if err != nil {
		log.WithError(err).Error("get available zone failed")
		return false
	}

	// 2. add topology spread
	if psts.Spec.MaxSkew > 0 {
		pod.Spec.TopologySpreadConstraints = append(pod.Spec.TopologySpreadConstraints, corev1.TopologySpreadConstraint{
			MaxSkew:           psts.Spec.MaxSkew,
			TopologyKey:       k8s.TopologyKeyOfPod,
			WhenUnsatisfiable: corev1.UnsatisfiableConstraintAction(psts.Spec.WhenUnsatisfiable),
			LabelSelector:     psts.Spec.Selector,
		})
	}

	// add affinity to pod
	// append node selector affinity if need
	addAffinity := addAffinityToPod(availableZoneSet.UnsortedList(), pod)
	if addAffinity {
		log.Infof("add affinity for subnet zone to pod")
	}
	return addAffinity
}

func (e *EndpointWebhook) addEndpointAffinity(pod *corev1.Pod) (add bool) {
	defer func() {
		if add {
			logrus.WithField("namespace", pod.Namespace).WithField("name", pod.Name).Infof("add affinity to fixed ip pod")
		}
	}()
	cli := k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Lister()
	ep, err := cli.CCEEndpoints(pod.Namespace).Get(pod.Name)
	if err != nil {
		return false
	}

	// don't need add node affinity to pod
	expressions := ep.Status.NodeSelectorRequirement
	if len(expressions) == 0 {
		return false
	}
	nodeSelectorTerm := corev1.NodeSelectorTerm{
		MatchExpressions: expressions,
	}
	nodeSelectorTerms := []corev1.NodeSelectorTerm{nodeSelectorTerm}
	nodeSelector := &corev1.NodeSelector{NodeSelectorTerms: nodeSelectorTerms}
	nodeAffinity := &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector}
	affinity := &corev1.Affinity{NodeAffinity: nodeAffinity}
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = affinity
		return true
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = nodeAffinity
		return true
	}
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSelector
		return true
	}
	pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, expressions...)
	return true
}

// addAffinityToPod
func addAffinityToPod(zones []string, pod *corev1.Pod) bool {
	matchExpression := corev1.NodeSelectorRequirement{
		Key:      k8s.TopologyKeyOfPod,
		Operator: corev1.NodeSelectorOpIn,
		Values:   zones,
	}
	nodeSelectorTerm := corev1.NodeSelectorTerm{
		MatchExpressions: []corev1.NodeSelectorRequirement{matchExpression},
	}
	nodeSelectorTerms := []corev1.NodeSelectorTerm{nodeSelectorTerm}
	nodeSelector := &corev1.NodeSelector{NodeSelectorTerms: nodeSelectorTerms}
	nodeAffinity := &corev1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector}
	affinity := &corev1.Affinity{NodeAffinity: nodeAffinity}
	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = affinity
		return true
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = nodeAffinity
		return true
	}
	if pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil ||
		len(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSelector
		return true
	}

	containsZoneAffinity := false
	for _, nodeSelectorT := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		for _, matchE := range nodeSelectorT.MatchExpressions {
			if matchE.Key == k8s.TopologyKeyOfPod {
				containsZoneAffinity = true
				break
			}
		}
	}
	if !containsZoneAffinity {
		pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions = append(pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions, matchExpression)
		return true
	}
	return false
}

func getAvailableZone(psts *ccev2.PodSubnetTopologySpread) (sets.String, error) {
	availableZoneSet := sets.NewString()
	for _, v := range psts.Status.AvailableSubnets {
		if !v.Enable {
			continue
		}
		zone, err := api.TransZoneNameToAvailableZone(v.AvailabilityZone)
		if err == nil {
			availableZoneSet.Insert(string(zone))
		}
	}

	if availableZoneSet.Len() == 0 {
		return nil, fmt.Errorf("no subnet available in psts(%s/%s) object", psts.Namespace, psts.Name)
	}
	return availableZoneSet, nil
}

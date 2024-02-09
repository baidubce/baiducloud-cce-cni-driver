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
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

type EndpointWebhook struct {
}

func (e *EndpointWebhook) AddNodeAffinity(pod *corev1.Pod) (add bool) {
	defer func() {
		if add {
			logrus.WithField("namespace", pod.Namespace).WithField("name", pod.Name).Infof("add affinity to fixed ip pod")
		}
	}()
	if !k8s.HaveFixedIPLabel(pod) {
		return false
	}
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

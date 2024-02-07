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
package pststrategy

import (
	"fmt"
	"sort"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	listerv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// SelectPSTS select the best podsubnettopologyspreads object associated with the pod
// And record the object at the annotation of the pod
func SelectPSTS(logEntry *logrus.Entry, pstsLister listerv2.PodSubnetTopologySpreadLister, pod *corev1.Pod) (*ccev2.PodSubnetTopologySpread, error) {

	pstsList, err := pstsLister.PodSubnetTopologySpreads(pod.Namespace).List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("list PodSubnetTopologySpreads at namespace %s error %w", pod.Namespace, err)
	}

	psts := SelectPriorityPSTS(pstsList, labels.Set(pod.Labels))
	if psts != nil {
		logEntry.Infof("select pod （%s/%s) matched psts %s", pod.GetNamespace(), pod.GetName(), psts.GetName())
	}
	return psts, nil
}

// Select the highest priority and matching pSTS object
// label: pod labels to be mached
// pstsList: list of PodSubnetTopologySpread
// return:
// - nil: if no PodSubnetTopologySpread matched
// - psts: the highest priority PodSubnetTopologySpread object
func SelectPriorityPSTS(pstsList []*ccev2.PodSubnetTopologySpread, label labels.Labels) *ccev2.PodSubnetTopologySpread {
	var matchedPstses []*ccev2.PodSubnetTopologySpread

	for i := 0; i < len(pstsList); i++ {
		psts := pstsList[i]
		if psts.Spec.Selector == nil {
			matchedPstses = append(matchedPstses, psts)
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(psts.Spec.Selector)
		if err != nil {
			continue
		}
		if selector.Matches(label) {
			matchedPstses = append(matchedPstses, psts)
		}
	}

	if len(matchedPstses) == 0 {
		return nil
	}
	sort.Slice(matchedPstses, func(i, j int) bool {
		pstsI := matchedPstses[i]
		pstsJ := matchedPstses[j]
		return pstsI.Spec.Priority < pstsJ.Spec.Priority
	})
	return matchedPstses[len(matchedPstses)-1]
}

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
package podmutating

import (
	"context"

	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	ipam       string
	eniUseMode string
)

func RegisterCustomerFlags(pset *pflag.FlagSet) {
	pset.StringVar(&ipam, "ipam", "", "ipam ")
	pset.StringVar(&eniUseMode, "eni-use-mode", "Secondary", "ipam ")
}

// add customer resource to pod while create pod
// add cce.baidubce.com/ip to the first container of pod resource
func addCustomerResource2Pod(ctx context.Context, pod *corev1.Pod, customerResource corev1.ResourceName) bool {
	if pod.Spec.HostNetwork {
		return false
	}
	if len(pod.Spec.Containers) == 0 {
		return false
	}

	for i := 0; i < len(pod.Spec.Containers); i++ {
		removeResource(&pod.Spec.Containers[i].Resources, customerResource)
	}
	firstContainerResource := &pod.Spec.Containers[0].Resources
	if len(firstContainerResource.Requests) == 0 {
		firstContainerResource.Requests = make(corev1.ResourceList)
	}
	if len(firstContainerResource.Limits) == 0 {
		firstContainerResource.Limits = make(corev1.ResourceList)
	}

	resourceValue := *resource.NewQuantity(1, resource.DecimalSI)
	firstContainerResource.Requests[customerResource] = resourceValue
	firstContainerResource.Limits[customerResource] = resourceValue
	log.Infof("addIPResource2Pod add 1 %s resource to resouces requests of pod(%s/%s)", customerResource, pod.Namespace, pod.Name)

	return true
}

func removeResource(rs *corev1.ResourceRequirements, key corev1.ResourceName) {
	delete(rs.Requests, key)
	delete(rs.Limits, key)
}

// AddENIToleration add toleration and container.resources to pod when pod has eni label
func addIPResource2Pod(ctx context.Context, pod *corev1.Pod) (add bool) {
	return addCustomerResource2Pod(ctx, pod, k8s.ResourceIPForNode)
}

// AddENIToleration add toleration and container.resources to pod when pod has eni label
func addENIResource2Pod(ctx context.Context, pod *corev1.Pod) (add bool) {
	defer func() {
		if add {
			log.WithField("namespace", pod.Namespace).WithField("name", pod.Name).Infof("mutate eni pod")
		}
	}()

	// default use vpc eni mode
	if ipam != ipamOption.IPAMVpcEni || eniUseMode != string(ccev2.ENIUseModePrimaryIP) {
		if !k8s.HavePrimaryENILabel(pod) {
			return false
		}
	}

	return addCustomerResource2Pod(ctx, pod, k8s.ResourceENIForNode)
}

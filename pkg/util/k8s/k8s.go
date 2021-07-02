/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package k8s

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientretry "k8s.io/client-go/util/retry"
	nodeutil "k8s.io/kubernetes/pkg/controller/util/node"
	utilnode "k8s.io/kubernetes/pkg/util/node"

	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func BuildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// NewKubeClient creates a k8s client
func NewKubeClient(kubeconfig string) (kubernetes.Interface, error) {
	config, err := BuildConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// NewCRDClient creates CRD client
func NewCRDClient(kubeconfig string) (clientset.Interface, error) {
	config, err := BuildConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return clientset.NewForConfig(config)
}

func IsStatefulSetPod(pod *v1.Pod) bool {
	controllerRef := metav1.GetControllerOf(pod)
	if controllerRef != nil {
		if controllerRef.Kind == "StatefulSet" {
			return true
		}
	}
	return false
}

func IsPodFinished(pod *v1.Pod) bool {
	// Ref: https://github.com/projectcalico/libcalico-go/blob/9bbd69b5de2b6df62f4508d7557ddb67b1be1dc2/lib/backend/k8s/conversion/conversion.go#L157-L171
	switch pod.Status.Phase {
	case v1.PodFailed, v1.PodSucceeded, v1.PodPhase("Completed"):
		return true
	}
	return false
}

func UpdateNetworkingCondition(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	nodeName string,
	networkReady bool,
	readyReason string,
	unReadyReason string,
	readyMsg string,
	unReadyMsg string,
) error {
	node, err := kubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	_, condition := nodeutil.GetNodeCondition(&(node.Status), v1.NodeNetworkUnavailable)
	if networkReady && condition != nil && condition.Status == v1.ConditionFalse {
		log.Infof(ctx, "set node %v with NodeNetworkUnavailable=false was canceled because it is already set", node.Name)
		return nil
	}

	if !networkReady && condition != nil && condition.Status == v1.ConditionTrue {
		log.Infof(ctx, "set node %v with NodeNetworkUnavailable=true was canceled because it is already set", node.Name)
		return nil
	}

	log.Infof(ctx, "patching node %v NetworkUnavailable status with %v, previous condition was:%+v", node.Name, !networkReady, condition)

	// either condition is not there, or has a value != to what we need
	// start setting it
	err = clientretry.RetryOnConflict(clientretry.DefaultRetry, func() error {
		var err error
		currentTime := metav1.Now()

		if networkReady {
			err = utilnode.SetNodeCondition(kubeClient, types.NodeName(node.Name), v1.NodeCondition{
				Type:               v1.NodeNetworkUnavailable,
				Status:             v1.ConditionFalse,
				Reason:             readyReason,
				Message:            readyMsg,
				LastTransitionTime: currentTime,
			})
		} else {
			err = utilnode.SetNodeCondition(kubeClient, types.NodeName(node.Name), v1.NodeCondition{
				Type:               v1.NodeNetworkUnavailable,
				Status:             v1.ConditionTrue,
				Reason:             unReadyReason,
				Message:            unReadyMsg,
				LastTransitionTime: currentTime,
			})
		}
		if err != nil {
			log.Errorf(ctx, "error updating node %s, retrying: %v", types.NodeName(node.Name), err)
		}
		return err
	})

	if err != nil {
		log.Errorf(ctx, "error updating node %s, retrying: %v", types.NodeName(node.Name), err)
		return err
	}

	return nil
}

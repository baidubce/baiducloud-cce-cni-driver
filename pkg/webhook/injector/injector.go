// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/baidu/jpaas-caas/baiducloud-cce-cni-driver/pkg/webhook/injector.go - injector fields to handler

// modification history
// --------------------
// 2022/07/28, by wangeweiwei22, create injector

// DESCRIPTION

package injector

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
)

// injector crd client to handler of webhook
type CRDClient interface {
	InjectCRDClient(crdClient versioned.Interface) error
}

// CrdClientInto will set crdClient and return the result on i if it implements Scheme.  Returns
// false if i does not implement Scheme.
func CrdClientInto(crdClient versioned.Interface, i interface{}) (bool, error) {
	if is, ok := i.(CRDClient); ok {
		return true, is.InjectCRDClient(crdClient)
	}
	return false, nil
}

// injector kube client to handler of webhook
type KubeClient interface {
	InjectKubeClient(kubeClient kubernetes.Interface) error
}

// CrdClientInto will set crdClient and return the result on i if it implements Scheme.  Returns
// false if i does not implement Scheme.
func KubeClientInto(kubeClient kubernetes.Interface, i interface{}) (bool, error) {
	if is, ok := i.(KubeClient); ok {
		return true, is.InjectKubeClient(kubeClient)
	}
	return false, nil
}

type CloudClientInject interface {
	InjectCloudClient(bceClient cloud.Interface) error
}

func CloudClientInto(bceClient cloud.Interface, i interface{}) (bool, error) {
	if is, ok := i.(CloudClientInject); ok {
		return true, is.InjectCloudClient(bceClient)
	}
	return false, nil
}

type KubeInformerInject interface {
	InjectKubeInformer(kubeInformer informers.SharedInformerFactory) error
}

func KubeInformerInto(kubeInformer informers.SharedInformerFactory, i interface{}) (bool, error) {
	if is, ok := i.(KubeInformerInject); ok {
		return true, is.InjectKubeInformer(kubeInformer)
	}
	return false, nil
}

type NetworkInformerInject interface {
	InjectNetworkInformer(networkInformer crdinformers.SharedInformerFactory) error
}

func NetworkInformerInto(networkInformer crdinformers.SharedInformerFactory, i interface{}) (bool, error) {
	if is, ok := i.(NetworkInformerInject); ok {
		return true, is.InjectNetworkInformer(networkInformer)
	}
	return false, nil
}

type CNIModeInject interface {
	InjectCNIMode(cniMode types.ContainerNetworkMode) error
}

func CNIModeInto(cniMode types.ContainerNetworkMode, i interface{}) (bool, error) {
	if is, ok := i.(CNIModeInject); ok {
		return true, is.InjectCNIMode(cniMode)
	}
	return false, nil
}

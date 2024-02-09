package eri

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sync"
	"time"
)

type IPAM struct {
	lock sync.RWMutex

	vpcID string
	// key is instanceID, value is list of node
	nodeCache map[string]*corev1.Node
	// ipam will rebuild cache if restarts, should not handle request from cni if cacheHasSynced is false
	cacheHasSynced bool
	gcPeriod       time.Duration

	eventRecorder record.EventRecorder
	kubeInformer  informers.SharedInformerFactory
	kubeClient    kubernetes.Interface
	crdInformer   crdinformers.SharedInformerFactory
	crdClient     versioned.Interface

	cloud cloud.Interface
}

var _ ipam.RoceInterface = &IPAM{}

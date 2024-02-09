package rdma

import (
	"sync"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/rdma/client"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
)

type IPAM struct {
	lock sync.RWMutex

	vpcID string
	// ipam will rebuild cache if restarts, should not handle request from cni if cacheHasSynced is false
	cacheHasSynced bool
	gcPeriod       time.Duration

	eventRecorder record.EventRecorder
	kubeInformer  informers.SharedInformerFactory
	kubeClient    kubernetes.Interface
	crdInformer   crdinformers.SharedInformerFactory
	crdClient     versioned.Interface

	iaasClient client.IaaSClient
}

var _ ipam.RoceInterface = &IPAM{}

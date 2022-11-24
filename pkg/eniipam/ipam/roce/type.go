package roce

import (
	"context"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"sync"
	"time"
)

type event struct {
	node    *corev1.Node
	enis    []*enisdk.Eni
	passive bool
	ctx     context.Context
}

type IPAM struct {
	lock sync.RWMutex
	// key is instanceID, value is list of enis attached
	hpcEniCache map[string][]hpc.Result
	// key is instanceID, value is list of node
	nodeCache map[string]*corev1.Node

	eventBroadcaster record.EventBroadcaster
	eventRecorder    record.EventRecorder

	kubeInformer informers.SharedInformerFactory
	kubeClient   kubernetes.Interface

	crdInformer crdinformers.SharedInformerFactory
	crdClient   versioned.Interface

	addIPBackoffCache map[string]*wait.Backoff

	// key is ip, value is mwep
	allocated map[string]*v1alpha1.MultiIPWorkloadEndpoint
	// ipam will rebuild cache if restarts, should not handle request from cni if cacheHasSynced is false
	cacheHasSynced bool
	cloud          cloud.Interface
	clock          clock.Clock

	eniSyncPeriod        time.Duration
	informerResyncPeriod time.Duration
	gcPeriod             time.Duration
	// event channel
	increasePoolEventChan map[string]chan *event
}

var _ ipam.RoceInterface = &IPAM{}

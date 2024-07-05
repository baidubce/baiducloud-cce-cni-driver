package watchers

import (
	"context"
	"fmt"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
)

func (k *K8sWatcher) initNRCS(cceClient *k8s.K8sCCEClient, asyncControllers *sync.WaitGroup) {
	apiGroup := k8sAPIGroupNRCSV2Alpha1
	informer := cceClient.Informers.Cce().V2alpha1().NetResourceConfigSets().Informer()
	k.blockWaitGroupToSyncResources(k.stop, nil, informer.HasSynced, apiGroup)
	go informer.Run(k.stop)
	asyncControllers.Done()
}

// GetNodeNrcs find 1st priority matched NetResourceConfigSet which matches own node
func (k *K8sWatcher) GetNodeNrcs(nodeName string) (*v2alpha1.NetResourceConfigSet, error) {
	nrcsList, err := k8s.CCEClient().Informers.Cce().V2alpha1().NetResourceConfigSets().Lister().List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list nrscs %w", err)
	}
	if len(nrcsList) == 0 {
		return nil, nil
	}

	k8sNode, err := k.GetK8sNode(context.TODO(), nodeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get k8s node %w", err)
	}

	var matched *v2alpha1.NetResourceConfigSet

	for _, nrcs := range nrcsList {
		selector, err := metav1.LabelSelectorAsSelector(nrcs.Spec.Selector)
		if err != nil {
			log.WithError(err).Warnf("invalid label selector in NetResourceConfigSet %q", nrcs.Name)
			continue
		}
		if selector.Matches(labels.Set(k8sNode.Labels)) {
			if matched == nil {
				matched = nrcs
			} else if matched.Spec.Priority < nrcs.Spec.Priority {
				matched = nrcs
			}
		}
	}
	return matched, nil
}

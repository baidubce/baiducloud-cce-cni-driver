package watchers

import (
	"fmt"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"k8s.io/client-go/tools/cache"
)

const (
	IndexIPToEndpoint = "ipToEndpoint"
	IndexIPToENI      = "ipToENI"
)

func StartWatchers(stopCh <-chan struct{}) {
	k8s.WatcherClient().Informers.Core().V1().Pods().Informer()
	k8s.WatcherClient().Informers.Core().V1().Nodes().Informer()
	k8s.WatcherClient().Informers.Core().V1().Namespaces().Informer()

	initNodePodStore()

	k8s.CCEClient().Informers.Cce().V2().NetResourceSets().Informer()
	k8s.CCEClient().Informers.Cce().V2().ENIs().Informer()

	addIPIndexer()

	k8s.CCEClient().Informers.Cce().V1().Subnets().Informer()
	k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Informer()
	k8s.CCEClient().Informers.Cce().V2alpha1().ClusterPodSubnetTopologySpreads().Informer()

	k8s.WatcherClient().Informers.Start(stopCh)
	k8s.CCEClient().Informers.Start(stopCh)
}

func WaitForCacheSync(stopCh <-chan struct{}) {
	k8s.WatcherClient().Informers.WaitForCacheSync(stopCh)
	k8s.CCEClient().Informers.WaitForCacheSync(stopCh)
}

// addIndexer add ip to endpoint indexer to cceendpoint informer
func addIPIndexer() {
	informer := k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer()
	informer.AddIndexers(cache.Indexers{
		IndexIPToEndpoint: func(obj interface{}) ([]string, error) {
			ep, ok := obj.(*ccev2.CCEEndpoint)
			if !ok {
				return nil, fmt.Errorf("object is not CCEEndpoint")
			}

			var ips []string

			if ep.Status.Networking != nil {
				for _, address := range ep.Status.Networking.Addressing {
					ips = append(ips, address.IP)
				}
			}
			return ips, nil
		},
	})

	informer = k8s.CCEClient().Informers.Cce().V2().ENIs().Informer()
	informer.AddIndexers(cache.Indexers{
		IndexIPToENI: func(obj interface{}) ([]string, error) {
			eni, ok := obj.(*ccev2.ENI)
			if !ok {
				return nil, fmt.Errorf("object is not ENI object")
			}

			var ips []string
			for _, addr := range eni.Spec.PrivateIPSet {
				ips = append(ips, addr.PrivateIPAddress)
			}
			for _, addr := range eni.Spec.IPV6PrivateIPSet {
				ips = append(ips, addr.PrivateIPAddress)
			}
			return ips, nil
		},
	})
}

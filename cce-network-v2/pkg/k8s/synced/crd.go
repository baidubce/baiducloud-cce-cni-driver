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

// Package synced provides tools for tracking if k8s resources have
// been initially sychronized with the k8s apiserver.
package synced

import (
	"context"
	"errors"
	"time"

	apiextclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	v1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/informer"
	k8sversion "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/version"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	slim_metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	k8sAPIGroupCRD = "CustomResourceDefinition"
)

func CRDResourceName(crd string) string {
	return "crd:" + crd
}

func agentCRDResourceNames() []string {
	result := []string{
		CRDResourceName(v2.NRSName),
		CRDResourceName(v2.PSTSName),
		CRDResourceName(v1.SubnetName),
		CRDResourceName(v2alpha1.KindNetResouceConfigSet + "." + v2alpha1.GroupName),
	}

	if !option.Config.DisableCCEEndpointCRD {
		result = append(result, CRDResourceName(v2.CEPName))
	}
	if !option.Config.DisableENICRD {
		result = append(result, CRDResourceName(v2.ENIName))
	}
	return result
}

// AgentCRDResourceNames returns a list of all CRD resource names the CCE
// agent needs to wait to be registered before initializing any k8s watchers.
func AgentCRDResourceNames() []string {
	return agentCRDResourceNames()
}

// AllCRDResourceNames returns a list of all CRD resource names that the
// clustermesh-apiserver or testsuite may register.
func AllCRDResourceNames() []string {
	return agentCRDResourceNames()
}

// SyncCRDs will sync CCE CRDs to ensure that they have all been
// installed inside the K8s cluster. These CRDs are added by the
// CCE Operator. This function will block until it finds all the
// CRDs or if a timeout occurs.
func SyncCRDs(ctx context.Context, crdNames []string, rs *Resources, ag *APIGroups) error {
	crds := newCRDState(crdNames)

	listerWatcher := newListWatchFromClient(
		newCRDGetter(k8s.WatcherAPIExtClient()),
		fields.Everything(),
	)
	_, crdController := informer.NewInformer(
		listerWatcher,
		&slim_metav1.PartialObjectMetadata{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj interface{}) { crds.add(obj) },
			DeleteFunc: func(obj interface{}) { crds.remove(obj) },
		},
		nil,
	)

	// Create a context so that we can timeout after the configured CRD wait
	// peroid.
	ctx, cancel := context.WithTimeout(ctx, option.Config.CRDWaitTimeout)
	defer cancel()

	crds.Lock()
	for crd := range crds.m {
		rs.BlockWaitGroupToSyncResources(
			ctx.Done(),
			nil,
			func() bool {
				crds.Lock()
				defer crds.Unlock()
				return crds.m[crd]
			},
			crd,
		)
	}
	crds.Unlock()

	// The above loop will call blockWaitGroupToSyncResources to populate the
	// K8sWatcher state with the current state of the CRDs. It will check the
	// state of each CRD, with the inline function provided. If the function
	// reports that the given CRD is true (has been synced), it will close a
	// channel associated with the given CRD. A subsequent call to
	// (*K8sWatcher).WaitForCacheSync will notice that a given CRD's channel
	// has been closed. Once all the CRDs passed to WaitForCacheSync have had
	// their channels closed, the function unblocks.
	//
	// Meanwhile, the below code kicks off the controller that was instantiated
	// above, and enters a loop looking for (1) if the context has deadlined or
	// (2) if the entire CRD state has been synced (all CRDs found in the
	// cluster). While we're in for-select loop, the controller is listening
	// for either add or delete events to the customresourcedefinition resource
	// (disguised inside a metav1.PartialObjectMetadata object). If (1) is
	// encountered, then CCE will fatal because it cannot proceed if the
	// CRDs are not present. If (2) is encountered, then make sure the
	// controller has exited by cancelling the context and we return out.

	go crdController.Run(ctx.Done())
	ag.AddAPI(k8sAPIGroupCRD)
	// We no longer need this API to show up in `cce status` as the
	// controller will exit after this function.
	defer ag.RemoveAPI(k8sAPIGroupCRD)

	log.Info("Waiting until all CCE CRDs are available")

	ticker := time.NewTicker(1 * time.Second)
	for {
		select {
		case <-ctx.Done():
			err := ctx.Err()
			if err != nil && !errors.Is(err, context.Canceled) {
				log.WithError(err).
					Fatalf("Unable to find all CCE CRDs necessary within "+
						"%v timeout. Please ensure that CCE Operator is "+
						"running, as it's responsible for registering all "+
						"the CCE CRDs. The following CRDs were not found: %v",
						option.Config.CRDWaitTimeout, crds.unSynced())
			}
			// If the context was canceled it means the daemon is being stopped
			// so we can return the context's error.
			return err
		case <-ticker.C:
			if crds.isSynced() {
				ticker.Stop()
				log.Info("All CCE CRDs have been found and are available")
				return nil
			}
		}
	}
}

func (s *crdState) add(obj interface{}) {
	if pom := k8s.ObjToV1PartialObjectMetadata(obj); pom != nil {
		s.Lock()
		s.m[CRDResourceName(pom.GetName())] = true
		s.Unlock()
	}
}

func (s *crdState) remove(obj interface{}) {
	if pom := k8s.ObjToV1PartialObjectMetadata(obj); pom != nil {
		s.Lock()
		s.m[CRDResourceName(pom.GetName())] = false
		s.Unlock()
	}
}

// isSynced returns whether all the CRDs inside `m` have all been synced,
// meaning all CRDs we care about in CCE exist in the cluster.
func (s *crdState) isSynced() bool {
	s.Lock()
	defer s.Unlock()
	for _, synced := range s.m {
		if !synced {
			return false
		}
	}
	return true
}

// unSynced returns a slice containing all CRDs that currently have not been
// synced.
func (s *crdState) unSynced() []string {
	s.Lock()
	defer s.Unlock()
	u := make([]string, 0, len(s.m))
	for crd, synced := range s.m {
		if !synced {
			u = append(u, crd)
		}
	}
	return u
}

// crdState contains the state of the CRDs inside the cluster.
type crdState struct {
	lock.Mutex

	// m is a map which maps the CRD name to its synced state in the cluster.
	// True means it exists, false means it doesn't exist.
	m map[string]bool
}

func newCRDState(crds []string) crdState {
	m := make(map[string]bool, len(crds))
	for _, name := range crds {
		m[name] = false
	}
	return crdState{
		m: m,
	}
}

// newListWatchFromClient is a copy of the NewListWatchFromClient from the
// "k8s.io/client-go/tools/cache" package, with many alterations made to
// efficiently retrieve CCE CRDs. Efficient retrieval is important because
// we don't want each agent to fetch the full CRDs across the cluster, because
// they potentially contain large validation schemas.
//
// This function also removes removes unnecessary calls from the upstream
// version that set the namespace and the resource when performing `Get`.
//
//   - If the resource was set, the following error was observed:
//     "customresourcedefinitions.apiextensions.k8s.io
//     "customresourcedefinitions" not found".
//   - If the namespace was set, the following error was observed:
//     "an empty namespace may not be set when a resource name is provided".
//
// The namespace problem can be worked around by using NamespaceIfScoped, but
// it's been omitted entirely here because it's equivalent in functionality.
func newListWatchFromClient(
	c cache.Getter,
	fieldSelector fields.Selector,
) *cache.ListWatch {
	optionsModifier := func(options *metav1.ListOptions) {
		options.FieldSelector = fieldSelector.String()
	}

	listFunc := func(options metav1.ListOptions) (runtime.Object, error) {
		optionsModifier(&options)

		// This lister will retrieve the CRDs as a
		// metav1{,v1beta1}.PartialObjectMetadataList object.
		getter := c.Get()
		// Setting this special header allows us to retrieve the objects the
		// same way that `kubectl get crds` does, except that kubectl retrieves
		// them as a collection inside a metav1{,v1beta1}.Table. Either way, we
		// request the CRDs in a metav1,{v1beta1}.PartialObjectMetadataList
		// object which contains individual metav1.PartialObjectMetadata
		// objects, containing the minimal representation of objects in K8s (in
		// this case a CRD). This matches with what the controller (informer)
		// expects as it wants a list type.
		getter = getter.SetHeader("Accept", pomListHeader)

		t := &slim_metav1.PartialObjectMetadataList{}
		if err := getter.
			VersionedParams(&options, metav1.ParameterCodec).
			Do(context.TODO()).
			Into(t); err != nil {
			return nil, err
		}

		return t, nil
	}
	watchFunc := func(options metav1.ListOptions) (watch.Interface, error) {
		optionsModifier(&options)

		getter := c.Get()
		// This watcher will retrieve each CRD that the lister has listed
		// as individual metav1.PartialObjectMetadata because it is
		// requesting the apiserver to return objects as such via the
		// "Accept" header.
		getter = getter.SetHeader("Accept", pomHeader)

		options.Watch = true
		return getter.
			VersionedParams(&options, metav1.ParameterCodec).
			Watch(context.TODO())
	}
	return &cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}

const (
	pomListHeader = "application/json;as=PartialObjectMetadataList;v=v1;g=meta.k8s.io,application/json;as=PartialObjectMetadataList;v=v1beta1;g=meta.k8s.io,application/json"
	pomHeader     = "application/json;as=PartialObjectMetadata;v=v1;g=meta.k8s.io,application/json;as=PartialObjectMetadata;v=v1beta1;g=meta.k8s.io,application/json"
)

// Get instantiates a GET request from the K8s REST client to retrieve CRDs. We
// define this getter because it's necessary to use the correct apiextensions
// client (v1 or v1beta1) in order to retrieve the CRDs in a
// backwards-compatible way. This implements the cache.Getter interface.
func (c *crdGetter) Get() *rest.Request {
	var req *rest.Request

	if k8sversion.Capabilities().APIExtensionsV1CRD {
		req = c.api.ApiextensionsV1().
			RESTClient().
			Get().
			Name("customresourcedefinitions")
	} else {
		req = c.api.ApiextensionsV1beta1().
			RESTClient().
			Get().
			Name("customresourcedefinitions")
	}

	return req
}

type crdGetter struct {
	api apiextclientset.Interface
}

func newCRDGetter(c apiextclientset.Interface) *crdGetter {
	return &crdGetter{api: c}
}

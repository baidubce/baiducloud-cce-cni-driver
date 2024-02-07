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

package k8s

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/comparator"
	dpTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/types"
	cce_v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	corev1 "k8s.io/api/core/v1"
	slim_discover_v1 "k8s.io/api/discovery/v1"
	slim_discover_v1beta1 "k8s.io/api/discovery/v1beta1"
	slim_networkingv1 "k8s.io/api/networking/v1"
	slim_metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ObjToV1NetworkPolicy(obj interface{}) *slim_networkingv1.NetworkPolicy {
	k8sNP, ok := obj.(*slim_networkingv1.NetworkPolicy)
	if ok {
		return k8sNP
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		k8sNP, ok := deletedObj.Obj.(*slim_networkingv1.NetworkPolicy)
		if ok {
			return k8sNP
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 NetworkPolicy")
	return nil
}

func ObjToV1Ingress(obj interface{}) *slim_networkingv1.Ingress {
	ing, ok := obj.(*slim_networkingv1.Ingress)
	if ok {
		return ing
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		svc, ok := deletedObj.Obj.(*slim_networkingv1.Ingress)
		if ok {
			return svc
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid v1 Ingress")
	return nil
}

func ObjToV1Services(obj interface{}) *corev1.Service {
	svc, ok := obj.(*corev1.Service)
	if ok {
		return svc
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		svc, ok := deletedObj.Obj.(*corev1.Service)
		if ok {
			return svc
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 Service")
	return nil
}

func ObjToV1Endpoints(obj interface{}) *corev1.Endpoints {
	ep, ok := obj.(*corev1.Endpoints)
	if ok {
		return ep
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		ep, ok := deletedObj.Obj.(*corev1.Endpoints)
		if ok {
			return ep
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 Endpoints")
	return nil
}

func ObjToV1Beta1EndpointSlice(obj interface{}) *slim_discover_v1beta1.EndpointSlice {
	ep, ok := obj.(*slim_discover_v1beta1.EndpointSlice)
	if ok {
		return ep
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		ep, ok := deletedObj.Obj.(*slim_discover_v1beta1.EndpointSlice)
		if ok {
			return ep
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1beta1 EndpointSlice")
	return nil
}

func ObjToV1EndpointSlice(obj interface{}) *slim_discover_v1.EndpointSlice {
	ep, ok := obj.(*slim_discover_v1.EndpointSlice)
	if ok {
		return ep
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		ep, ok := deletedObj.Obj.(*slim_discover_v1.EndpointSlice)
		if ok {
			return ep
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 EndpointSlice")
	return nil
}

func ObjTov1Pod(obj interface{}) *corev1.Pod {
	pod, ok := obj.(*corev1.Pod)
	if ok {
		return pod
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		pod, ok := deletedObj.Obj.(*corev1.Pod)
		if ok {
			return pod
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 Pod")
	return nil
}

func ObjToV1Node(obj interface{}) *v1.Node {
	node, ok := obj.(*v1.Node)
	if ok {
		return node
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		node, ok := deletedObj.Obj.(*v1.Node)
		if ok {
			return node
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 Node")
	return nil
}

func ObjToV1Namespace(obj interface{}) *corev1.Namespace {
	ns, ok := obj.(*corev1.Namespace)
	if ok {
		return ns
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		ns, ok := deletedObj.Obj.(*corev1.Namespace)
		if ok {
			return ns
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 Namespace")
	return nil
}

func ObjToV1PartialObjectMetadata(obj interface{}) *slim_metav1.PartialObjectMetadata {
	pom, ok := obj.(*slim_metav1.PartialObjectMetadata)
	if ok {
		return pom
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		pom, ok := deletedObj.Obj.(*slim_metav1.PartialObjectMetadata)
		if ok {
			return pom
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 PartialObjectMetadata")
	return nil
}

func ObjToV1Secret(obj interface{}) *corev1.Secret {
	secret, ok := obj.(*corev1.Secret)
	if ok {
		return secret
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		secret, ok := deletedObj.Obj.(*corev1.Secret)
		if ok {
			return secret
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid k8s v1 Secret")
	return nil
}

func EqualV1Services(k8sSVC1, k8sSVC2 *corev1.Service, nodeAddressing dpTypes.NodeAddressing) bool {
	// Service annotations are used to mark services as global, shared, etc.
	if !comparator.MapStringEquals(k8sSVC1.GetAnnotations(), k8sSVC2.GetAnnotations()) {
		return false
	}

	// Please write all the equalness logic inside the K8sServiceInfo.Equals()
	// method.
	return false
}

// AnnotationsEqual returns whether the annotation with any key in
// relevantAnnotations is equal in anno1 and anno2.
func AnnotationsEqual(relevantAnnotations []string, anno1, anno2 map[string]string) bool {
	for _, an := range relevantAnnotations {
		if anno1[an] != anno2[an] {
			return false
		}
	}
	return true
}

func convertToK8sServicePorts(ports []v1.ServicePort) []corev1.ServicePort {
	if ports == nil {
		return nil
	}

	slimPorts := make([]corev1.ServicePort, 0, len(ports))
	for _, v1Port := range ports {
		slimPorts = append(slimPorts,
			corev1.ServicePort{
				Name:     v1Port.Name,
				Protocol: corev1.Protocol(v1Port.Protocol),
				Port:     v1Port.Port,
				NodePort: v1Port.NodePort,
			},
		)
	}
	return slimPorts
}

func ConvertToK8sV1ServicePorts(slimPorts []corev1.ServicePort) []v1.ServicePort {
	if slimPorts == nil {
		return nil
	}

	ports := make([]v1.ServicePort, 0, len(slimPorts))
	for _, port := range slimPorts {
		ports = append(ports,
			v1.ServicePort{
				Name:     port.Name,
				Protocol: v1.Protocol(port.Protocol),
				Port:     port.Port,
				NodePort: port.NodePort,
			},
		)
	}
	return ports
}

func convertToK8sServiceAffinityConfig(saCfg *v1.SessionAffinityConfig) *corev1.SessionAffinityConfig {
	if saCfg == nil {
		return nil
	}

	if saCfg.ClientIP == nil {
		return &corev1.SessionAffinityConfig{}
	}

	return &corev1.SessionAffinityConfig{
		ClientIP: &corev1.ClientIPConfig{
			TimeoutSeconds: saCfg.ClientIP.TimeoutSeconds,
		},
	}
}

func ConvertToK8sV1ServiceAffinityConfig(saCfg *corev1.SessionAffinityConfig) *v1.SessionAffinityConfig {
	if saCfg == nil {
		return nil
	}

	if saCfg.ClientIP == nil {
		return &v1.SessionAffinityConfig{}
	}

	return &v1.SessionAffinityConfig{
		ClientIP: &v1.ClientIPConfig{
			TimeoutSeconds: saCfg.ClientIP.TimeoutSeconds,
		},
	}
}

func convertToK8sLoadBalancerIngress(lbIngs []v1.LoadBalancerIngress) []corev1.LoadBalancerIngress {
	if lbIngs == nil {
		return nil
	}

	slimLBIngs := make([]corev1.LoadBalancerIngress, 0, len(lbIngs))
	for _, lbIng := range lbIngs {
		slimLBIngs = append(slimLBIngs,
			corev1.LoadBalancerIngress{
				IP: lbIng.IP,
			},
		)
	}
	return slimLBIngs
}

func ConvertToK8sV1LoadBalancerIngress(slimLBIngs []corev1.LoadBalancerIngress) []v1.LoadBalancerIngress {
	if slimLBIngs == nil {
		return nil
	}

	lbIngs := make([]v1.LoadBalancerIngress, 0, len(slimLBIngs))
	for _, lbIng := range slimLBIngs {
		ports := make([]v1.PortStatus, 0, len(lbIng.Ports))
		for _, port := range lbIng.Ports {
			ports = append(ports, v1.PortStatus{
				Port:     port.Port,
				Protocol: v1.Protocol(port.Protocol),
				Error:    port.Error,
			})
		}
		lbIngs = append(lbIngs,
			v1.LoadBalancerIngress{
				IP:       lbIng.IP,
				Hostname: lbIng.Hostname,
				Ports:    ports,
			},
		)
	}
	return lbIngs
}

// ConvertToK8sService converts a *v1.Service into a
// *corev1.Service or a cache.DeletedFinalStateUnknown into
// a cache.DeletedFinalStateUnknown with a *corev1.Service in its Obj.
// If the given obj can't be cast into either *corev1.Service
// nor cache.DeletedFinalStateUnknown, the original obj is returned.
func ConvertToK8sService(obj interface{}) interface{} {
	switch concreteObj := obj.(type) {
	case *v1.Service:
		return &corev1.Service{
			TypeMeta: slim_metav1.TypeMeta{
				Kind:       concreteObj.TypeMeta.Kind,
				APIVersion: concreteObj.TypeMeta.APIVersion,
			},
			ObjectMeta: slim_metav1.ObjectMeta{
				Name:            concreteObj.ObjectMeta.Name,
				Namespace:       concreteObj.ObjectMeta.Namespace,
				ResourceVersion: concreteObj.ObjectMeta.ResourceVersion,
				UID:             concreteObj.ObjectMeta.UID,
				Labels:          concreteObj.ObjectMeta.Labels,
				Annotations:     concreteObj.ObjectMeta.Annotations,
			},
			Spec: corev1.ServiceSpec{
				Ports:                 convertToK8sServicePorts(concreteObj.Spec.Ports),
				Selector:              concreteObj.Spec.Selector,
				ClusterIP:             concreteObj.Spec.ClusterIP,
				Type:                  corev1.ServiceType(concreteObj.Spec.Type),
				ExternalIPs:           concreteObj.Spec.ExternalIPs,
				SessionAffinity:       corev1.ServiceAffinity(concreteObj.Spec.SessionAffinity),
				LoadBalancerIP:        concreteObj.Spec.LoadBalancerIP,
				ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyType(concreteObj.Spec.ExternalTrafficPolicy),
				HealthCheckNodePort:   concreteObj.Spec.HealthCheckNodePort,
				SessionAffinityConfig: convertToK8sServiceAffinityConfig(concreteObj.Spec.SessionAffinityConfig),
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: convertToK8sLoadBalancerIngress(concreteObj.Status.LoadBalancer.Ingress),
				},
			},
		}
	case cache.DeletedFinalStateUnknown:
		svc, ok := concreteObj.Obj.(*v1.Service)
		if !ok {
			return obj
		}
		return cache.DeletedFinalStateUnknown{
			Key: concreteObj.Key,
			Obj: &corev1.Service{
				TypeMeta: slim_metav1.TypeMeta{
					Kind:       svc.TypeMeta.Kind,
					APIVersion: svc.TypeMeta.APIVersion,
				},
				ObjectMeta: slim_metav1.ObjectMeta{
					Name:            svc.ObjectMeta.Name,
					Namespace:       svc.ObjectMeta.Namespace,
					ResourceVersion: svc.ObjectMeta.ResourceVersion,
					UID:             svc.ObjectMeta.UID,
					Labels:          svc.ObjectMeta.Labels,
					Annotations:     svc.ObjectMeta.Annotations,
				},
				Spec: corev1.ServiceSpec{
					Ports:                 convertToK8sServicePorts(svc.Spec.Ports),
					Selector:              svc.Spec.Selector,
					ClusterIP:             svc.Spec.ClusterIP,
					Type:                  corev1.ServiceType(svc.Spec.Type),
					ExternalIPs:           svc.Spec.ExternalIPs,
					SessionAffinity:       corev1.ServiceAffinity(svc.Spec.SessionAffinity),
					LoadBalancerIP:        svc.Spec.LoadBalancerIP,
					ExternalTrafficPolicy: corev1.ServiceExternalTrafficPolicyType(svc.Spec.ExternalTrafficPolicy),
					HealthCheckNodePort:   svc.Spec.HealthCheckNodePort,
					SessionAffinityConfig: convertToK8sServiceAffinityConfig(svc.Spec.SessionAffinityConfig),
				},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{
						Ingress: convertToK8sLoadBalancerIngress(svc.Status.LoadBalancer.Ingress),
					},
				},
			},
		}
	default:
		return obj
	}
}

func convertToAddress(v1Addrs []v1.NodeAddress) []corev1.NodeAddress {
	if v1Addrs == nil {
		return nil
	}

	addrs := make([]corev1.NodeAddress, 0, len(v1Addrs))
	for _, addr := range v1Addrs {
		addrs = append(
			addrs,
			corev1.NodeAddress{
				Type:    corev1.NodeAddressType(addr.Type),
				Address: addr.Address,
			},
		)
	}
	return addrs
}

func convertToTaints(v1Taints []v1.Taint) []corev1.Taint {
	if v1Taints == nil {
		return nil
	}

	taints := make([]corev1.Taint, 0, len(v1Taints))
	for _, taint := range v1Taints {
		var ta *slim_metav1.Time
		if taint.TimeAdded != nil {
			t := slim_metav1.NewTime(taint.TimeAdded.Time)
			ta = &t
		}
		taints = append(
			taints,
			corev1.Taint{
				Key:       taint.Key,
				Value:     taint.Value,
				Effect:    corev1.TaintEffect(taint.Effect),
				TimeAdded: ta,
			},
		)
	}
	return taints
}

// ConvertToNode converts a *v1.Node into a
// *types.Node or a cache.DeletedFinalStateUnknown into
// a cache.DeletedFinalStateUnknown with a *types.Node in its Obj.
// If the given obj can't be cast into either *v1.Node
// nor cache.DeletedFinalStateUnknown, the original obj is returned.
// WARNING calling this function will set *all* fields of the given Node as
// empty.
func ConvertToNode(obj interface{}) interface{} {
	switch concreteObj := obj.(type) {
	case *v1.Node:
		p := &corev1.Node{
			TypeMeta: slim_metav1.TypeMeta{
				Kind:       concreteObj.TypeMeta.Kind,
				APIVersion: concreteObj.TypeMeta.APIVersion,
			},
			ObjectMeta: slim_metav1.ObjectMeta{
				Name:            concreteObj.ObjectMeta.Name,
				Namespace:       concreteObj.ObjectMeta.Namespace,
				UID:             concreteObj.ObjectMeta.UID,
				ResourceVersion: concreteObj.ObjectMeta.ResourceVersion,
				Labels:          concreteObj.ObjectMeta.Labels,
				Annotations:     concreteObj.ObjectMeta.Annotations,
			},
			Spec: corev1.NodeSpec{
				PodCIDR:  concreteObj.Spec.PodCIDR,
				PodCIDRs: concreteObj.Spec.PodCIDRs,
				Taints:   convertToTaints(concreteObj.Spec.Taints),
			},
			Status: corev1.NodeStatus{
				Addresses: convertToAddress(concreteObj.Status.Addresses),
			},
		}
		*concreteObj = v1.Node{}
		return p
	case cache.DeletedFinalStateUnknown:
		node, ok := concreteObj.Obj.(*v1.Node)
		if !ok {
			return obj
		}
		dfsu := cache.DeletedFinalStateUnknown{
			Key: concreteObj.Key,
			Obj: &corev1.Node{
				TypeMeta: slim_metav1.TypeMeta{
					Kind:       node.TypeMeta.Kind,
					APIVersion: node.TypeMeta.APIVersion,
				},
				ObjectMeta: slim_metav1.ObjectMeta{
					Name:            node.ObjectMeta.Name,
					Namespace:       node.ObjectMeta.Namespace,
					UID:             node.ObjectMeta.UID,
					ResourceVersion: node.ObjectMeta.ResourceVersion,
					Labels:          node.ObjectMeta.Labels,
					Annotations:     node.ObjectMeta.Annotations,
				},
				Spec: corev1.NodeSpec{
					PodCIDR:  node.Spec.PodCIDR,
					PodCIDRs: node.Spec.PodCIDRs,
					Taints:   convertToTaints(node.Spec.Taints),
				},
				Status: corev1.NodeStatus{
					Addresses: convertToAddress(node.Status.Addresses),
				},
			},
		}
		*node = v1.Node{}
		return dfsu
	default:
		return obj
	}
}

// ConvertToNetResourceSet converts a *cce_v2.NetResourceSet into a
// *cce_v2.NetResourceSet or a cache.DeletedFinalStateUnknown into
// a cache.DeletedFinalStateUnknown with a *cce_v2.NetResourceSet in its Obj.
// If the given obj can't be cast into either *cce_v2.NetResourceSet
// nor cache.DeletedFinalStateUnknown, the original obj is returned.
func ConvertToNetResourceSet(obj interface{}) interface{} {
	// TODO create a slim type of the NetResourceSet
	switch concreteObj := obj.(type) {
	case *cce_v2.NetResourceSet:
		return concreteObj
	case cache.DeletedFinalStateUnknown:
		netResourceSet, ok := concreteObj.Obj.(*cce_v2.NetResourceSet)
		if !ok {
			return obj
		}
		return cache.DeletedFinalStateUnknown{
			Key: concreteObj.Key,
			Obj: netResourceSet,
		}
	default:
		return obj
	}
}

// ObjToNetResourceSet attempts to cast object to a NetResourceSet object and
// returns the NetResourceSet objext if the cast succeeds. Otherwise, nil is returned.
func ObjToNetResourceSet(obj interface{}) *cce_v2.NetResourceSet {
	cn, ok := obj.(*cce_v2.NetResourceSet)
	if ok {
		return cn
	}
	deletedObj, ok := obj.(cache.DeletedFinalStateUnknown)
	if ok {
		// Delete was not observed by the watcher but is
		// removed from kube-apiserver. This is the last
		// known state and the object no longer exists.
		cn, ok := deletedObj.Obj.(*cce_v2.NetResourceSet)
		if ok {
			return cn
		}
	}
	log.WithField(logfields.Object, logfields.Repr(obj)).
		Warn("Ignoring invalid v2 NetResourceSet")
	return nil
}

// ConvertToCCEEndpoint converts a *cce_v2.NetResourceSet into a
// *cce_v2.NetResourceSet or a cache.DeletedFinalStateUnknown into
// a cache.DeletedFinalStateUnknown with a *cce_v2.NetResourceSet in its Obj.
// If the given obj can't be cast into either *cce_v2.NetResourceSet
// nor cache.DeletedFinalStateUnknown, the original obj is returned.
func ConvertToCCEEndpoint(obj interface{}) interface{} {
	// TODO create a slim type of the NetResourceSet
	switch concreteObj := obj.(type) {
	case *cce_v2.CCEEndpoint:
		return concreteObj
	case cache.DeletedFinalStateUnknown:
		netResourceSet, ok := concreteObj.Obj.(*cce_v2.CCEEndpoint)
		if !ok {
			return obj
		}
		return cache.DeletedFinalStateUnknown{
			Key: concreteObj.Key,
			Obj: netResourceSet,
		}
	default:
		return obj
	}
}

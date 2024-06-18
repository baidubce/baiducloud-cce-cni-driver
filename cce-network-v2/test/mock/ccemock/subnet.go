package ccemock

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
)

type ExceptResourceVersion struct {
	ResourceVersion string

	// Received flag to mark whatever object has been received
	Received bool
}

type ExceptResourceVersionMap map[string]*ExceptResourceVersion

func (m ExceptResourceVersionMap) Add(name, rv string) {
	if _, ok := m[name]; !ok {
		m[name] = &ExceptResourceVersion{
			ResourceVersion: rv,
		}
	}
}

func (m ExceptResourceVersionMap) Receive(name, rv string) bool {
	if except, ok := m[name]; ok {
		if except.ResourceVersion == rv {
			except.Received = true
		}
		return true
	}
	return false
}

func (m ExceptResourceVersionMap) IsAllObjectReceived() bool {
	for _, except := range m {
		if !except.Received {
			return false
		}
	}
	return true
}

type CreateObjectFunc func(context.Context) []metav1.Object

func EnsureObjectToInformer(t *testing.T, informer cache.SharedIndexInformer, fun CreateObjectFunc) error {
	var (
		ctx                = context.Background()
		resourceVersionMap = make(ExceptResourceVersionMap)
	)

	// wait all object
	informer.AddEventHandler(&cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if actual, ok := obj.(metav1.Object); ok {
				resourceVersionMap.Receive(actual.GetName(), actual.GetResourceVersion())
			}
		},
	})

	for _, obj := range fun(ctx) {
		resourceVersionMap.Add(obj.GetName(), obj.GetResourceVersion())
	}

outter:
	for {
		select {
		case <-time.After(wait.ForeverTestTimeout):
			return errors.New("timeout wait informer sync")
		default:
			if resourceVersionMap.IsAllObjectReceived() {
				break outter
			}
		}
	}
	return nil
}

func EnsureSubnetIDsToInformer(t *testing.T, vpcID string, subnetIDs []string) error {
	createFunc := func(ctx context.Context) []metav1.Object {
		var toWaitObj []metav1.Object

		for i, subnetID := range subnetIDs {
			cidr := fmt.Sprintf("10.58.%d.0/24", i)
			sbn := NewMockSubnet(subnetID, cidr)
			sbn.Spec.VPCID = vpcID
			_, err := k8s.CCEClient().CceV1().Subnets().Get(ctx, subnetID, metav1.GetOptions{})
			if err == nil {
				continue
			}
			result, err := k8s.CCEClient().CceV1().Subnets().Create(ctx, sbn, metav1.CreateOptions{})
			if err == nil {
				toWaitObj = append(toWaitObj, result)
			}
		}
		return toWaitObj
	}
	return EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V1().Subnets().Informer(), createFunc)
}

func EnsureSubnetsToInformer(t *testing.T, subnets []*ccev1.Subnet) error {
	createFunc := func(ctx context.Context) []metav1.Object {
		var toWaitObj []metav1.Object

		for _, sbn := range subnets {
			_, err := k8s.CCEClient().CceV1().Subnets().Get(ctx, sbn.Name, metav1.GetOptions{})
			if err == nil {
				continue
			}
			result, err := k8s.CCEClient().CceV1().Subnets().Create(ctx, sbn, metav1.CreateOptions{})
			if err == nil {
				toWaitObj = append(toWaitObj, result)
			}
		}
		return toWaitObj
	}
	return EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V1().Subnets().Informer(), createFunc)
}

func NewMockSubnet(name, cidr string) *ccev1.Subnet {
	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ccev1.SubnetSpec{
			ID:               name,
			Name:             name,
			AvailabilityZone: "cn-bj-d",
			CIDR:             cidr,
		},
		Status: ccev1.SubnetStatus{
			Enable:         true,
			AvailableIPNum: 255,
		},
	}
	return sbn
}

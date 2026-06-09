package ccemock

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
		// In tests, object may be quickly updated by controllers before informer
		// receives the initial Add event. Accept same or newer resourceVersion as received.
		if except.ResourceVersion == rv {
			except.Received = true
			return true
		}
		expectedRV, expectedErr := strconv.ParseInt(except.ResourceVersion, 10, 64)
		actualRV, actualErr := strconv.ParseInt(rv, 10, 64)
		if expectedErr != nil || actualErr != nil || actualRV >= expectedRV {
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
		UpdateFunc: func(oldObj, newObj interface{}) {
			if actual, ok := newObj.(metav1.Object); ok {
				resourceVersionMap.Receive(actual.GetName(), actual.GetResourceVersion())
			}
		},
	})

	for _, obj := range fun(ctx) {
		resourceVersionMap.Add(obj.GetName(), obj.GetResourceVersion())
	}

	if err := wait.PollImmediate(10*time.Millisecond, wait.ForeverTestTimeout, func() (bool, error) {
		return resourceVersionMap.IsAllObjectReceived(), nil
	}); err != nil {
		return errors.New("timeout wait informer sync")
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
			VPCID:            "vpc-test",
		},
		Status: ccev1.SubnetStatus{
			Enable:         true,
			AvailableIPNum: 255,
		},
	}
	return sbn
}

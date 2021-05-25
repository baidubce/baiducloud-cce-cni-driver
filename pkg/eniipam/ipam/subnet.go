package ipam

import (
	"context"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
)

func CreateSubnetCR(ctx context.Context, cloud cloud.Interface, crdClient versioned.Interface, subnetID string) error {
	resp, err := cloud.DescribeSubnet(ctx, subnetID)
	if err != nil {
		return err
	}

	s := &v1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subnetID,
			Namespace: v1.NamespaceDefault,
		},
		Spec: v1alpha1.SubnetSpec{
			ID:               resp.SubnetId,
			Name:             resp.Name,
			AvailabilityZone: resp.ZoneName,
			CIDR:             resp.Cidr,
		},
		Status: v1alpha1.SubnetStatus{
			AvailableIPNum: resp.AvailableIp,
			Enable:         true,
			HasNoMoreIP:    false,
		},
	}

	_, err = crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Create(s)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	return nil
}

package ipam

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
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

func DeclareSubnetHasNoMoreIP(
	ctx context.Context,
	crdClient versioned.Interface,
	crdInformer crdinformers.SharedInformerFactory,
	subnetID string,
	hasNoMoreIP bool,
) error {
	subnet, err := crdInformer.Cce().V1alpha1().Subnets().Lister().Subnets(v1.NamespaceDefault).Get(subnetID)
	if err != nil {
		return err
	}

	status := subnet.Status

	// the status is different, perform an update
	if status.HasNoMoreIP != hasNoMoreIP {
		status.HasNoMoreIP = hasNoMoreIP
		err = PatchSubnetStatus(ctx, crdClient, subnet.Name, &status)
		if err != nil {
			return err
		}
	}

	return nil
}

func PatchSubnetStatus(ctx context.Context, crdClient versioned.Interface, name string, status *v1alpha1.SubnetStatus) error {
	json, err := json.Marshal(status)
	if err != nil {
		return err
	}

	patchData := []byte(fmt.Sprintf(`{"status":%s}`, json))
	_, err = crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Patch(name, ktypes.MergePatchType, patchData)
	if err != nil {
		return err
	}

	return nil
}

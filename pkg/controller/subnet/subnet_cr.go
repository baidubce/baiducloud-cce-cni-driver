package subnet

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ktypes "k8s.io/apimachinery/pkg/types"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	bcecloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func CreateSubnetCR(ctx context.Context, cloud bcecloud.Interface, crdClient versioned.Interface, subnetID string) error {
	var s = &v1alpha1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subnetID,
			Namespace: v1.NamespaceDefault,
		},
		Spec: v1alpha1.SubnetSpec{
			ID: subnetID,
		},
	}
	resp, err := cloud.DescribeSubnet(ctx, subnetID)
	if err != nil {
		// If the subnet does not exist on the corresponding VPC,
		// create an unavailable subnet object
		if bcecloud.IsErrorReasonNoSuchObject(err) {
			s.Status = v1alpha1.SubnetStatus{
				Enable:         false,
				Reason:         string(bcecloud.ErrorReasonNoSuchObject),
				AvailableIPNum: 0,
				HasNoMoreIP:    true,
			}
			goto patch
		}
		return err
	}

	s = &v1alpha1.Subnet{
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

patch:
	_, err = crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Create(ctx, s, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}

	return nil
}

// MarkExclusiveSubnet
// exclusive true : mark as a exclusive subnet
func MarkExclusiveSubnet(ctx context.Context, crdClient versioned.Interface, subnetID string, exclusive bool) (*v1alpha1.Subnet, error) {
	patchData := []byte(fmt.Sprintf(`{"spec":{"exclusive": %t}}`, exclusive))
	return crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Patch(ctx, subnetID, ktypes.MergePatchType, patchData, metav1.PatchOptions{})
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

	sbn := subnet.DeepCopy()
	// the status is different, perform an update
	if sbn.Status.HasNoMoreIP != hasNoMoreIP {
		sbn.Status.HasNoMoreIP = hasNoMoreIP
		return UpdateSubnet(ctx, crdClient, sbn)
	}
	return nil
}

func UpdateSubnet(ctx context.Context, crdClient versioned.Interface, sbn *v1alpha1.Subnet) error {
	_, err := crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Update(ctx, sbn, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	logger.V(5).Infof(ctx, "update subnet %s success : %s", sbn.Name, logger.ToJson(sbn))
	return nil
}

func GetSubnet(ctx context.Context, crdClient versioned.Interface, subnetID string) (*v1alpha1.Subnet, error) {
	return crdClient.CceV1alpha1().Subnets(v1.NamespaceDefault).Get(ctx, subnetID, metav1.GetOptions{})
}

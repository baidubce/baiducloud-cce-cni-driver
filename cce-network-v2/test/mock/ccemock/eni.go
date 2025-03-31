package ccemock

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/allocator"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// NewMockEni create new ready bcc eni, you should update fields after created
// Parameters:
//
//	ready: whatever the eni is ready
func NewMockEni(nodeName, instanceID, subnetID, cidrstr string, ipNum int) (*ccev2.ENI, error) {
	eniID := AppendHashString("eni")
	sbnCidr := cidr.MustParseCIDR(cidrstr)

	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: eniID,
			Labels: map[string]string{
				k8s.LabelInstanceID: instanceID,
				k8s.LabelNodeName:   nodeName,
			},
		},
		Spec: ccev2.ENISpec{
			NodeName:                  nodeName,
			UseMode:                   ccev2.ENIUseModeSecondaryIP,
			InstallSourceBasedRouting: true,
			VPCVersion:                1,
			Type:                      ccev2.ENIForBCC,
			ENI: models.ENI{
				Description:      defaults.DefaultENIDescription,
				Name:             CreateNameForENI("cce-test", instanceID, nodeName),
				ID:               eniID,
				InstanceID:       instanceID,
				SubnetID:         subnetID,
				ZoneName:         "cn-bj-d",
				VpcID:            "vpc-test",
				MacAddress:       "02:18:9b:6e:3d:4f",
				SecurityGroupIds: []string{"sg-test"},
			},
		},
		Status: ccev2.ENIStatus{},
	}

	// allocate ip to eni
	if ipNum > 0 {
		// 1. set eni status to ready
		eni.Status = ccev2.ENIStatus{
			GatewayIPv4:   sbnCidr.IP.String(),
			InterfaceName: "cce-eni-0",
			VPCStatus:     ccev2.VPCENIStatusInuse,
			VPCStatusChangeLog: []ccev2.ENIStatusChange{
				{
					VPCStatus: ccev2.VPCENIStatusInuse,
					StatusChange: ccev2.StatusChange{
						Code: "200",
						Time: metav1.Now(),
					},
				},
			},
			CCEStatus: ccev2.ENIStatusReadyOnNode,
			CCEStatusChangeLog: []ccev2.ENIStatusChange{
				{
					CCEENIStatus: ccev2.ENIStatusReadyOnNode,
					StatusChange: ccev2.StatusChange{
						Code: "200",
						Time: metav1.Now(),
					},
				},
			},
			VPCVersion: eni.Spec.VPCVersion,
		}

		// 2. add private ip to eni
		allocator, err := allocator.NewSubRangePoolAllocator(ipamTypes.PoolID(subnetID), sbnCidr, 2)
		if err != nil {
			return nil, err
		}
		ips, err := allocator.AllocateMany(ipNum)
		if err != nil {
			return nil, fmt.Errorf("allocate ip failed: %w", err)
		}
		for i, ip := range ips {
			eni.Spec.ENI.PrivateIPSet = append(eni.Spec.ENI.PrivateIPSet, &models.PrivateIP{
				Primary:          i == 0,
				GatewayIP:        sbnCidr.IP.String(),
				SubnetID:         subnetID,
				PrivateIPAddress: ip.String(),
			})
		}
	}

	return eni, nil
}

func AppendHashString(prefix string) string {
	hash := sha1.Sum([]byte(time.Now().String()))
	suffix := hex.EncodeToString(hash[:])
	return fmt.Sprintf("%s-%s", prefix, suffix[:12])
}

func CreateNameForENI(clusterID, instanceID, nodeName string) string {
	hash := sha1.Sum([]byte(time.Now().String()))
	suffix := hex.EncodeToString(hash[:])

	// eni name length is 64
	name := fmt.Sprintf("%s/%s/%s", clusterID, instanceID, nodeName)
	if len(name) > 57 {
		name = name[:57]
	}
	return fmt.Sprintf("%s/%s", name, suffix[:6])
}

func EnsureEnisToInformer(t *testing.T, enis []*ccev2.ENI) error {
	createFunc := func(ctx context.Context) []metav1.Object {
		var toWaitObj []metav1.Object

		lister := k8s.CCEClient().Informers.Cce().V2().ENIs().Lister()
		for _, eni := range enis {
			_, err := lister.Get(eni.Name)
			if err == nil {
				continue
			}

			result, err := k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{})
			if err == nil {
				toWaitObj = append(toWaitObj, result)
			}
		}
		return toWaitObj
	}
	return EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), createFunc)
}

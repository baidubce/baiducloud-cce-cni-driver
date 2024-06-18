package vpceni

import (
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	cloudtesting "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
)

func newMockInstancesManager(t *testing.T) *InstancesManager {
	mockCtl := gomock.NewController(t)
	mockCloudInterface := cloudtesting.NewMockInterface(mockCtl)

	return newInstancesManager(mockCloudInterface,
		k8s.CCEClient().Informers.Cce().V2().ENIs().Lister(),
		k8s.CCEClient().Informers.Cce().V1().Subnets().Lister(),
		watchers.CCEEndpointClient,
	)
}

func (im *InstancesManager) GetMockCloudInterface() *cloudtesting.MockInterface {
	return im.bceclient.(*cloudtesting.MockInterface)
}

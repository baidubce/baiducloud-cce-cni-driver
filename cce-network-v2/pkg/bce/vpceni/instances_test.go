package vpceni

import (
	"github.com/golang/mock/gomock"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	cloudtesting "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
)

func newMockInstancesManager(mockCtl *gomock.Controller) *InstancesManager {
	mockCloudInterface := cloudtesting.NewMockInterface(mockCtl)

	im := newInstancesManager(mockCloudInterface,
		k8s.CCEClient().Informers.Cce().V2().ENIs().Lister(),
		k8s.CCEClient().Informers.Cce().V1().Subnets().Lister(),
		watchers.CCEEndpointClient,
	)
	im.nrsGetterUpdater = watchers.NetResourceSetClient
	return im
}

func (im *InstancesManager) GetMockCloudInterface() *cloudtesting.MockInterface {
	return im.bceclient.(*cloudtesting.MockInterface)
}

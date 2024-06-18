package ccemock

import (
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"

	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
)

func NewMockClient() {
	stopCh := make(chan struct{})
	k8s.InitNewFakeK8sClient()
	watchers.StartWatchers(stopCh)
	k8s.WatcherClient().Informers.WaitForCacheSync(stopCh)
	k8s.CCEClient().Informers.WaitForCacheSync(stopCh)
}

func InitMockEnv() error {
	operatorOption.Config.BCECloudContry = "cn"
	operatorOption.Config.BCECloudRegion = "bj"
	operatorOption.Config.EnableIPv4 = true
	NewMockClient()

	return nil
}

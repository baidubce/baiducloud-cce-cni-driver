package bcesync

import (
	"testing"

	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/stretchr/testify/assert"
)

func TestBorrowedSubnet_Borrow(t *testing.T) {
	mocksbn := ccemock.NewMockSubnet("sbnid", "176.16.0.0/24")
	bs := NewBorrowedSubnet(mocksbn)
	assert.Equal(t, 255, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 0, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	num := bs.Borrow("eni1", 1)
	assert.Equal(t, 1, num)
	assert.Equal(t, 254, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 1, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	num = bs.Borrow("eni1", 2)
	assert.Equal(t, 2, num)
	assert.Equal(t, 253, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 2, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	bs.forceBorrowForENI("eni1", 10)
	assert.Equal(t, 245, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 10, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	bs.Done("eni1", 3)
	assert.Equal(t, 245, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 7, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)

	bs.Cancel("eni1")
	assert.Equal(t, 252, bs.BorrowedAvailableIPsCount)
	assert.Equal(t, 0, bs.BorrowedIPsCount)
	assert.Equal(t, 255, bs.Status.AvailableIPNum)
}

func Test_restoreBSM(t *testing.T) {
	ccemock.InitMockEnv()
	InitBSM()

	sbn := ccemock.NewMockSubnet("sbn-bbcprimary", "10.128.34.0/24")
	err := ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return
	}

	// to borrow
	nrs := ccemock.NewMockSimpleNrs("10.128.34.56", "bcc")
	nrs.Spec.ENI.MaxIPsPerENI = 40
	nrs.Spec.ENI.MaxAllocateENI = 8
	nrs.Spec.ENI.BurstableMehrfachENI = 1
	nrs.Annotations = map[string]string{}
	err = ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs})
	if !assert.NoError(t, err, "ensure nrs to informer failed") {
		return
	}
	eni, err := ccemock.NewMockEni("10.128.34.56", "i-wqdasds", "sbn-bbcprimary", "10.128.34.0/24", 0)
	eni.Spec.BorrowIPCount = 40
	assert.NoError(t, err, "create eni failed")
	err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni})
	if !assert.NoError(t, err, "ensure enis to informer failed") {
		return
	}

	// no borrowed
	eni, err = ccemock.NewMockEni("10.128.34.56", "i-wqdasds", "sbn-bbcprimary", "10.128.34.0/24", 40)
	assert.NoError(t, err, "create eni failed")
	err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{eni})
	if !assert.NoError(t, err, "ensure enis to informer failed") {
		return
	}

	InitBSM()

	bsb, err := GlobalBSM().EnsureSubnet(sbn.Spec.VPCID, sbn.Name)
	if !assert.NoError(t, err, "get subnet failed") {
		return
	}

	assert.Equal(t, 40, bsb.BorrowedIPsCount)
	assert.Equal(t, 215, bsb.BorrowedAvailableIPsCount)
}

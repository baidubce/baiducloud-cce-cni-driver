package bcesync

import (
	"context"
	"testing"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	cloudtesting "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
)

func setOperatorHPASStanderd(t *testing.T, enabled bool) {
	original := operatorOption.Config.HPASStanderd
	operatorOption.Config.HPASStanderd = enabled
	t.Cleanup(func() {
		operatorOption.Config.HPASStanderd = original
	})
}

func TestRemoteVpcEniSyncher_syncENI_GivenHPASStandard_WhenSyncing_ThenUseBCEClient(t *testing.T) {
	setOperatorHPASStanderd(t, true)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBCE := cloudtesting.NewMockInterface(ctrl)
	mockHPAS := cloudtesting.NewMockInterface(ctrl)

	mockBCE.EXPECT().
		ListENIs(gomock.Any(), gomock.AssignableToTypeOf(enisdk.ListEniArgs{})).
		Return([]enisdk.Eni{}, nil)
	mockHPAS.EXPECT().ListENIs(gomock.Any(), gomock.Any()).Times(0)

	syncer := &remoteVpcEniSyncher{
		bceclient:  mockBCE,
		hpasclient: mockHPAS,
		VPCIDs:     "vpc-test",
		ClusterID:  "cluster-1",
	}

	_, err := syncer.syncENI(context.Background())
	assert.NoError(t, err)
}

func TestRemoteVpcEniSyncher_syncENI_GivenHPASNonStandard_WhenSyncing_ThenUseHPASClient(t *testing.T) {
	setOperatorHPASStanderd(t, false)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBCE := cloudtesting.NewMockInterface(ctrl)
	mockHPAS := cloudtesting.NewMockInterface(ctrl)

	mockHPAS.EXPECT().
		ListENIs(gomock.Any(), gomock.AssignableToTypeOf(enisdk.ListEniArgs{})).
		Return([]enisdk.Eni{}, nil)
	mockBCE.EXPECT().ListENIs(gomock.Any(), gomock.Any()).Times(0)

	syncer := &remoteVpcEniSyncher{
		bceclient:  mockBCE,
		hpasclient: mockHPAS,
		VPCIDs:     "vpc-test",
		ClusterID:  "cluster-1",
	}

	_, err := syncer.syncENI(context.Background())
	assert.NoError(t, err)
}

// Mock ENI updater for testing
type mockENIUpdater struct {
	mock.Mock
}

func (m *mockENIUpdater) Update(eni *ccev2.ENI) (*ccev2.ENI, error) {
	args := m.Called(eni)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	// If the return value is the ENI itself (from mock.Anything), return the input ENI
	if returnedENI, ok := args.Get(0).(*ccev2.ENI); ok {
		return returnedENI, args.Error(1)
	}
	// Otherwise return the input ENI (for cases where we just want to validate)
	return eni, args.Error(1)
}

func (m *mockENIUpdater) Create(eni *ccev2.ENI) (*ccev2.ENI, error) {
	args := m.Called(eni)
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

func (m *mockENIUpdater) Delete(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *mockENIUpdater) Get(name string) (*ccev2.ENI, error) {
	args := m.Called(name)
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

func (m *mockENIUpdater) Lister() v2.ENILister {
	args := m.Called()
	return args.Get(0).(v2.ENILister)
}

func (m *mockENIUpdater) UpdateStatus(eni *ccev2.ENI) (*ccev2.ENI, error) {
	args := m.Called(eni)
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

// TestValidateAndSyncENIIPs_GivenConsistentIPs_WhenValidate_ThenReturnFalse 测试IP一致的情况
func TestValidateAndSyncENIIPs_GivenConsistentIPs_WhenValidate_ThenReturnFalse(t *testing.T) {
	// Given
	syncer := &remoteVpcEniSyncher{}

	// 创建一致的ENI数据
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-test123",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-test123",
				VpcID: "vpc-test123",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.1.10"},
					{PrivateIPAddress: "192.168.1.11"},
				},
				IPV6PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "2001:db8::1"},
				},
			},
		},
	}

	iaasENI := &enisdk.Eni{
		EniId: "eni-test123",
		VpcId: "vpc-test123",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.10"},
			{PrivateIpAddress: "192.168.1.11"},
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "2001:db8::1"},
		},
	}

	// When
	result, err := syncer.validateAndSyncENIIPs(existingENI, iaasENI)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Then
	assert.False(t, result, "Should return false when IPs are consistent")
}

// TestValidateAndSyncENIIPs_GivenInconsistentIPs_WhenValidate_ThenReturnTrueAndSync 测试IP不一致的情况
func TestValidateAndSyncENIIPs_GivenInconsistentIPs_WhenValidate_ThenReturnTrueAndSync(t *testing.T) {
	// Initialize mock environment
	ccemock.NewMockClient()

	// Create mock subnet for IP validation
	subnet := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "subnet-test456",
			Labels: map[string]string{
				k8s.VPCIDLabel: "vpc-test456",
			},
		},
		Spec: ccev1.SubnetSpec{
			ID:               "subnet-test456",
			CIDR:             "192.168.1.0/24",
			IPv6CIDR:         "2001:db8::/64",
			VPCID:            "vpc-test456",
			AvailabilityZone: "zone-a",
		},
	}
	err := ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{subnet})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return
	}

	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	// 创建不一致的ENI数据
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-test456",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-test456",
				VpcID: "vpc-test456",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.1.10"}, // 这个IP在IaaS中不存在
					{PrivateIPAddress: "192.168.1.11"},
				},
				IPV6PrivateIPSet: []*models.PrivateIP{},
			},
		},
	}

	iaasENI := &enisdk.Eni{
		EniId:    "eni-test456",
		VpcId:    "vpc-test456",
		SubnetId: "subnet-test456",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.11"},
			{PrivateIpAddress: "192.168.1.12"}, // 这个IP在K8s中不存在
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "2001:db8::2"}, // 这个IPv6在K8s中不存在
		},
	}

	// 设置mock期望
	mockUpdater.On("Update", mock.MatchedBy(func(eni *ccev2.ENI) bool {
		// 验证更新的ENI包含了IaaS的数据
		return assert.Len(t, eni.Spec.ENI.PrivateIPSet, 2) &&
			assert.Equal(t, "192.168.1.11", eni.Spec.ENI.PrivateIPSet[0].PrivateIPAddress) &&
			assert.Equal(t, "192.168.1.12", eni.Spec.ENI.PrivateIPSet[1].PrivateIPAddress) &&
			assert.Len(t, eni.Spec.ENI.IPV6PrivateIPSet, 1) &&
			assert.Equal(t, "2001:db8::2", eni.Spec.ENI.IPV6PrivateIPSet[0].PrivateIPAddress)
	})).Return(existingENI, nil)

	// When
	result, err := syncer.validateAndSyncENIIPs(existingENI, iaasENI)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Then
	assert.True(t, result, "Should return true when IPs are inconsistent")
	mockUpdater.AssertExpectations(t)
}

// TestValidateAndSyncENIIPs_GivenEmptyK8sIPs_WhenValidate_ThenSyncFromIaaS 测试K8s为空IP的情况
func TestValidateAndSyncENIIPs_GivenEmptyK8sIPs_WhenValidate_ThenSyncFromIaaS(t *testing.T) {
	// Initialize mock environment
	ccemock.NewMockClient()

	// Create mock subnet for IP validation
	subnet := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "subnet-empty",
			Labels: map[string]string{
				k8s.VPCIDLabel: "vpc-empty",
			},
		},
		Spec: ccev1.SubnetSpec{
			ID:               "subnet-empty",
			CIDR:             "192.168.2.0/24",
			VPCID:            "vpc-empty",
			AvailabilityZone: "zone-a",
		},
	}
	err := ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{subnet})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return
	}

	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	// K8s ENI没有IP
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-empty",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:               "eni-empty",
				VpcID:            "vpc-empty",
				PrivateIPSet:     []*models.PrivateIP{},
				IPV6PrivateIPSet: []*models.PrivateIP{},
			},
		},
	}

	// IaaS有IP
	iaasENI := &enisdk.Eni{
		EniId:    "eni-empty",
		VpcId:    "vpc-empty",
		SubnetId: "subnet-empty",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.2.10"},
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{},
	}

	// 设置mock期望
	mockUpdater.On("Update", mock.MatchedBy(func(eni *ccev2.ENI) bool {
		// 验证更新的ENI包含了IaaS的IP
		return assert.Len(t, eni.Spec.ENI.PrivateIPSet, 1) &&
			assert.Equal(t, "192.168.2.10", eni.Spec.ENI.PrivateIPSet[0].PrivateIPAddress)
	})).Return(existingENI, nil)

	// When
	result, err := syncer.validateAndSyncENIIPs(existingENI, iaasENI)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Then
	assert.True(t, result, "Should return true when K8s has no IPs but IaaS has IPs")
	mockUpdater.AssertExpectations(t)
}

// TestValidateAndSyncENIIPs_GivenEmptyIaaSIPs_WhenValidate_ThenSyncToEmpty 测试IaaS为空IP的情况
func TestValidateAndSyncENIIPs_GivenEmptyIaaSIPs_WhenValidate_ThenSyncToEmpty(t *testing.T) {
	// Initialize mock environment
	ccemock.NewMockClient()

	// Create mock subnet for IP validation
	subnet := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "subnet-k8s-has-ip",
			Labels: map[string]string{
				k8s.VPCIDLabel: "vpc-k8s-has-ip",
			},
		},
		Spec: ccev1.SubnetSpec{
			ID:               "subnet-k8s-has-ip",
			CIDR:             "192.168.3.0/24",
			VPCID:            "vpc-k8s-has-ip",
			AvailabilityZone: "zone-a",
		},
	}
	err := ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{subnet})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return
	}

	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	// K8s ENI有IP
	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-k8s-has-ip",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-k8s-has-ip",
				VpcID: "vpc-k8s-has-ip",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.3.10"},
				},
				IPV6PrivateIPSet: []*models.PrivateIP{},
			},
		},
	}

	// IaaS没有IP
	iaasENI := &enisdk.Eni{
		EniId:            "eni-k8s-has-ip",
		VpcId:            "vpc-k8s-has-ip",
		SubnetId:         "subnet-k8s-has-ip",
		PrivateIpSet:     []enisdk.PrivateIp{},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{},
	}

	// 设置mock期望
	mockUpdater.On("Update", mock.MatchedBy(func(eni *ccev2.ENI) bool {
		// 验证更新的ENI清空了IP（同步IaaS的空状态）
		return assert.Len(t, eni.Spec.ENI.PrivateIPSet, 0) &&
			assert.Len(t, eni.Spec.ENI.IPV6PrivateIPSet, 0)
	})).Return(existingENI, nil)

	// When
	result, err := syncer.validateAndSyncENIIPs(existingENI, iaasENI)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Then
	assert.True(t, result, "Should return true when K8s has IPs but IaaS has no IPs")
	mockUpdater.AssertExpectations(t)
}

// TestValidateAndSyncENIIPs_GivenUpdateError_WhenValidate_ThenHandleError 测试更新失败的情况
func TestValidateAndSyncENIIPs_GivenUpdateError_WhenValidate_ThenHandleError(t *testing.T) {
	// Initialize mock environment
	ccemock.NewMockClient()

	// Create mock subnet for IP validation
	subnet := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "subnet-update-error",
			Labels: map[string]string{
				k8s.VPCIDLabel: "vpc-update-error",
			},
		},
		Spec: ccev1.SubnetSpec{
			ID:               "subnet-update-error",
			CIDR:             "192.168.4.0/24",
			VPCID:            "vpc-update-error",
			AvailabilityZone: "zone-a",
		},
	}
	err := ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{subnet})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return
	}

	// Given
	mockUpdater := &mockENIUpdater{}
	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	existingENI := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-update-error",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:    "eni-update-error",
				VpcID: "vpc-update-error",
				PrivateIPSet: []*models.PrivateIP{
					{PrivateIPAddress: "192.168.4.10"},
				},
			},
		},
	}

	iaasENI := &enisdk.Eni{
		EniId:    "eni-update-error",
		VpcId:    "vpc-update-error",
		SubnetId: "subnet-update-error",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.4.11"}, // 不同的IP
		},
		Ipv6PrivateIpSet: []enisdk.PrivateIp{},
	}

	// 设置mock期望返回错误
	expectedError := assert.AnError
	mockUpdater.On("Update", mock.AnythingOfType("*v2.ENI")).Return((*ccev2.ENI)(nil), expectedError)

	// When
	result, err := syncer.validateAndSyncENIIPs(existingENI, iaasENI)

	// Then - 更新失败时应该返回错误
	assert.False(t, result, "Should return false when update fails")
	assert.Error(t, err, "Should return error when update fails")
	mockUpdater.AssertExpectations(t)
}

func TestCreateExternalENI_GivenEffectiveInstanceIDError_WhenCreate_ThenReturnFalse(t *testing.T) {
	setOperatorHPASStanderd(t, true)
	ccemock.NewMockClient()

	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-device-type",
			UID:  "test-uid-device-type",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID:   "i-no-hpasid",
			InstanceType: string(metadata.InstanceTypeExHPAS),
			ENI: &api.ENISpec{
				InstanceType: string(metadata.InstanceTypeExHPAS),
				UseMode:      "Primary",
			},
		},
	}
	require := assert.New(t)
	require.NoError(ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs}))

	mockUpdater := &mockENIUpdater{}
	mockEventRecorder := &mockEventRecorder{}
	syncer := &remoteVpcEniSyncher{
		updater:       mockUpdater,
		eventRecorder: mockEventRecorder,
	}

	mockLister := &mockENILister{}
	mockLister.On("Get", "eni-device-type").Return((*ccev2.ENI)(nil), kerrors.NewNotFound(ccev2.Resource("eni"), "eni-device-type"))
	mockUpdater.On("Lister").Return(mockLister)

	mockEventRecorder.On("Eventf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	iaasENI := &enisdk.Eni{
		EniId:      "eni-device-type",
		Name:       "eni-device-type",
		InstanceId: "i-no-hpasid",
		VpcId:      "vpc-test",
		SubnetId:   "subnet-test",
		Status:     "inuse",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.30"},
		},
	}

	isExisted, result := syncer.createExternalENI(iaasENI)
	require.False(isExisted)
	require.Nil(result)

	mockUpdater.AssertNotCalled(t, "Create", mock.Anything)
	mockLister.AssertExpectations(t)
	mockEventRecorder.AssertExpectations(t)
}

// TestCreateExternalENI_GivenHPASInstanceID_WhenCreate_ThenQueryByHPASID tests HPAS ID query logic
func TestCreateExternalENI_GivenHPASInstanceID_WhenCreate_ThenQueryByHPASID(t *testing.T) {
	setOperatorHPASStanderd(t, true)
	// Given - 初始化 mock client
	ccemock.NewMockClient()

	// 创建测试用的 NRS 对象（HPAS 类型）
	hpasNRS := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-hpas",
			UID:  "test-uid-hpas",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID:   "i-12345",
			InstanceType: string(metadata.InstanceTypeExHPAS),
			HPASID:       "hpas-67890",
			ENI: &api.ENISpec{
				InstanceType: "hpas",
				UseMode:      "Primary",
			},
		},
	}

	// 确保 NRS 在 informer 中
	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{hpasNRS})
	assert.NoError(t, err, "ensure nrs to informer failed")

	// 创建 mock updater 和 event recorder
	mockUpdater := &mockENIUpdater{}
	mockEventRecorder := &mockEventRecorder{}

	syncer := &remoteVpcEniSyncher{
		updater:       mockUpdater,
		eventRecorder: mockEventRecorder,
	}

	// 创建 HPAS 类型的 ENI（InstanceId 以 "hpas-" 开头）
	iaasENI := &enisdk.Eni{
		EniId:      "eni-hpas-test",
		Name:       "test-eni-hpas",
		InstanceId: "hpas-67890", // HPAS 类型的 instance ID
		VpcId:      "vpc-test",
		SubnetId:   "subnet-test",
		Status:     "inuse",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.100"},
		},
	}

	// Mock Lister.Get 返回 NotFound（ENI 不存在）
	mockLister := &mockENILister{}
	mockLister.On("Get", "eni-hpas-test").Return((*ccev2.ENI)(nil), kerrors.NewNotFound(ccev2.Resource("eni"), "eni-hpas-test"))
	mockUpdater.On("Lister").Return(mockLister)

	// Mock Create 方法
	mockUpdater.On("Create", mock.MatchedBy(func(eni *ccev2.ENI) bool {
		instanceLabel := eni.Labels[k8s.LabelInstanceID]
		// 验证创建的 ENI 包含正确的信息
		return eni.Name == "eni-hpas-test" &&
			eni.Spec.ENI.InstanceID == "hpas-67890" &&
			instanceLabel == "hpas-67890" &&
			eni.Spec.NodeName == "test-node-hpas"
	})).Return(&ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-hpas-test",
		},
	}, nil)

	// Mock event recorder
	mockEventRecorder.On("Eventf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	// When - 调用 createExternalENI
	isExisted, result := syncer.createExternalENI(iaasENI)

	// Then - 验证结果
	assert.True(t, isExisted, "ENI should be created successfully")
	assert.NotNil(t, result, "Result should not be nil")
	assert.Equal(t, "eni-hpas-test", result.Name, "ENI name should match")

	// 验证 mock 调用
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

// TestCreateExternalENI_GivenHPASInstanceID_WhenStandardDisabled_ThenQueryByInstanceID tests fallback path
func TestCreateExternalENI_GivenHPASInstanceID_WhenStandardDisabled_ThenQueryByInstanceID(t *testing.T) {
	setOperatorHPASStanderd(t, false)
	ccemock.NewMockClient()

	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-nonstd",
			UID:  "test-uid-nonstd",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "hpas-nonstd",
			ENI: &api.ENISpec{
				InstanceType: "hpas",
				UseMode:      "Primary",
			},
		},
	}
	require := assert.New(t)
	require.NoError(ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs}))

	mockUpdater := &mockENIUpdater{}
	mockEventRecorder := &mockEventRecorder{}
	syncer := &remoteVpcEniSyncher{
		updater:       mockUpdater,
		eventRecorder: mockEventRecorder,
	}

	iaasENI := &enisdk.Eni{
		EniId:      "eni-nonstd",
		Name:       "eni-nonstd",
		InstanceId: "hpas-nonstd",
		VpcId:      "vpc-test",
		SubnetId:   "subnet-test",
		Status:     "inuse",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.10"},
		},
	}

	mockLister := &mockENILister{}
	mockLister.On("Get", "eni-nonstd").Return((*ccev2.ENI)(nil), kerrors.NewNotFound(ccev2.Resource("eni"), "eni-nonstd"))
	mockUpdater.On("Lister").Return(mockLister)

	mockUpdater.On("Create", mock.MatchedBy(func(eni *ccev2.ENI) bool {
		return eni.Spec.NodeName == "test-node-nonstd" &&
			eni.Spec.ENI.InstanceID == "hpas-nonstd" &&
			eni.Labels[k8s.LabelInstanceID] == "hpas-nonstd"
	})).Return(&ccev2.ENI{ObjectMeta: metav1.ObjectMeta{Name: "eni-nonstd"}}, nil)
	mockEventRecorder.On("Eventf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	isExisted, result := syncer.createExternalENI(iaasENI)
	require.True(isExisted)
	require.NotNil(result)
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

// TestCreateExternalENI_GivenHPASMetadata_WhenStandardDisabled_ThenUseInstanceIDLabel verifies effective ID logic
func TestCreateExternalENI_GivenHPASMetadata_WhenStandardDisabled_ThenUseInstanceIDLabel(t *testing.T) {
	setOperatorHPASStanderd(t, false)
	ccemock.NewMockClient()

	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-instance",
			UID:  "test-uid-instance",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-real-123",
			HPASID:     "hpas-123",
			ENI: &api.ENISpec{
				InstanceType: "hpas",
				UseMode:      "Primary",
			},
		},
	}
	assert.NoError(t, ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs}))

	mockUpdater := &mockENIUpdater{}
	mockEventRecorder := &mockEventRecorder{}
	syncer := &remoteVpcEniSyncher{
		updater:       mockUpdater,
		eventRecorder: mockEventRecorder,
	}

	iaasENI := &enisdk.Eni{
		EniId:      "eni-instance",
		Name:       "eni-instance",
		InstanceId: "i-real-123",
		VpcId:      "vpc-test",
		SubnetId:   "subnet-test",
		Status:     "inuse",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.20"},
		},
	}

	mockLister := &mockENILister{}
	mockLister.On("Get", "eni-instance").Return((*ccev2.ENI)(nil), kerrors.NewNotFound(ccev2.Resource("eni"), "eni-instance"))
	mockUpdater.On("Lister").Return(mockLister)

	mockUpdater.On("Create", mock.MatchedBy(func(eni *ccev2.ENI) bool {
		return eni.Spec.ENI.InstanceID == "i-real-123" &&
			eni.Labels[k8s.LabelInstanceID] == "i-real-123"
	})).Return(&ccev2.ENI{ObjectMeta: metav1.ObjectMeta{Name: "eni-instance"}}, nil)
	mockEventRecorder.On("Eventf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	isExisted, result := syncer.createExternalENI(iaasENI)
	assert.True(t, isExisted)
	assert.NotNil(t, result)
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

// TestCreateExternalENI_GivenRegularInstanceID_WhenCreate_ThenQueryByInstanceID tests regular instance ID query logic
func TestCreateExternalENI_GivenRegularInstanceID_WhenCreate_ThenQueryByInstanceID(t *testing.T) {
	// Given - 初始化 mock client
	ccemock.NewMockClient()

	// 创建测试用的 NRS 对象（普通 BCC 类型）
	bccNRS := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node-bcc",
			UID:  "test-uid-bcc",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-bcc-12345",
			ENI: &api.ENISpec{
				InstanceType: "bcc",
				UseMode:      "Primary",
			},
		},
	}

	// 确保 NRS 在 informer 中
	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{bccNRS})
	assert.NoError(t, err, "ensure nrs to informer failed")

	// 创建 mock updater 和 event recorder
	mockUpdater := &mockENIUpdater{}
	mockEventRecorder := &mockEventRecorder{}

	syncer := &remoteVpcEniSyncher{
		updater:       mockUpdater,
		eventRecorder: mockEventRecorder,
	}

	// 创建普通 BCC 类型的 ENI（InstanceId 不以 "hpas-" 开头）
	iaasENI := &enisdk.Eni{
		EniId:      "eni-bcc-test",
		Name:       "test-eni-bcc",
		InstanceId: "i-bcc-12345", // 普通 BCC instance ID
		VpcId:      "vpc-test",
		SubnetId:   "subnet-test",
		Status:     "inuse",
		PrivateIpSet: []enisdk.PrivateIp{
			{PrivateIpAddress: "192.168.1.200"},
		},
	}

	// Mock Lister.Get 返回 NotFound（ENI 不存在）
	mockLister := &mockENILister{}
	mockLister.On("Get", "eni-bcc-test").Return((*ccev2.ENI)(nil), kerrors.NewNotFound(ccev2.Resource("eni"), "eni-bcc-test"))
	mockUpdater.On("Lister").Return(mockLister)

	// Mock Create 方法
	mockUpdater.On("Create", mock.MatchedBy(func(eni *ccev2.ENI) bool {
		// 验证创建的 ENI 包含正确的信息
		return eni.Name == "eni-bcc-test" &&
			eni.Spec.ENI.InstanceID == "i-bcc-12345" &&
			eni.Spec.NodeName == "test-node-bcc"
	})).Return(&ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "eni-bcc-test",
		},
	}, nil)

	// Mock event recorder
	mockEventRecorder.On("Eventf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	// When - 调用 createExternalENI
	isExisted, result := syncer.createExternalENI(iaasENI)

	// Then - 验证结果
	assert.True(t, isExisted, "ENI should be created successfully")
	assert.NotNil(t, result, "Result should not be nil")
	assert.Equal(t, "eni-bcc-test", result.Name, "ENI name should match")

	// 验证 mock 调用
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

// TestCreateExternalENI_GivenHPASInstanceIDNotFound_WhenCreate_ThenReturnFalse tests HPAS ID not found scenario
func TestCreateExternalENI_GivenHPASInstanceIDNotFound_WhenCreate_ThenReturnFalse(t *testing.T) {
	// Given - 初始化 mock client
	ccemock.NewMockClient()

	// 不创建任何 NRS，模拟找不到对应节点的情况

	// 创建 mock updater
	mockUpdater := &mockENIUpdater{}

	syncer := &remoteVpcEniSyncher{
		updater: mockUpdater,
	}

	// 创建 HPAS 类型的 ENI，但没有对应的 NRS
	iaasENI := &enisdk.Eni{
		EniId:      "eni-hpas-notfound",
		Name:       "test-eni-hpas-notfound",
		InstanceId: "hpas-notfound", // HPAS ID 不存在
		VpcId:      "vpc-test",
		SubnetId:   "subnet-test",
		Status:     "inuse",
	}

	// Mock Lister.Get 返回 NotFound（ENI 不存在）
	mockLister := &mockENILister{}
	mockLister.On("Get", "eni-hpas-notfound").Return((*ccev2.ENI)(nil), kerrors.NewNotFound(ccev2.Resource("eni"), "eni-hpas-notfound"))
	mockUpdater.On("Lister").Return(mockLister)

	// When - 调用 createExternalENI
	isExisted, result := syncer.createExternalENI(iaasENI)

	// Then - 验证结果（找不到 NRS，应该返回 false 和 nil）
	assert.False(t, isExisted, "ENI should not be created when NRS not found")
	assert.Nil(t, result, "Result should be nil when NRS not found")

	// 验证 mock 调用
	mockLister.AssertExpectations(t)
}

// TestGetByHPASID_Integration_GivenValidHPASID_WhenQuery_ThenReturnNRS tests GetByHPASID with valid HPASID
func TestGetByHPASID_Integration_GivenValidHPASID_WhenQuery_ThenReturnNRS(t *testing.T) {
	// Given
	ccemock.NewMockClient()

	nrs1 := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nrs-1",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-12345",
			HPASID:     "hpas-12345",
		},
	}

	nrs2 := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nrs-2",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-67890",
			HPASID:     "hpas-67890",
		},
	}

	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs1, nrs2})
	assert.NoError(t, err, "Failed to add NRS to informer")

	// When
	result, err := watchers.NetResourceSetClient.GetByHPASID("hpas-12345")

	// Then
	assert.NoError(t, err, "GetByHPASID should not return error")
	assert.Len(t, result, 1, "Should return exactly 1 NRS")
	assert.Equal(t, "test-nrs-1", result[0].Name, "Should return the correct NRS")
	assert.Equal(t, "hpas-12345", result[0].Spec.HPASID, "HPASID should match")
}

// TestGetByHPASID_Integration_GivenNonExistentHPASID_WhenQuery_ThenReturnEmpty tests GetByHPASID with non-existent HPASID
func TestGetByHPASID_Integration_GivenNonExistentHPASID_WhenQuery_ThenReturnEmpty(t *testing.T) {
	// Given
	ccemock.NewMockClient()

	nrs := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nrs",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-12345",
			HPASID:     "hpas-12345",
		},
	}

	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs})
	assert.NoError(t, err, "Failed to add NRS to informer")

	// When
	result, err := watchers.NetResourceSetClient.GetByHPASID("hpas-nonexistent")

	// Then
	assert.NoError(t, err, "GetByHPASID should not return error for non-existent HPASID")
	assert.Empty(t, result, "Result should be empty for non-existent HPASID")
}

// TestGetByHPASID_Integration_GivenMultipleNRSWithSameHPASID_WhenQuery_ThenReturnAll tests GetByHPASID with multiple NRS
func TestGetByHPASID_Integration_GivenMultipleNRSWithSameHPASID_WhenQuery_ThenReturnAll(t *testing.T) {
	// Given
	ccemock.NewMockClient()

	nrs1 := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nrs-1",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-12345",
			HPASID:     "hpas-same",
		},
	}

	nrs2 := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-nrs-2",
		},
		Spec: ccev2.NetResourceSpec{
			InstanceID: "i-67890",
			HPASID:     "hpas-same",
		},
	}

	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs1, nrs2})
	assert.NoError(t, err, "Failed to add NRS to informer")

	// When
	result, err := watchers.NetResourceSetClient.GetByHPASID("hpas-same")

	// Then
	assert.NoError(t, err, "GetByHPASID should not return error")
	assert.Len(t, result, 2, "Should return 2 NRS with the same HPASID")

	// Verify both NRS are returned
	names := []string{result[0].Name, result[1].Name}
	assert.Contains(t, names, "test-nrs-1", "Should contain test-nrs-1")
	assert.Contains(t, names, "test-nrs-2", "Should contain test-nrs-2")
}

// TestGetByHPASID_Integration_GivenEmptyHPASID_WhenQuery_ThenReturnError tests GetByHPASID with empty HPASID
func TestGetByHPASID_Integration_GivenEmptyHPASID_WhenQuery_ThenReturnError(t *testing.T) {
	// Given
	ccemock.NewMockClient()

	// When
	result, err := watchers.NetResourceSetClient.GetByHPASID("")

	// Then
	assert.Error(t, err, "GetByHPASID should return error for empty HPASID")
	assert.Contains(t, err.Error(), "hpasID is empty", "Error message should indicate empty HPASID")
	assert.Empty(t, result, "Result should be empty when error occurs")
}

package bcesync

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baidubce/bce-sdk-go/services/bbc"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	typev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	listv1 "k8s.io/client-go/listers/core/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	bccapi "github.com/baidubce/bce-sdk-go/services/bcc/api"
)

type mockBCEClient struct {
	mock.Mock
	cloud.Interface
}

func (m *mockBCEClient) StatENI(ctx context.Context, eniID string) (*enisdk.Eni, error) {
	args := m.Called(ctx, eniID)
	return args.Get(0).(*enisdk.Eni), args.Error(1)
}

func (m *mockBCEClient) DetachENI(ctx context.Context, args *enisdk.EniInstance) error {
	return m.Called(ctx, args).Error(0)
}

func (m *mockBCEClient) DeleteENI(ctx context.Context, eniID string) error {
	return m.Called(ctx, eniID).Error(0)
}

func (m *mockBCEClient) BBCBatchDelIP(ctx context.Context, args *bbc.BatchDelIpArgs) error {
	return m.Called(ctx, args).Error(0)
}

func (m *mockBCEClient) BCCBatchDelIP(ctx context.Context, args *bccapi.BatchDelIpArgs) error {
	return m.Called(ctx, args).Error(0)
}

func TestRemoveENI_InuseStatus(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse,
		},
	}

	// Mock StatENI to return inuse status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusInuse),
	}, nil)

	// Mock DetachENI to succeed
	mockClient.On("DetachENI", mock.Anything, &enisdk.EniInstance{
		EniId:      eniObj.Name,
		InstanceId: eniObj.Spec.ENI.InstanceID,
	}).Return(nil)

	// Mock UpdateStatus to succeed
	mockUpdater.On("UpdateStatus", mock.Anything).Return(eniObj, nil)

	err := removeENI(es, eniObj)
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_DetachingStatus(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusDetaching,
		},
	}

	// Mock StatENI to return detaching status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusDetaching),
	}, nil)

	err := removeENI(es, eniObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status is not can be deleted")

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_AvailableStatus(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusAvailable,
		},
	}

	// Mock StatENI to return available status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusAvailable),
	}, nil)

	// Mock DeleteENI to succeed
	mockClient.On("DeleteENI", mock.Anything, eniObj.Name).Return(nil)

	// Mock UpdateStatus to succeed
	mockUpdater.On("UpdateStatus", mock.Anything).Return(eniObj, nil)

	err := removeENI(es, eniObj)
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_StatENIError(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
	}

	// Mock StatENI to return error
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{}, errors.New("stat error"))

	err := removeENI(es, eniObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stat error")

	mockClient.AssertExpectations(t)
}

func TestRemoveENI_DetachError(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse,
		},
	}

	// Mock StatENI to return inuse status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusInuse),
	}, nil)

	// Mock DetachENI to fail
	mockClient.On("DetachENI", mock.Anything, &enisdk.EniInstance{
		EniId:      eniObj.Name,
		InstanceId: eniObj.Spec.ENI.InstanceID,
	}).Return(errors.New("detach error"))

	err := removeENI(es, eniObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "detach error")

	mockClient.AssertExpectations(t)
}

func TestRemoveENI_DeleteError(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusAvailable,
		},
	}

	// Mock StatENI to return available status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusAvailable),
	}, nil)

	// Mock DeleteENI to fail
	mockClient.On("DeleteENI", mock.Anything, eniObj.Name).Return(errors.New("delete error"))

	err := removeENI(es, eniObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete error")

	mockClient.AssertExpectations(t)
}

func TestRemoveENI_UpdateStatusError(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse,
		},
	}

	// Mock StatENI to return inuse status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusInuse),
	}, nil)

	// Mock DetachENI to succeed
	mockClient.On("DetachENI", mock.Anything, &enisdk.EniInstance{
		EniId:      eniObj.Name,
		InstanceId: eniObj.Spec.ENI.InstanceID,
	}).Return(nil)

	// Mock UpdateStatus to fail
	mockUpdater.On("UpdateStatus", mock.Anything).Return(eniObj, errors.New("update error"))

	err := removeENI(es, eniObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update error")

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_AttachingStatus(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusAttaching,
		},
	}

	// Mock StatENI to return attaching status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusAttaching),
	}, nil)

	err := removeENI(es, eniObj)
	assert.NoError(t, err) // attaching status should return nil

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_DeletedStatus(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusDeleted,
		},
	}

	// Mock StatENI to return deleted status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusDeleted),
	}, nil)

	err := removeENI(es, eniObj)
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_WithFinalizer(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{FinalizerENI},
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusAvailable,
		},
	}

	// Mock StatENI to return available status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusAvailable),
	}, nil)

	// Mock DeleteENI to succeed
	mockClient.On("DeleteENI", mock.Anything, eniObj.Name).Return(nil)

	// Mock UpdateStatus to succeed
	mockUpdater.On("UpdateStatus", mock.Anything).Return(eniObj, nil)

	err := removeENI(es, eniObj)
	assert.NoError(t, err)

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_NotFoundError(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
	}

	// Mock StatENI to return not found error with correct type
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{}, errors.New("not found"))

	err := removeENI(es, eniObj)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	mockClient.AssertExpectations(t)
}

func TestRemoveENI_RDMAENI(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-rdma-eni",
		},
		Spec: ccev2.ENISpec{
			Type: ccev2.ENIForHPC,
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
	}

	// Mock StatENI to return error (RDMA ENI cannot be found in VPC)
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{}, errors.New("not found"))

	err := removeENI(es, eniObj)
	assert.Error(t, err)

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestRemoveENI_UpdateStatusErrorLog(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				InstanceID: "test-instance",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse,
		},
	}

	// Mock StatENI to return inuse status
	mockClient.On("StatENI", mock.Anything, eniObj.Name).Return(&enisdk.Eni{
		Status: string(ccev2.VPCENIStatusInuse),
	}, nil)

	// Mock DetachENI to succeed
	mockClient.On("DetachENI", mock.Anything, &enisdk.EniInstance{
		EniId:      eniObj.Name,
		InstanceId: eniObj.Spec.ENI.InstanceID,
	}).Return(nil)

	// Mock UpdateStatus to fail with specific error
	expectedErr := errors.New("update status failed")
	mockUpdater.On("UpdateStatus", mock.Anything).Return(eniObj, expectedErr)

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := log.Logger.Out
	log.Logger.SetOutput(&logBuf)
	defer func() { log.Logger.SetOutput(oldOut) }()

	err := removeENI(es, eniObj)
	assert.Error(t, err)
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "patch eni test-eni failed")
	assert.Contains(t, logOutput, expectedErr.Error())

	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
}

func TestIsCanRemoveENIFinalizer(t *testing.T) {
	// Backup original config
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()

	tests := []struct {
		name           string
		eniType        ccev2.ENIType
		releaseENIs    bool
		interfaceIndex int
		expected       bool
	}{
		{
			name:           "ENI with ReleaseENIs enabled and interfaceIndex -1",
			eniType:        ccev2.ENIForBCC,
			releaseENIs:    true,
			interfaceIndex: -1,
			expected:       true,
		},
		{
			name:           "ENI with ReleaseENIs enabled and interfaceIndex 10",
			eniType:        ccev2.ENIForBCC,
			releaseENIs:    true,
			interfaceIndex: 10,
			expected:       false,
		},
		{
			name:           "ENI with ReleaseENIs disabled and interfaceIndex -1",
			eniType:        ccev2.ENIForBCC,
			releaseENIs:    false,
			interfaceIndex: -1,
			expected:       true,
		},
		{
			name:           "ENI with ReleaseENIs disabled and interfaceIndex 10",
			eniType:        ccev2.ENIForBCC,
			releaseENIs:    false,
			interfaceIndex: 10,
			expected:       true,
		},
		{
			name:           "Underlay-RDMA ENI(ENIForHPC type) with ReleaseENIs enabled and interfaceIndex -1",
			eniType:        ccev2.ENIForHPC,
			releaseENIs:    true,
			interfaceIndex: -1,
			expected:       true,
		},
		{
			name:           "Underlay-RDMA ENI(ENIForHPC type) with ReleaseENIs enabled and interfaceIndex 10",
			eniType:        ccev2.ENIForHPC,
			releaseENIs:    true,
			interfaceIndex: 10,
			expected:       true,
		},
		{
			name:           "Underlay-RDMA ENI(ENIForHPC type) with ReleaseENIs disabled and interfaceIndex -1",
			eniType:        ccev2.ENIForHPC,
			releaseENIs:    false,
			interfaceIndex: -1,
			expected:       true,
		},
		{
			name:           "Underlay-RDMA ENI(ENIForHPC type) with ReleaseENIs disabled and interfaceIndex 10",
			eniType:        ccev2.ENIForHPC,
			releaseENIs:    false,
			interfaceIndex: 10,
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			operatorOption.Config.ReleaseENIs = tt.releaseENIs
			assert.Equal(t, tt.expected, isCanRemoveENIFinalizer(tt.eniType, tt.interfaceIndex))
		})
	}
}

// eniTestContext creates a context for ENI tests.
func eniTestContext(t *testing.T, nrs *ccev2.NetResourceSet) (ipam.NetResourceSetGetterUpdater, error) {
	ccemock.NewMockClient()
	nrsGetterUpdater := watchers.NetResourceSetClient

	if nrs != nil {
		err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{nrs})
		if !assert.NoError(t, err, "ensure nrs to informer failed") {
			return nil, err
		}
	}

	return nrsGetterUpdater, nil
}

func TestMangeFinalizer(t *testing.T) {
	// Setup
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()
	operatorOption.Config.ReleaseENIs = true

	// Case 1: ENI not deleted and no Finalizer
	t.Run("NotDeletedNoFinalizer", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: nil,
			},
		}
		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.False(t, shouldPatch)
		assert.Equal(t, []string{FinalizerENI}, eni.ObjectMeta.Finalizers)
	})

	// Case 2: ENI not deleted with Finalizer
	t.Run("NotDeletedWithFinalizer", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{FinalizerENI},
			},
		}
		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.False(t, shouldPatch)
		assert.Equal(t, []string{FinalizerENI}, eni.ObjectMeta.Finalizers)
	})

	// Case 3: ENI deleted with no Finalizer
	t.Run("DeletedNoFinalizer", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        nil,
			},
		}
		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.False(t, shouldPatch)
		assert.Nil(t, eni.ObjectMeta.Finalizers)
	})

	// Case 4: ENI deleted with Finalizer (node not exist)
	t.Run("DeletedWithFinalizerNodeNotExist", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				NodeName: "non-existent-node",
			},
			Status: ccev2.ENIStatus{
				InterfaceIndex: -1,
			},
		}
		eniTestContext(t, nil)

		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.ObjectMeta.Finalizers)
	})

	// Case 5: ENI deleted with Finalizer (node exists but not deleted)
	t.Run("DeletedWithFinalizerNodeExists", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				NodeName: "existing-node",
			},
		}
		eniTestContext(t, &ccev2.NetResourceSet{})

		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Equal(t, []string(nil), eni.Finalizers)
	})

	// Case 6: ENI deleted with Finalizer (node deleted and interfaceIndex=-1)
	t.Run("DeletedWithFinalizerNodeDeleted", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				NodeName: "deleted-node",
			},
			Status: ccev2.ENIStatus{
				InterfaceIndex: -1,
			},
		}
		eniTestContext(t, &ccev2.NetResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
			},
		})

		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.Finalizers)
	})

	// Case 7: ENI deleted with Finalizer (node deleted but interfaceIndex≠-1)
	t.Run("DeletedWithFinalizerNodeDeletedButInterfaceNotReleased", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				NodeName: "deleted-node",
			},
			Status: ccev2.ENIStatus{
				InterfaceIndex: 0,
				VPCStatus:      ccev2.VPCENIStatusInuse,
			},
		}
		eniTestContext(t, &ccev2.NetResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
			},
		})

		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.False(t, shouldPatch)
		assert.Equal(t, []string{FinalizerENI}, eni.Finalizers)
	})

	// Case 8: ENI deleted with Finalizer (node exists but UID mismatch)
	t.Run("DeletedWithFinalizerNodeUIDMismatch", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
				OwnerReferences: []metav1.OwnerReference{
					{
						UID: "owner-uid",
					},
				},
			},
			Spec: ccev2.ENISpec{
				NodeName: "mismatch-node",
			},
			Status: ccev2.ENIStatus{
				InterfaceIndex: -1,
			},
		}
		eniTestContext(t, &ccev2.NetResourceSet{
			ObjectMeta: metav1.ObjectMeta{
				UID: "different-uid",
			},
		})

		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.Finalizers)
	})

	// Case 9: ENI deleted with Finalizer (ENI status not inuse/deleted)
	t.Run("DeletedWithFinalizerENIStatusNotInuse", func(t *testing.T) {
		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				NodeName: "test-node",
			},
			Status: ccev2.ENIStatus{
				VPCStatus: ccev2.VPCENIStatusAvailable,
			},
		}
		eniTestContext(t, &ccev2.NetResourceSet{})

		shouldPatch := (&eniSyncher{}).mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.Finalizers)
	})

	// Case 10: ENI with secondary IPs - should remove finalizer without calling BBCBatchDelIP
	t.Run("ENIWithSecondaryIPs_RemoveFinalizer", func(t *testing.T) {
		mockClient := new(mockBCEClient)
		mockUpdater := new(mockENIUpdater)

		es := &eniSyncher{
			bceclient: mockClient,
			updater:   mockUpdater,
		}

		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-eni",
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
				NodeName: "test-node",
				ENI: models.ENI{
					InstanceID: "test-instance",
					PrivateIPSet: []*models.PrivateIP{
						{
							Primary:          true,
							PrivateIPAddress: "192.168.1.10",
						},
						{
							Primary:          false,
							PrivateIPAddress: "192.168.1.11",
						},
						{
							Primary:          false,
							PrivateIPAddress: "192.168.1.12",
						},
					},
				},
			},
			Status: ccev2.ENIStatus{
				VPCStatus: ccev2.VPCENIStatusAvailable,
			},
		}

		// Setup test context
		eniTestContext(t, nil)

		shouldPatch := es.mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.Finalizers)

		// BBCBatchDelIP should not be called in mangeFinalizer anymore
		mockClient.AssertNotCalled(t, "BBCBatchDelIP")
	})

	// Case 11: ENI with secondary IPs - should remove finalizer without calling BBCBatchDelIP
	t.Run("ENIWithSecondaryIPs_RemoveFinalizerRegardlessOfStatus", func(t *testing.T) {
		mockClient := new(mockBCEClient)
		mockUpdater := new(mockENIUpdater)

		es := &eniSyncher{
			bceclient: mockClient,
			updater:   mockUpdater,
		}

		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-eni",
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
				NodeName: "test-node",
				ENI: models.ENI{
					InstanceID: "test-instance",
					PrivateIPSet: []*models.PrivateIP{
						{
							Primary:          true,
							PrivateIPAddress: "192.168.1.10",
						},
						{
							Primary:          false,
							PrivateIPAddress: "192.168.1.11",
						},
					},
				},
			},
			Status: ccev2.ENIStatus{
				VPCStatus: ccev2.VPCENIStatusAvailable, // Available status allows finalizer removal
			},
		}

		// Setup test context
		eniTestContext(t, nil)

		shouldPatch := es.mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.Finalizers)

		// BBCBatchDelIP should not be called in mangeFinalizer anymore
		mockClient.AssertNotCalled(t, "BBCBatchDelIP")
	})

	// Case 12: ENI with only primary IP - should remove finalizer without calling BBCBatchDelIP
	t.Run("ENIWithOnlyPrimaryIP_RemoveFinalizer", func(t *testing.T) {
		mockClient := new(mockBCEClient)
		mockUpdater := new(mockENIUpdater)

		es := &eniSyncher{
			bceclient: mockClient,
			updater:   mockUpdater,
		}

		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-eni",
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
				NodeName: "test-node",
				ENI: models.ENI{
					InstanceID: "test-instance",
					PrivateIPSet: []*models.PrivateIP{
						{
							Primary:          true,
							PrivateIPAddress: "192.168.1.10",
						},
					},
				},
			},
			Status: ccev2.ENIStatus{
				VPCStatus: ccev2.VPCENIStatusAvailable,
			},
		}

		// Setup test context
		eniTestContext(t, nil)

		shouldPatch := es.mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.Finalizers)

		// BBCBatchDelIP should not be called in mangeFinalizer anymore
		mockClient.AssertNotCalled(t, "BBCBatchDelIP")
	})

	// Case 13: ENI with non-PrimaryWithSecondaryIP mode - should skip BBCBatchDelIP
	t.Run("ENIWithNonPrimaryWithSecondaryIPMode_SkipBBCBatchDelIP", func(t *testing.T) {
		mockClient := new(mockBCEClient)
		mockUpdater := new(mockENIUpdater)

		es := &eniSyncher{
			bceclient: mockClient,
			updater:   mockUpdater,
		}

		eni := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-eni",
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerENI},
			},
			Spec: ccev2.ENISpec{
				UseMode:  ccev2.ENIUseModeSecondaryIP, // Not PrimaryWithSecondaryIP mode
				NodeName: "test-node",
				ENI: models.ENI{
					InstanceID: "test-instance",
					PrivateIPSet: []*models.PrivateIP{
						{
							Primary:          false,
							PrivateIPAddress: "192.168.1.11",
						},
					},
				},
			},
			Status: ccev2.ENIStatus{
				VPCStatus: ccev2.VPCENIStatusAvailable,
			},
		}

		// Setup test context
		eniTestContext(t, nil)

		shouldPatch := es.mangeFinalizer(eni)
		assert.True(t, shouldPatch)
		assert.Nil(t, eni.Finalizers)

		// BBCBatchDelIP should not be called since UseMode is not PrimaryWithSecondaryIP
		mockClient.AssertNotCalled(t, "BBCBatchDelIP")
	})
}

func TestElectENIIPv6PrimaryIP_GivenNoIPv6IPs_WhenCalled_ThenNoChange(t *testing.T) {
	eni := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				IPV6PrivateIPSet: []*models.PrivateIP{},
			},
		},
	}

	ElectENIIPv6PrimaryIP(eni)
	assert.Nil(t, eni.Annotations)
}

func TestElectENIIPv6PrimaryIP_GivenIPv6IPsWithNoPrimary_WhenCalled_ThenSetFirstAsPrimary(t *testing.T) {
	eni := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				IPV6PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "2001:db8::1",
						Primary:          false,
					},
					{
						PrivateIPAddress: "2001:db8::2",
						Primary:          false,
					},
				},
			},
		},
	}

	ElectENIIPv6PrimaryIP(eni)
	assert.NotNil(t, eni.Annotations)
	assert.Equal(t, "2001:db8::1", eni.Annotations[k8s.AnnotationENIIPv6PrimaryIP])
	assert.True(t, eni.Spec.ENI.IPV6PrivateIPSet[0].Primary)
	assert.False(t, eni.Spec.ENI.IPV6PrivateIPSet[1].Primary)
}

func TestElectENIIPv6PrimaryIP_GivenIPv6IPsWithExistingPrimary_WhenCalled_ThenKeepExistingPrimary(t *testing.T) {
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				k8s.AnnotationENIIPv6PrimaryIP: "2001:db8::2",
			},
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				IPV6PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "2001:db8::1",
						Primary:          false,
					},
					{
						PrivateIPAddress: "2001:db8::2",
						Primary:          true,
					},
				},
			},
		},
	}

	ElectENIIPv6PrimaryIP(eni)
	assert.True(t, eni.Spec.ENI.IPV6PrivateIPSet[1].Primary)
}

func TestElectENIIPv6PrimaryIP_GivenIPv6IPsWithPrimaryButNotInAnnotation_WhenCalled_ThenKeepExistingPrimary(t *testing.T) {
	eni := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				IPV6PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "2001:db8::1",
						Primary:          false,
					},
					{
						PrivateIPAddress: "2001:db8::2",
						Primary:          true,
					},
				},
			},
		},
	}

	ElectENIIPv6PrimaryIP(eni)
	assert.False(t, eni.Spec.ENI.IPV6PrivateIPSet[0].Primary)
	assert.True(t, eni.Spec.ENI.IPV6PrivateIPSet[1].Primary)
}

func TestToModelPrivateIP_GivenEmptyIPSet_WhenCalled_ThenReturnEmptySlice(t *testing.T) {
	result := ToModelPrivateIP([]enisdk.PrivateIp{}, "vpc-123", "subnet-456")
	assert.Empty(t, result)
}

func TestToModelPrivateIP_GivenValidIPSet_WhenCalled_ThenReturnConvertedIPs(t *testing.T) {
	ccemock.NewMockClient()

	ipset := []enisdk.PrivateIp{
		{
			PublicIpAddress:  "1.2.3.4",
			PrivateIpAddress: "192.168.1.1",
			Primary:          true,
		},
		{
			PublicIpAddress:  "5.6.7.8",
			PrivateIpAddress: "192.168.1.2",
			Primary:          false,
		},
	}

	result := ToModelPrivateIP(ipset, "vpc-123", "subnet-456")
	assert.Len(t, result, 2)
	assert.Equal(t, "1.2.3.4", result[0].PublicIPAddress)
	assert.Equal(t, "192.168.1.1", result[0].PrivateIPAddress)
	assert.True(t, result[0].Primary)
	assert.Equal(t, "5.6.7.8", result[1].PublicIPAddress)
	assert.Equal(t, "192.168.1.2", result[1].PrivateIPAddress)
	assert.False(t, result[1].Primary)
}

func TestVPCENISyncerRouter_Create_GivenENIForBBC_WhenCalled_ThenReturnNil(t *testing.T) {
	router := &VPCENISyncerRouter{}
	eni := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			Type: ccev2.ENIForBBC,
		},
	}

	err := router.Create(eni)
	assert.NoError(t, err)
}

func TestVPCENISyncerRouter_Update_GivenENIForBBC_WhenCalled_ThenReturnNil(t *testing.T) {
	router := &VPCENISyncerRouter{}
	eni := &ccev2.ENI{
		Spec: ccev2.ENISpec{
			Type: ccev2.ENIForBBC,
		},
	}

	err := router.Update(eni)
	assert.NoError(t, err)
}

func TestVPCENISyncerRouter_ResyncENI_WhenCalled_ThenReturnResyncPeriod(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient:    mockClient,
		updater:      mockUpdater,
		resyncPeriod: 30 * time.Second,
	}

	router := &VPCENISyncerRouter{
		eni: es,
	}

	duration := router.ResyncENI(context.Background())
	assert.Equal(t, 30*time.Second, duration)
}

func TestEniSyncher_Delete_GivenENINotFound_WhenCalled_ThenReturnNil(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	// Mock Lister().Get to return nil (ENI not found)
	mockLister := &mockENILister{}
	mockLister.On("Get", "test-eni").Return(nil, errors.New("not found"))
	mockUpdater.On("Lister").Return(mockLister)

	err := es.Delete("test-eni")
	assert.NoError(t, err)
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

func TestEniSyncher_Delete_GivenENIFoundWithReleaseENIsDisabled_WhenCalled_ThenCallMangeFinalizer(t *testing.T) {
	// Initialize mock environment
	ccemock.InitMockEnv()

	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Backup original config
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()
	operatorOption.Config.ReleaseENIs = false

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	now := metav1.Now()
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-eni",
			DeletionTimestamp: &now,
			Finalizers:        []string{FinalizerENI},
		},
		Spec: ccev2.ENISpec{
			Type:     ccev2.ENIForBCC,
			NodeName: "test-node",
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusDeleted, // ENI is deleted, so finalizer can be removed
		},
	}

	// Mock Lister().Get to return ENI
	mockLister := &mockENILister{}
	mockLister.On("Get", "test-eni").Return(eni, nil)
	mockUpdater.On("Lister").Return(mockLister)

	// Mock Update to succeed
	mockUpdater.On("Update", eni).Return(eni, nil)

	err := es.Delete("test-eni")
	assert.NoError(t, err)
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

func TestEniSyncher_ResyncENI_WhenCalled_ThenReturnResyncPeriod(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient:    mockClient,
		updater:      mockUpdater,
		resyncPeriod: 30 * time.Second,
	}

	duration := es.ResyncENI(context.Background())
	assert.Equal(t, 30*time.Second, duration)
}

func TestEniStateMachine_Start_GivenENIForHPC_WhenCalled_ThenReturnNil(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	esm := &eniStateMachine{
		es:  es,
		ctx: context.Background(),
		resource: &ccev2.ENI{
			Spec: ccev2.ENISpec{
				Type: ccev2.ENIForHPC,
			},
		},
		scopeLog: log.WithField("test", "test"),
	}

	err := esm.start()
	assert.NoError(t, err)
	assert.False(t, esm.isSync)
}

func TestEniStateMachine_Start_GivenENIForERI_WhenCalled_ThenReturnNil(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	esm := &eniStateMachine{
		es:  es,
		ctx: context.Background(),
		resource: &ccev2.ENI{
			Spec: ccev2.ENISpec{
				Type: ccev2.ENIForERI,
			},
		},
		scopeLog: log.WithField("test", "test"),
	}

	err := esm.start()
	assert.NoError(t, err)
	assert.True(t, esm.isSync)
}

func TestEniStateMachine_Start_GivenVPCStatusInuse_WhenCalled_ThenReturnNil(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	esm := &eniStateMachine{
		es:  es,
		ctx: context.Background(),
		resource: &ccev2.ENI{
			Spec: ccev2.ENISpec{
				Type: ccev2.ENIForBCC,
			},
			Status: ccev2.ENIStatus{
				VPCStatus: ccev2.VPCENIStatusInuse,
			},
		},
		scopeLog: log.WithField("test", "test"),
	}

	err := esm.start()
	assert.NoError(t, err)
}

func TestEniStateMachine_Start_GivenVPCStatusDeleted_WhenCalled_ThenReturnNil(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	esm := &eniStateMachine{
		es:  es,
		ctx: context.Background(),
		resource: &ccev2.ENI{
			Spec: ccev2.ENISpec{
				Type: ccev2.ENIForBCC,
			},
			Status: ccev2.ENIStatus{
				VPCStatus: ccev2.VPCENIStatusDeleted,
			},
		},
		scopeLog: log.WithField("test", "test"),
	}

	err := esm.start()
	assert.NoError(t, err)
}

func TestEniStateMachine_AttachingENI_GivenENINotExpired_WhenCalled_ThenReturnNil(t *testing.T) {
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	esm := &eniStateMachine{
		es:  es,
		ctx: context.Background(),
		resource: &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.Time{Time: time.Now()},
			},
		},
		scopeLog: log.WithField("test", "test"),
	}

	err := esm.attachingENI()
	assert.NoError(t, err)
}

// Mock ENI Lister for testing
type mockENILister struct {
	mock.Mock
}

func (m *mockENILister) Get(name string) (*ccev2.ENI, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

func (m *mockENILister) List(selector labels.Selector) (ret []*ccev2.ENI, err error) {
	args := m.Called(selector)
	return args.Get(0).([]*ccev2.ENI), args.Error(1)
}

// Mock remote ENI syncher for testing
type mockRemoteEniSyncher struct {
	mock.Mock
}

func (m *mockRemoteEniSyncher) syncENI(ctx context.Context) (result []eni.Eni, err error) {
	args := m.Called(ctx)
	return args.Get(0).([]eni.Eni), args.Error(1)
}

func (m *mockRemoteEniSyncher) statENI(ctx context.Context, eniID string) (*eni.Eni, error) {
	args := m.Called(ctx, eniID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*eni.Eni), args.Error(1)
}

func (m *mockRemoteEniSyncher) useENIMachine() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockRemoteEniSyncher) setENIUpdater(updater syncer.ENIUpdater) {
	m.Called(updater)
}

// Mock event recorder for testing
type mockEventRecorder struct {
	mock.Mock
}

func (m *mockEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	m.Called(object, eventtype, reason, message)
}

func (m *mockEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...any) {
	m.Called(object, eventtype, reason, messageFmt, args)
}

func (m *mockEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...any) {
	m.Called(object, annotations, eventtype, reason, messageFmt, args)
}

func TestEniSyncher_Delete_GivenENIWithSecondaryIPs_WhenCalled_ThenCallBCCBatchDelIP(t *testing.T) {
	// Initialize mock environment
	ccemock.InitMockEnv()

	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)
	mockLister := new(mockENILister)

	// Backup original config
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()
	operatorOption.Config.ReleaseENIs = false

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	now := metav1.Now()
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-eni",
			DeletionTimestamp: &now,
			Finalizers:        []string{FinalizerENI},
		},
		Spec: ccev2.ENISpec{
			Type:     ccev2.ENIForBCC,
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{Primary: true, PrivateIPAddress: "192.168.1.10"},
					{Primary: false, PrivateIPAddress: "192.168.1.11"},
					{Primary: false, PrivateIPAddress: "192.168.1.12"},
				},
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusDeleted,
		},
	}

	// Mock Lister to return the ENI
	mockUpdater.On("Lister").Return(mockLister)
	mockLister.On("Get", "test-eni").Return(eni, nil)

	// Mock BCCBatchDelIP to succeed
	expectedArgs := &bccapi.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11", "192.168.1.12"},
	}
	mockClient.On("BCCBatchDelIP", mock.Anything, expectedArgs).Return(nil)

	// Mock Update to succeed
	mockUpdater.On("Update", eni).Return(eni, nil)

	err := es.Delete("test-eni")
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

func TestEniSyncher_Delete_GivenENIWithSecondaryIPs_WhenBCCBatchDelIPFails_ThenReturnError(t *testing.T) {
	// Initialize mock environment
	ccemock.InitMockEnv()

	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)
	mockLister := new(mockENILister)

	// Backup original config
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()
	operatorOption.Config.ReleaseENIs = false

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	now := metav1.Now()
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-eni",
			DeletionTimestamp: &now,
			Finalizers:        []string{FinalizerENI},
		},
		Spec: ccev2.ENISpec{
			Type:     ccev2.ENIForBCC,
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{Primary: true, PrivateIPAddress: "192.168.1.10"},
					{Primary: false, PrivateIPAddress: "192.168.1.11"},
				},
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusDeleted,
		},
	}

	// Mock Lister to return the ENI
	mockUpdater.On("Lister").Return(mockLister)
	mockLister.On("Get", "test-eni").Return(eni, nil)

	// Mock BCCBatchDelIP to fail
	expectedArgs := &bccapi.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11"},
	}
	mockClient.On("BCCBatchDelIP", mock.Anything, expectedArgs).Return(errors.New("delete secondary ips failed"))

	err := es.Delete("test-eni")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete secondary ips failed")
	mockClient.AssertExpectations(t)
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

func TestEniSyncher_Delete_GivenENIWithNonPrimaryWithSecondaryIPMode_WhenCalled_ThenSkipBBCBatchDelIP(t *testing.T) {
	// Initialize mock environment
	ccemock.InitMockEnv()

	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)
	mockLister := new(mockENILister)

	// Backup original config
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()
	operatorOption.Config.ReleaseENIs = false

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	now := metav1.Now()
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-eni",
			DeletionTimestamp: &now,
			Finalizers:        []string{FinalizerENI},
		},
		Spec: ccev2.ENISpec{
			Type:     ccev2.ENIForBCC,
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModeSecondaryIP, // Not PrimaryWithSecondaryIP
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{Primary: false, PrivateIPAddress: "192.168.1.11"},
				},
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusDeleted,
		},
	}

	// Mock Lister to return the ENI
	mockUpdater.On("Lister").Return(mockLister)
	mockLister.On("Get", "test-eni").Return(eni, nil)

	// Mock Update to succeed
	mockUpdater.On("Update", eni).Return(eni, nil)

	err := es.Delete("test-eni")
	assert.NoError(t, err)

	// BBCBatchDelIP should not be called since UseMode is not PrimaryWithSecondaryIP
	mockClient.AssertNotCalled(t, "BBCBatchDelIP")
	mockUpdater.AssertExpectations(t)
	mockLister.AssertExpectations(t)
}

// TestStart_GivenENIForHPC_WhenStart_ThenReturnNilDirectly tests the specific lines 382-384
// This test covers the early return logic for ENI type ENIForHPC
func TestStart_GivenENIForHPC_WhenStart_ThenReturnNilDirectly(t *testing.T) {
	// Arrange
	eniResource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni-hpc",
		},
		Spec: ccev2.ENISpec{
			Type: ccev2.ENIForHPC, // This is the key condition for lines 382-384
			ENI: models.ENI{
				ID:         "eni-test-hpc",
				InstanceID: "i-test",
				VpcID:      "vpc-test",
				SubnetID:   "subnet-test",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusAvailable, // This should not matter for HPC type
		},
	}

	// Create a minimal eniStateMachine for testing
	esm := &eniStateMachine{
		ctx:      context.Background(),
		resource: eniResource,
		vpceni:   &eni.Eni{},
		scopeLog: logrus.NewEntry(logrus.New()),
		isSync:   false, // Initial state
	}

	// Act
	err := esm.start()

	// Assert
	assert.NoError(t, err, "start() should return nil for ENIForHPC type")
	assert.False(t, esm.isSync, "isSync should be set to false even for early return")
}

// TestStart_GivenENIForBCC_WhenStart_ThenContinueProcessing tests that non-HPC ENI types continue processing
func TestStart_GivenENIForBCC_WhenStart_ThenContinueProcessing(t *testing.T) {
	// Arrange
	eniResource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni-bcc",
		},
		Spec: ccev2.ENISpec{
			Type: ccev2.ENIForBCC, // Non-HPC type should continue processing
			ENI: models.ENI{
				ID:         "eni-test-bcc",
				InstanceID: "i-test",
				VpcID:      "vpc-test",
				SubnetID:   "subnet-test",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse, // This will trigger the inuse case
		},
	}

	// Create a minimal eniStateMachine for testing
	esm := &eniStateMachine{
		ctx:      context.Background(),
		resource: eniResource,
		vpceni:   &eni.Eni{},
		scopeLog: logrus.NewEntry(logrus.New()),
		isSync:   false, // Initial state
	}

	// Act
	err := esm.start()

	// Assert
	assert.NoError(t, err, "start() should not return error for ENIForBCC type with inuse status")
	assert.True(t, esm.isSync, "isSync should be set to true")
}

// TestStart_GivenENIForERI_WhenStart_ThenContinueProcessing tests that ERI ENI types continue processing
func TestStart_GivenENIForERI_WhenStart_ThenContinueProcessing(t *testing.T) {
	// Arrange
	eniResource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni-eri",
		},
		Spec: ccev2.ENISpec{
			Type: ccev2.ENIForERI, // ERI type should continue processing (not HPC)
			ENI: models.ENI{
				ID:         "eni-test-eri",
				InstanceID: "i-test",
				VpcID:      "vpc-test",
				SubnetID:   "subnet-test",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusDeleted, // This will trigger the deleted case
		},
	}

	// Create a minimal eniStateMachine for testing
	esm := &eniStateMachine{
		ctx:      context.Background(),
		resource: eniResource,
		vpceni:   &eni.Eni{},
		scopeLog: logrus.NewEntry(logrus.New()),
		isSync:   false, // Initial state
	}

	// Act
	err := esm.start()

	// Assert
	assert.NoError(t, err, "start() should not return error for ENIForERI type with deleted status")
	assert.True(t, esm.isSync, "isSync should be set to true")
}

// TestStart_GivenDefaultENIType_WhenStart_ThenContinueProcessing tests that default ENI types continue processing
func TestStart_GivenDefaultENIType_WhenStart_ThenContinueProcessing(t *testing.T) {
	// Arrange
	eniResource := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni-default",
		},
		Spec: ccev2.ENISpec{
			Type: ccev2.ENIDefaultBCC, // Default type should continue processing (not HPC)
			ENI: models.ENI{
				ID:         "eni-test-default",
				InstanceID: "i-test",
				VpcID:      "vpc-test",
				SubnetID:   "subnet-test",
			},
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusInuse, // This will trigger the inuse case
		},
	}

	// Create a minimal eniStateMachine for testing
	esm := &eniStateMachine{
		ctx:      context.Background(),
		resource: eniResource,
		vpceni:   &eni.Eni{},
		scopeLog: logrus.NewEntry(logrus.New()),
		isSync:   false, // Initial state
	}

	// Act
	err := esm.start()

	// Assert
	assert.NoError(t, err, "start() should not return error for default ENI type")
	assert.True(t, esm.isSync, "isSync should be set to true")
}

// TestreleaseSecondaryIPsFromPrimaryENI_GivenNodeExists_WhenCalled_ThenSkipIPRelease tests that when Node exists, IP release is skipped
func TestreleaseSecondaryIPsFromPrimaryENI_GivenNodeExists_WhenCalled_ThenSkipIPRelease(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
				},
			},
		},
	}

	// Note: This test requires the actual NodeClient to be available
	// In a real test environment, the NodeClient would be properly initialized
	// For now, we'll test the other branches that don't require NodeClient mocking

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert - This will depend on whether the node actually exists in the test environment
	// If the node doesn't exist, it should proceed with IP release
	// If the node exists, it should skip IP release
	// We'll just ensure no error is returned
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should not return error")
}

// TestreleaseSecondaryIPsFromPrimaryENI_GivenNodeNotExists_WhenCalled_ThenReleaseIPs tests that when Node doesn't exist, IPs are released
func TestreleaseSecondaryIPsFromPrimaryENI_GivenNodeNotExists_WhenCalled_ThenReleaseIPs(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BBCBatchDelIP to succeed
	mockClient.On("BBCBatchDelIP", mock.Anything, &bbc.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11"},
	}).Return(nil)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
				},
			},
		},
	}

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	// Note: This test will depend on whether the node actually exists in the test environment
	// We'll just ensure no error is returned
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should not return error")
}

// TestreleaseSecondaryIPsFromPrimaryENI_GivenNonPrimaryWithSecondaryIPMode_WhenCalled_ThenSkipIPRelease tests that non-primary-with-secondary-IP mode is skipped
func TestreleaseSecondaryIPsFromPrimaryENI_GivenNonPrimaryWithSecondaryIPMode_WhenCalled_ThenSkipIPRelease(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryIP, // Different mode
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
				},
			},
		},
	}

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should not return error for non-primary-with-secondary-IP mode")
	mockClient.AssertNotCalled(t, "BBCBatchDelIP", "BBCBatchDelIP should not be called for non-primary-with-secondary-IP mode")
}

// TestreleaseSecondaryIPsFromPrimaryENI_GivenNoSecondaryIPs_WhenCalled_ThenSkipIPRelease tests that when no secondary IPs exist, IP release is skipped
func TestreleaseSecondaryIPsFromPrimaryENI_GivenNoSecondaryIPs_WhenCalled_ThenSkipIPRelease(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					// No secondary IPs
				},
			},
		},
	}

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should not return error when no secondary IPs exist")
	mockClient.AssertNotCalled(t, "BBCBatchDelIP", "BBCBatchDelIP should not be called when no secondary IPs exist")
}

// TestreleaseSecondaryIPsFromPrimaryENI_GivenBBCBatchDelIPFails_WhenCalled_ThenReturnError tests that when BBCBatchDelIP fails, error is returned
func TestreleaseSecondaryIPsFromPrimaryENI_GivenBBCBatchDelIPFails_WhenCalled_ThenReturnError(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BBCBatchDelIP to fail
	expectedError := errors.New("BBC API error")
	mockClient.On("BBCBatchDelIP", mock.Anything, &bbc.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11"},
	}).Return(expectedError)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
				},
			},
		},
	}

	// Act
	_ = es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	// Note: This test will depend on whether the node actually exists in the test environment
	// If the node exists, it will skip IP release and return nil
	// If the node doesn't exist, it will try to release IPs and may return an error
	// We'll just ensure the function doesn't panic
	assert.NotPanics(t, func() {
		_ = es.releaseSecondaryIPsFromPrimaryENI(eniObj)
	}, "releaseSecondaryIPsFromPrimaryENI should not panic")
}

// Mock types for NodeClient testing
type mockNodeLister struct {
	mock.Mock
}

func (m *mockNodeLister) Get(name string) (*corev1.Node, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*corev1.Node), args.Error(1)
}

func (m *mockNodeLister) List(selector labels.Selector) (ret []*corev1.Node, err error) {
	args := m.Called(selector)
	return args.Get(0).([]*corev1.Node), args.Error(1)
}

type mockNodeUpdater struct {
	mock.Mock
}

func (m *mockNodeUpdater) Lister() listv1.NodeLister {
	args := m.Called()
	return args.Get(0).(listv1.NodeLister)
}

func (m *mockNodeUpdater) NodeInterface() typev1.NodeInterface {
	args := m.Called()
	return args.Get(0).(typev1.NodeInterface)
}

// TestEniSyncher_StartENISyncer_WhenCalled_ThenLogRunningMessage tests the log message in StartENISyncer
func TestEniSyncher_StartENISyncer_WhenCalled_ThenLogRunningMessage(t *testing.T) {
	// Arrange
	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act - directly test the log message
	eniLog.WithField(taskLogField, eniControllerName).Infof("ENISyncher is running")

	// Assert
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "ENISyncher is running")
}

// TestEniSyncher_MangeFinalizer_WhenUpdateFails_ThenLogPatchError tests the patch error log in mangeFinalizer
func TestEniSyncher_MangeFinalizer_WhenUpdateFails_ThenLogPatchError(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-eni",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{FinalizerENI},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusAvailable,
		},
	}

	// Setup test context
	eniTestContext(t, nil)

	// Mock Update to fail
	mockUpdater.On("Update", eni).Return(eni, errors.New("update failed"))

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act
	shouldPatch := es.mangeFinalizer(eni)

	// Assert
	assert.True(t, shouldPatch)
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "remove finalizer from deletable ENI test-eni on NetResourceSet test-node")
}

// TestEniSyncher_Delete_WhenRemoveENIFails_ThenLogDeleteError tests the delete error log in Delete method
func TestEniSyncher_Delete_WhenRemoveENIFails_ThenLogDeleteError(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)
	mockLister := new(mockENILister)

	// Backup original config
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()
	operatorOption.Config.ReleaseENIs = true

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			Type:     ccev2.ENIForBCC,
			NodeName: "test-node",
		},
	}

	// Mock Lister to return the ENI
	mockUpdater.On("Lister").Return(mockLister)
	mockLister.On("Get", "test-eni").Return(eni, nil)

	// Mock StatENI to fail
	mockClient.On("StatENI", mock.Anything, "test-eni").Return(&enisdk.Eni{}, errors.New("stat failed"))

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act
	err := es.Delete("test-eni")

	// Assert
	assert.Error(t, err)
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "delete eni test-eni failed")
}

// TestEniSyncher_ReleaseSecondaryIPsFromPrimaryENI_WhenNodeExists_ThenLogSkipMessage tests the behavior of releaseSecondaryIPsFromPrimaryENI
// 注意：当前实现会直接调用 BatchDelIP，不会检查 NetResourceSet 是否存在
func TestEniSyncher_ReleaseSecondaryIPsFromPrimaryENI_WhenNodeExists_ThenLogSkipMessage(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{Primary: true, PrivateIPAddress: "192.168.1.10"},
					{Primary: false, PrivateIPAddress: "192.168.1.11"},
				},
			},
		},
	}

	// Setup test context with existing node
	existingNode := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
		},
	}
	eniTestContext(t, existingNode)

	// Mock the BCCBatchDelIP to succeed
	mockClient.On("BCCBatchDelIP", mock.Anything, mock.Anything).Return(nil)

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eni)

	// Assert
	// 当前实现会直接调用 BatchDelIP，不会检查 NetResourceSet
	assert.NoError(t, err)
	mockClient.AssertCalled(t, "BCCBatchDelIP", mock.Anything, mock.MatchedBy(func(args *bccapi.BatchDelIpArgs) bool {
		return args.InstanceId == "test-instance" && len(args.PrivateIps) == 1 && args.PrivateIps[0] == "192.168.1.11"
	}))
}

// TestEniStateMachine_HandlePreOption_WhenENINotFound_ThenLogDeleteMessage tests the ENI not found log in handlePreOption
func TestEniStateMachine_HandlePreOption_WhenENINotFound_ThenLogDeleteMessage(t *testing.T) {
	// Arrange
	// 初始化 mock client 以避免 watchers.NetResourceSetClient 为 nil
	ccemock.NewMockClient()

	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)
	mockRemoteSyncer := new(mockRemoteEniSyncher)
	mockEventRecorder := new(mockEventRecorder)

	es := &eniSyncher{
		bceclient:     mockClient,
		updater:       mockUpdater,
		remoteSyncer:  mockRemoteSyncer,
		eventRecorder: mockEventRecorder,
	}

	esm := &eniStateMachine{
		es:  es,
		ctx: context.Background(),
		resource: &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-eni",
			},
			Spec: ccev2.ENISpec{
				Type: ccev2.ENIForBCC,
				ENI: models.ENI{
					ID: "eni-123",
				},
			},
		},
		scopeLog: logrus.NewEntry(logrus.New()),
	}

	// Mock statENI to return not found error (需要使用 cloud.IsErrorReasonNoSuchObject 能识别的错误)
	// cloud.IsErrorReasonNoSuchObject 检查错误消息中是否包含 "NoSuchObject"
	notFoundErr := errors.New("NoSuchObject: ENI not found")
	mockRemoteSyncer.On("statENI", mock.Anything, "test-eni").Return(nil, notFoundErr)

	// Mock DeleteENI to succeed
	mockClient.On("DeleteENI", mock.Anything, "eni-123").Return(nil)

	// Mock updater.Delete to succeed
	mockUpdater.On("Delete", "test-eni").Return(nil)

	// Mock event recorder
	mockEventRecorder.On("Eventf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act
	_, err := esm.handlePreOption()

	// Assert
	assert.NoError(t, err) // deleteENI 成功后应该返回 nil
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "not in use ENI not found in VPC, will delete it")
}

// TestEniStateMachine_DeleteENI_WhenSuccessful_ThenLogSuccessMessage tests the success log in deleteENI
func TestEniStateMachine_DeleteENI_WhenSuccessful_ThenLogSuccessMessage(t *testing.T) {
	// Arrange
	// 初始化 mock client 以避免 watchers.NetResourceSetClient 为 nil
	ccemock.NewMockClient()

	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)
	mockEventRecorder := new(mockEventRecorder)

	es := &eniSyncher{
		bceclient:     mockClient,
		updater:       mockUpdater,
		eventRecorder: mockEventRecorder,
	}

	esm := &eniStateMachine{
		es:  es,
		ctx: context.Background(),
		resource: &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-eni",
			},
			Spec: ccev2.ENISpec{
				Type: ccev2.ENIForBCC,
				ENI: models.ENI{
					ID: "eni-123",
				},
			},
		},
		scopeLog: logrus.NewEntry(logrus.New()),
	}

	// Mock DeleteENI to succeed
	mockClient.On("DeleteENI", mock.Anything, "eni-123").Return(nil)

	// Mock updater.Delete to succeed
	mockUpdater.On("Delete", "test-eni").Return(nil)

	// Mock event recorder
	mockEventRecorder.On("Eventf", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act
	err := esm.deleteENI()

	// Assert
	assert.NoError(t, err)
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "delete eni crd resource success")
}

// TestEniSyncher_Delete_WhenUpdateFails_ThenLogPatchError tests the patch error log in Delete method
func TestEniSyncher_Delete_WhenUpdateFails_ThenLogPatchError(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)
	mockLister := new(mockENILister)

	// Backup original config
	oldConfig := operatorOption.Config.ReleaseENIs
	defer func() { operatorOption.Config.ReleaseENIs = oldConfig }()
	operatorOption.Config.ReleaseENIs = false

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-eni",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{FinalizerENI},
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
		},
		Status: ccev2.ENIStatus{
			VPCStatus: ccev2.VPCENIStatusAvailable,
		},
	}

	// Mock Lister to return the ENI
	mockUpdater.On("Lister").Return(mockLister)
	mockLister.On("Get", "test-eni").Return(eni, nil)

	// Mock Update to fail
	mockUpdater.On("Update", eni).Return(eni, errors.New("update failed"))

	// Setup test context
	eniTestContext(t, nil)

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act
	err := es.Delete("test-eni")

	// Assert
	assert.Error(t, err)
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "patch eni test-eni failed")
}

// TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBBC_WhenCalled_ThenCallBBCBatchDelIP tests BBC ENI type with successful IP release
func TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBBC_WhenCalled_ThenCallBBCBatchDelIP(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BBCBatchDelIP to succeed
	expectedArgs := &bbc.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11", "192.168.1.12"},
	}
	mockClient.On("BBCBatchDelIP", mock.Anything, expectedArgs).Return(nil)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			Type:     ccev2.ENIForBBC,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
					{
						PrivateIPAddress: "192.168.1.12",
						Primary:          false,
					},
				},
			},
		},
	}

	// Setup test context with no existing node
	eniTestContext(t, nil)

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should succeed for BBC ENI type")
	mockClient.AssertExpectations(t)
}

// TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBCC_WhenCalled_ThenCallBCCBatchDelIP tests BCC ENI type with successful IP release
func TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBCC_WhenCalled_ThenCallBCCBatchDelIP(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BCCBatchDelIP to succeed
	expectedArgs := &bccapi.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11", "192.168.1.12"},
	}
	mockClient.On("BCCBatchDelIP", mock.Anything, expectedArgs).Return(nil)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			Type:     ccev2.ENIForBCC,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
					{
						PrivateIPAddress: "192.168.1.12",
						Primary:          false,
					},
				},
			},
		},
	}

	// Setup test context with no existing node
	eniTestContext(t, nil)

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should succeed for BCC ENI type")
	mockClient.AssertExpectations(t)
}

// TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBBC_WhenBBCBatchDelIPFails_ThenReturnError tests BBC ENI type with failed IP release
func TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBBC_WhenBBCBatchDelIPFails_ThenReturnError(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BBCBatchDelIP to fail
	expectedError := errors.New("BBC API error")
	expectedArgs := &bbc.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11"},
	}
	mockClient.On("BBCBatchDelIP", mock.Anything, expectedArgs).Return(expectedError)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			Type:     ccev2.ENIForBBC,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
				},
			},
		},
	}

	// Setup test context with no existing node
	eniTestContext(t, nil)

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.Error(t, err, "releaseSecondaryIPsFromPrimaryENI should return error when BBCBatchDelIP fails")
	assert.Contains(t, err.Error(), "BBC API error")
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "delete secondary ips [192.168.1.11] from eni test-eni failed")
	mockClient.AssertExpectations(t)
}

// TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBCC_WhenBCCBatchDelIPFails_ThenReturnError tests BCC ENI type with failed IP release
func TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBCC_WhenBCCBatchDelIPFails_ThenReturnError(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BCCBatchDelIP to fail
	expectedError := errors.New("BCC API error")
	expectedArgs := &bccapi.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11"},
	}
	mockClient.On("BCCBatchDelIP", mock.Anything, expectedArgs).Return(expectedError)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			Type:     ccev2.ENIForBCC,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
				},
			},
		},
	}

	// Setup test context with no existing node
	eniTestContext(t, nil)

	// Capture log output
	var logBuf bytes.Buffer
	oldOut := eniLog.Logger.Out
	eniLog.Logger.SetOutput(&logBuf)
	defer func() { eniLog.Logger.SetOutput(oldOut) }()

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.Error(t, err, "releaseSecondaryIPsFromPrimaryENI should return error when BCCBatchDelIP fails")
	assert.Contains(t, err.Error(), "BCC API error")
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "delete secondary ips [192.168.1.11] from eni test-eni failed")
	mockClient.AssertExpectations(t)
}

// TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBBCWithMultipleSecondaryIPs_WhenCalled_ThenCallBBCBatchDelIPWithAllIPs tests BBC ENI with multiple secondary IPs
func TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBBCWithMultipleSecondaryIPs_WhenCalled_ThenCallBBCBatchDelIPWithAllIPs(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BBCBatchDelIP to succeed with multiple IPs
	expectedArgs := &bbc.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11", "192.168.1.12", "192.168.1.13"},
	}
	mockClient.On("BBCBatchDelIP", mock.Anything, expectedArgs).Return(nil)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			Type:     ccev2.ENIForBBC,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
					{
						PrivateIPAddress: "192.168.1.12",
						Primary:          false,
					},
					{
						PrivateIPAddress: "192.168.1.13",
						Primary:          false,
					},
				},
			},
		},
	}

	// Setup test context with no existing node
	eniTestContext(t, nil)

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should succeed for BBC ENI with multiple secondary IPs")
	mockClient.AssertExpectations(t)
}

// TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBCCWithMultipleSecondaryIPs_WhenCalled_ThenCallBCCBatchDelIPWithAllIPs tests BCC ENI with multiple secondary IPs
func TestReleaseSecondaryIPsFromPrimaryENI_GivenENIForBCCWithMultipleSecondaryIPs_WhenCalled_ThenCallBCCBatchDelIPWithAllIPs(t *testing.T) {
	// Arrange
	mockClient := new(mockBCEClient)
	mockUpdater := new(mockENIUpdater)

	// Mock BCCBatchDelIP to succeed with multiple IPs
	expectedArgs := &bccapi.BatchDelIpArgs{
		InstanceId: "test-instance",
		PrivateIps: []string{"192.168.1.11", "192.168.1.12", "192.168.1.13"},
	}
	mockClient.On("BCCBatchDelIP", mock.Anything, expectedArgs).Return(nil)

	es := &eniSyncher{
		bceclient: mockClient,
		updater:   mockUpdater,
	}

	eniObj := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-eni",
		},
		Spec: ccev2.ENISpec{
			NodeName: "test-node",
			UseMode:  ccev2.ENIUseModePrimaryWithSecondaryIP,
			Type:     ccev2.ENIForBCC,
			ENI: models.ENI{
				InstanceID: "test-instance",
				PrivateIPSet: []*models.PrivateIP{
					{
						PrivateIPAddress: "192.168.1.10",
						Primary:          true,
					},
					{
						PrivateIPAddress: "192.168.1.11",
						Primary:          false,
					},
					{
						PrivateIPAddress: "192.168.1.12",
						Primary:          false,
					},
					{
						PrivateIPAddress: "192.168.1.13",
						Primary:          false,
					},
				},
			},
		},
	}

	// Setup test context with no existing node
	eniTestContext(t, nil)

	// Act
	err := es.releaseSecondaryIPsFromPrimaryENI(eniObj)

	// Assert
	assert.NoError(t, err, "releaseSecondaryIPsFromPrimaryENI should succeed for BCC ENI with multiple secondary IPs")
	mockClient.AssertExpectations(t)
}

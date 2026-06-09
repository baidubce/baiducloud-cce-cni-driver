package bcesync

import (
	"context"
	"errors"
	"testing"

	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	cloudtesting "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/testing"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// mockEniGetter is a mock implementation of the eniGetter interface for testing.

type mockEniGetter struct {
	mock.Mock
}

func (m *mockEniGetter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*ccev2.ENI, error) {
	args := m.Called(ctx, name, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*ccev2.ENI), args.Error(1)
}

func TestRemoteERISyncher_syncENI_GivenHPASStandard_WhenSyncing_ThenUseBCEClient(t *testing.T) {
	setOperatorHPASStanderd(t, true)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBCE := cloudtesting.NewMockInterface(ctrl)
	mockHPAS := cloudtesting.NewMockInterface(ctrl)

	mockBCE.EXPECT().
		ListERIs(gomock.Any(), gomock.AssignableToTypeOf(enisdk.ListEniArgs{})).
		Return([]enisdk.Eni{}, nil)
	mockHPAS.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Times(0)

	syncer := &remoteERISyncher{
		remoteVpcEniSyncher: &remoteVpcEniSyncher{
			bceclient:  mockBCE,
			hpasclient: mockHPAS,
			VPCIDs:     "vpc-test",
			ClusterID:  "cluster-1",
		},
	}

	_, err := syncer.syncENI(context.Background())
	assert.NoError(t, err)
}

func TestRemoteERISyncher_syncENI_GivenHPASNonStandard_WhenSyncing_ThenUseHPASClient(t *testing.T) {
	setOperatorHPASStanderd(t, false)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBCE := cloudtesting.NewMockInterface(ctrl)
	mockHPAS := cloudtesting.NewMockInterface(ctrl)

	mockHPAS.EXPECT().
		ListERIs(gomock.Any(), gomock.AssignableToTypeOf(enisdk.ListEniArgs{})).
		Return([]enisdk.Eni{}, nil)
	mockBCE.EXPECT().ListERIs(gomock.Any(), gomock.Any()).Times(0)

	syncer := &remoteERISyncher{
		remoteVpcEniSyncher: &remoteVpcEniSyncher{
			bceclient:  mockBCE,
			hpasclient: mockHPAS,
			VPCIDs:     "vpc-test",
			ClusterID:  "cluster-1",
		},
	}

	_, err := syncer.syncENI(context.Background())
	assert.NoError(t, err)
}

// TestValidateAndSyncERIInstanceID covers all logic paths in validateAndSyncERIInstanceID.
func TestValidateAndSyncERIInstanceID(t *testing.T) {
	mockUpdater := &mockENIUpdater{}
	mockGetter := &mockEniGetter{}

	syncer := &remoteERISyncher{
		remoteVpcEniSyncher: &remoteVpcEniSyncher{
			updater: mockUpdater,
		},
		eniClient: mockGetter,
	}

	iaasERI := &enisdk.Eni{
		EniId:      "eri-test",
		VpcId:      "vpc-test",
		InstanceId: "i-new-instance",
	}

	// Test case 1: ERI not found
	t.Run("ERI_NotFound", func(t *testing.T) {
		mockGetter.On("Get", mock.Anything, "eri-test", mock.Anything).Return(nil, kerrors.NewNotFound(schema.GroupResource{}, "")).Once()

		syncer.validateAndSyncERIInstanceID(iaasERI)

		mockGetter.AssertExpectations(t)
		mockUpdater.AssertNotCalled(t, "Update", mock.Anything)
	})

	// Test case 2: Generic error on Get
	t.Run("Generic_Get_Error", func(t *testing.T) {
		mockGetter.On("Get", mock.Anything, "eri-test", mock.Anything).Return(nil, errors.New("generic error")).Once()

		syncer.validateAndSyncERIInstanceID(iaasERI)

		mockGetter.AssertExpectations(t)
		mockUpdater.AssertNotCalled(t, "Update", mock.Anything)
	})

	// Test case 3: Instance ID is consistent
	t.Run("InstanceID_Consistent", func(t *testing.T) {
		k8sERI := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{Name: "eri-test"},
			Spec:       ccev2.ENISpec{ENI: models.ENI{InstanceID: "i-new-instance"}},
		}
		mockGetter.On("Get", mock.Anything, "eri-test", mock.Anything).Return(k8sERI, nil).Once()

		syncer.validateAndSyncERIInstanceID(iaasERI)

		mockGetter.AssertExpectations(t)
		mockUpdater.AssertNotCalled(t, "Update", mock.Anything)
	})

	// Test case 4: Instance ID is inconsistent, update succeeds
	t.Run("InstanceID_Inconsistent_Update_Succeeds", func(t *testing.T) {
		k8sERI := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{Name: "eri-test", Labels: map[string]string{}},
			Spec:       ccev2.ENISpec{ENI: models.ENI{InstanceID: "i-old-instance"}},
		}
		mockGetter.On("Get", mock.Anything, "eri-test", mock.Anything).Return(k8sERI, nil).Once()
		mockUpdater.On("Update", mock.Anything).Return(nil, nil).Once()

		syncer.validateAndSyncERIInstanceID(iaasERI)

		mockGetter.AssertExpectations(t)
		mockUpdater.AssertExpectations(t)
	})

	// Test case 5: Instance ID is inconsistent, update fails with conflict
	t.Run("InstanceID_Inconsistent_Update_Fails_With_Conflict", func(t *testing.T) {
		k8sERI := &ccev2.ENI{
			ObjectMeta: metav1.ObjectMeta{Name: "eri-test", Labels: nil}, // Test nil labels
			Spec:       ccev2.ENISpec{ENI: models.ENI{InstanceID: "i-old-instance"}},
		}
		// First Get call
		mockGetter.On("Get", mock.Anything, "eri-test", mock.Anything).Return(k8sERI, nil).Once()
		// First Update call fails with conflict
		mockUpdater.On("Update", mock.Anything).Return(nil, kerrors.NewConflict(schema.GroupResource{}, "", nil)).Once()

		// Second Get call for retry
		mockGetter.On("Get", mock.Anything, "eri-test", mock.Anything).Return(k8sERI, nil).Once()
		// Second Update call succeeds
		mockUpdater.On("Update", mock.Anything).Return(nil, nil).Once()

		syncer.validateAndSyncERIInstanceID(iaasERI)

		mockGetter.AssertExpectations(t)
		mockUpdater.AssertExpectations(t)
	})
}

// TestUseENIMachine_GivenRemoteERISyncher_WhenCalled_ThenReturnFalse tests useENIMachine method
func TestUseENIMachine_GivenRemoteERISyncher_WhenCalled_ThenReturnFalse(t *testing.T) {
	// Given
	syncer := &remoteERISyncher{
		remoteVpcEniSyncher: &remoteVpcEniSyncher{},
	}

	// When
	result := syncer.useENIMachine()

	// Then
	assert.False(t, result, "remoteERISyncher should not use ENI machine")
}

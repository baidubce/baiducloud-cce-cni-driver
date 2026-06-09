package bcesync

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/eni"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

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
	assert.True(t, esm.isSync, "isSync should be set to true even for early return")
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

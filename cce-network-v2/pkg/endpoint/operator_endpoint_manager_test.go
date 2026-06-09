//go:build !linux
// +build !linux

package endpoint

import (
	"context"
	"sync"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccelister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v2"
)

// MockCCEEndpointGetterUpdater is a mock implementation of CCEEndpointGetterUpdater
type MockCCEEndpointGetterUpdater struct {
	mock.Mock
}

func (m *MockCCEEndpointGetterUpdater) Create(node *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	args := m.Called(node)
	return args.Get(0).(*ccev2.CCEEndpoint), args.Error(1)
}

func (m *MockCCEEndpointGetterUpdater) Update(newResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	args := m.Called(newResource)
	return args.Get(0).(*ccev2.CCEEndpoint), args.Error(1)
}

func (m *MockCCEEndpointGetterUpdater) UpdateStatus(newResource *ccev2.CCEEndpoint) (*ccev2.CCEEndpoint, error) {
	args := m.Called(newResource)
	return args.Get(0).(*ccev2.CCEEndpoint), args.Error(1)
}

func (m *MockCCEEndpointGetterUpdater) Delete(namespace, name string) error {
	args := m.Called(namespace, name)
	return args.Error(0)
}

func (m *MockCCEEndpointGetterUpdater) Lister() ccelister.CCEEndpointLister {
	args := m.Called()
	return args.Get(0).(ccelister.CCEEndpointLister)
}

// MockCCEEndpointLister is a mock implementation of CCEEndpointLister
type MockCCEEndpointLister struct {
	mock.Mock
}

func (m *MockCCEEndpointLister) List(selector labels.Selector) ([]*ccev2.CCEEndpoint, error) {
	args := m.Called(selector)
	return args.Get(0).([]*ccev2.CCEEndpoint), args.Error(1)
}

func (m *MockCCEEndpointLister) CCEEndpoints(namespace string) ccelister.CCEEndpointNamespaceLister {
	args := m.Called(namespace)
	return args.Get(0).(ccelister.CCEEndpointNamespaceLister)
}

// MockCCEEndpointNamespaceLister is a mock implementation of CCEEndpointNamespaceLister
type MockCCEEndpointNamespaceLister struct {
	mock.Mock
}

func (m *MockCCEEndpointNamespaceLister) List(selector labels.Selector) ([]*ccev2.CCEEndpoint, error) {
	args := m.Called(selector)
	return args.Get(0).([]*ccev2.CCEEndpoint), args.Error(1)
}

func (m *MockCCEEndpointNamespaceLister) Get(name string) (*ccev2.CCEEndpoint, error) {
	args := m.Called(name)
	return args.Get(0).(*ccev2.CCEEndpoint), args.Error(1)
}

// MockDirectIPAllocator is a mock implementation of DirectIPAllocator
type MockDirectIPAllocator struct {
	mock.Mock
}

func (m *MockDirectIPAllocator) ResyncPool(ctx context.Context, scopedLog *logrus.Entry) (map[string]ipamTypes.AllocationMap, error) {
	args := m.Called(ctx, scopedLog)
	return args.Get(0).(map[string]ipamTypes.AllocationMap), args.Error(1)
}

func (m *MockDirectIPAllocator) NodeEndpoint(cep *ccev2.CCEEndpoint) (DirectEndpointOperation, error) {
	args := m.Called(cep)
	return args.Get(0).(DirectEndpointOperation), args.Error(1)
}

// MockDirectEndpointOperation is a mock implementation of DirectEndpointOperation
type MockDirectEndpointOperation struct {
	mock.Mock
}

func (m *MockDirectEndpointOperation) FilterAvailableSubnetIds(subnetIds []string, count int) []*bcesync.BorrowedSubnet {
	args := m.Called(subnetIds, count)
	return args.Get(0).([]*bcesync.BorrowedSubnet)
}

func (m *MockDirectEndpointOperation) AllocateIP(ctx context.Context, action *DirectIPAction) error {
	args := m.Called(ctx, action)
	return args.Error(0)
}

func (m *MockDirectEndpointOperation) DeleteIP(ctx context.Context, allocation *DirectIPAction) error {
	args := m.Called(ctx, allocation)
	return args.Error(0)
}

func TestEndpointManager_Update(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*MockCCEEndpointGetterUpdater, *MockCCEEndpointLister, *MockDirectIPAllocator)
		cep            *ccev2.CCEEndpoint
		expectedError  bool
		expectedResult *ccev2.CCEEndpoint
	}{
		{
			name: "non-managed endpoint",
			setupMocks: func(mockUpdater *MockCCEEndpointGetterUpdater, mockLister *MockCCEEndpointLister, mockAllocator *MockDirectIPAllocator) {
				// No mocks needed as the endpoint is not managed
			},
			cep: &ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test-ep",
				},
				Spec: ccev2.EndpointSpec{
					Network: ccev2.EndpointNetworkSpec{
						IPAllocation: &ccev2.IPAllocation{
							DirectIPAllocation: ccev2.DirectIPAllocation{
								Type:            ccev2.IPAllocTypeElastic,
								ReleaseStrategy: ccev2.ReleaseStrategyTTL,
							},
						},
					},
				},
			},
			expectedError:  false,
			expectedResult: nil,
		},
		{
			name: "stale resource version",
			setupMocks: func(mockUpdater *MockCCEEndpointGetterUpdater, mockLister *MockCCEEndpointLister, mockAllocator *MockDirectIPAllocator) {
				cep := &ccev2.CCEEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "test-ns",
						Name:            "test-ep",
						ResourceVersion: "2",
					},
					Spec: ccev2.EndpointSpec{
						Network: ccev2.EndpointNetworkSpec{
							IPAllocation: &ccev2.IPAllocation{
								DirectIPAllocation: ccev2.DirectIPAllocation{
									Type:            ccev2.IPAllocTypeElastic,
									ReleaseStrategy: ccev2.ReleaseStrategyTTL,
								},
							},
						},
					},
				}
				mockUpdater.On("Update", cep).Return(cep, nil)
			},
			cep: &ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:       "test-ns",
					Name:            "test-ep",
					ResourceVersion: "1",
				},
				Spec: ccev2.EndpointSpec{
					Network: ccev2.EndpointNetworkSpec{
						IPAllocation: &ccev2.IPAllocation{
							DirectIPAllocation: ccev2.DirectIPAllocation{
								Type:            ccev2.IPAllocTypeElastic,
								ReleaseStrategy: ccev2.ReleaseStrategyTTL,
							},
						},
					},
				},
			},
			expectedError:  false,
			expectedResult: nil,
		},
		{
			name: "successful IP allocation",
			setupMocks: func(mockUpdater *MockCCEEndpointGetterUpdater, mockLister *MockCCEEndpointLister, mockAllocator *MockDirectIPAllocator) {
				cep := &ccev2.CCEEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "test-ep",
					},
					Spec: ccev2.EndpointSpec{
						Network: ccev2.EndpointNetworkSpec{
							IPAllocation: &ccev2.IPAllocation{
								DirectIPAllocation: ccev2.DirectIPAllocation{
									Type:            ccev2.IPAllocTypeElastic,
									ReleaseStrategy: ccev2.ReleaseStrategyTTL,
								},
							},
						},
					},
					Status: ccev2.EndpointStatus{
						Networking: &ccev2.EndpointNetworking{
							IPs: "10.0.0.1",
						},
					},
				}
				mockUpdater.On("Update", cep).Return(cep, nil)
			},
			cep: &ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test-ep",
				},
				Spec: ccev2.EndpointSpec{
					Network: ccev2.EndpointNetworkSpec{
						IPAllocation: &ccev2.IPAllocation{
							DirectIPAllocation: ccev2.DirectIPAllocation{
								Type:            ccev2.IPAllocTypeElastic,
								ReleaseStrategy: ccev2.ReleaseStrategyTTL,
							},
						},
					},
				},
			},
			expectedError:  false,
			expectedResult: &ccev2.CCEEndpoint{},
		},
		{
			name: "failed IP allocation",
			setupMocks: func(mockUpdater *MockCCEEndpointGetterUpdater, mockLister *MockCCEEndpointLister, mockAllocator *MockDirectIPAllocator) {
				cep := &ccev2.CCEEndpoint{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: "test-ns",
						Name:      "test-ep",
					},
					Spec: ccev2.EndpointSpec{
						Network: ccev2.EndpointNetworkSpec{
							IPAllocation: &ccev2.IPAllocation{
								DirectIPAllocation: ccev2.DirectIPAllocation{
									Type:            ccev2.IPAllocTypeElastic,
									ReleaseStrategy: ccev2.ReleaseStrategyTTL,
								},
							},
						},
					},
				}
				mockUpdater.On("Update", cep).Return(nil, assert.AnError)
			},
			cep: &ccev2.CCEEndpoint{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test-ep",
				},
				Spec: ccev2.EndpointSpec{
					Network: ccev2.EndpointNetworkSpec{
						IPAllocation: &ccev2.IPAllocation{
							DirectIPAllocation: ccev2.DirectIPAllocation{
								Type:            ccev2.IPAllocTypeElastic,
								ReleaseStrategy: ccev2.ReleaseStrategyTTL,
							},
						},
					},
				},
			},
			expectedError:  true,
			expectedResult: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockUpdater := new(MockCCEEndpointGetterUpdater)
			mockLister := new(MockCCEEndpointLister)
			mockAllocator := new(MockDirectIPAllocator)
			tt.setupMocks(mockUpdater, mockLister, mockAllocator)

			manager := &EndpointManager{
				k8sAPI:                 mockUpdater,
				syncedPool:             sync.Map{},
				fixedIPPoolEndpointMap: make(map[string]map[string]*ccev2.CCEEndpoint),
				localPool:              newLocalPool(),
				remoteIPPool:           make(map[string]ipamTypes.AllocationMap),
				directIPAllocator:      mockAllocator,
			}

			err := manager.Update(tt.cep)
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			mockUpdater.AssertExpectations(t)
			mockLister.AssertExpectations(t)
			mockAllocator.AssertExpectations(t)
		})
	}
}

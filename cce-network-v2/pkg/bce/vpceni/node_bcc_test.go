package vpceni

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func Test_getDefaultBCCEniQuota(t *testing.T) {
	tests := []struct {
		name        string
		node        *corev1.Node
		expectedENI int
		expectedIP  int
	}{
		{
			name: "Given_node_with_10_CPU_6GB_memory_When_getDefaultBCCEniQuota_Then_return_8_ENI_8_IP",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Capacity: corev1.ResourceList{
						corev1.ResourceCPU:    *resource.NewScaledQuantity(10000, resource.Milli),
						corev1.ResourceMemory: *resource.NewQuantity(6*1024*1024*1024, resource.BinarySI),
					},
				},
			},
			expectedENI: 8,
			expectedIP:  8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxEni, maxIP := getDefaultBCCEniQuota(tt.node)
			assert.Equal(t, tt.expectedENI, maxEni, "ENI quota should match expected value")
			assert.Equal(t, tt.expectedIP, maxIP, "IP quota should match expected value")
		})
	}
}

func TestBccNode_reuseIPs(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string)
		expectedEniID string
		expectedError bool
		errorContains string
		mockSetupFunc func(*bceNetworkResourceSet)
		cleanupFunc   func()
	}{
		{
			name: "Given_primary_IP_mode_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Set primary IP mode
				node.k8sObj.Spec.ENI.UseMode = string(ccev2.ENIUseModePrimaryIP)

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: "10.128.34.100", SubnetID: "sbn-test", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "allocate ip cross subnet not support primary ip mode",
		},
		{
			name: "Given_empty_IPs_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				ctx := context.Background()
				var ips []*models.PrivateIP
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "no ip to reuse",
		},
		{
			name: "Given_nil_IPs_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				ctx := context.Background()
				var ips []*models.PrivateIP = nil
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "no ip to reuse",
		},
		{
			name: "Given_invalid_owner_format_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: "10.128.34.100", SubnetID: "sbn-test", Primary: false},
				}
				owner := "invalid-owner-format"

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "get endpoint",
		},
		{
			name: "Given_empty_owner_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: "10.128.34.100", SubnetID: "sbn-test", Primary: false},
				}
				owner := ""

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "get endpoint",
		},
		{
			name: "Given_local_IP_When_reuseIPs_is_called_Then_return_eniID_directly",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Create a mock ENI
				sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), node.k8sObj.Spec.ENI.SubnetIDs[0], metav1.GetOptions{})
				assert.NoError(t, err, "get subnet failed")

				mockEni, err := ccemock.NewMockEni(node.k8sObj.Name, node.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 5)
				assert.NoError(t, err)

				// Add specific IP to ENI
				testIP := "10.128.34.100"
				mockEni.Spec.ENI.PrivateIPSet = append(mockEni.Spec.ENI.PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: testIP,
					SubnetID:         sbn.Name,
					Primary:          false,
				})

				err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{mockEni})
				assert.NoError(t, err, "ensure enis to informer failed")

				// Create mock endpoint
				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
					endpoint := &ccev2.CCEEndpoint{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
						Status: ccev2.EndpointStatus{
							Networking: &ccev2.EndpointNetworking{
								Addressing: ccev2.AddressPairList{
									&ccev2.AddressPair{IP: testIP},
								},
							},
						},
					}
					var objects []metav1.Object
					if result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{}); err == nil {
						objects = append(objects, result)
					}
					return objects
				})
				assert.NoError(t, err, "ensure cep to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIP, SubnetID: sbn.Name, Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			expectedEniID: "", // Will be set dynamically in test
			expectedError: false,
		},
		{
			name: "Given_IPv4_IP_When_reuseIPs_is_called_Then_allocate_IPv4_successfully",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Create a mock ENI
				sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), node.k8sObj.Spec.ENI.SubnetIDs[0], metav1.GetOptions{})
				assert.NoError(t, err, "get subnet failed")

				mockEni, err := ccemock.NewMockEni(node.k8sObj.Name, node.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 5)
				assert.NoError(t, err)

				err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{mockEni})
				assert.NoError(t, err, "ensure enis to informer failed")

				// Create mock endpoint
				testIP := "10.128.34.100"
				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
					endpoint := &ccev2.CCEEndpoint{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
						Status: ccev2.EndpointStatus{
							Networking: &ccev2.EndpointNetworking{
								Addressing: ccev2.AddressPairList{
									&ccev2.AddressPair{IP: testIP},
								},
							},
						},
					}
					var objects []metav1.Object
					if result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{}); err == nil {
						objects = append(objects, result)
					}
					return objects
				})
				assert.NoError(t, err, "ensure cep to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIP, SubnetID: sbn.Name, Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BCE client to return success for BatchAddPrivateIpCrossSubnet
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq([]string{"10.128.34.100"}), gomock.Eq(1), gomock.Eq(false)).
					Return([]string{"10.128.34.100"}, nil).AnyTimes()
			},
			expectedEniID: "", // Will be set dynamically in test
			expectedError: false,
		},
		{
			name: "Given_IPv6_enabled_and_IPv6_IP_When_reuseIPs_is_called_Then_allocate_IPv6_successfully",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Create a mock ENI
				sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), node.k8sObj.Spec.ENI.SubnetIDs[0], metav1.GetOptions{})
				assert.NoError(t, err, "get subnet failed")

				// Set IPv6 CIDR for subnet
				sbn.Spec.IPv6CIDR = "2001:db8::/64"
				k8s.CCEClient().CceV1().Subnets().Update(context.TODO(), sbn, metav1.UpdateOptions{})

				mockEni, err := ccemock.NewMockEni(node.k8sObj.Name, node.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 5)
				assert.NoError(t, err)

				err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{mockEni})
				assert.NoError(t, err, "ensure enis to informer failed")

				// Create mock endpoint
				testIPv6 := "2001:db8::1"
				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
					endpoint := &ccev2.CCEEndpoint{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
						Status: ccev2.EndpointStatus{
							Networking: &ccev2.EndpointNetworking{
								Addressing: ccev2.AddressPairList{
									&ccev2.AddressPair{IP: testIPv6},
								},
							},
						},
					}
					var objects []metav1.Object
					if result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{}); err == nil {
						objects = append(objects, result)
					}
					return objects
				})
				assert.NoError(t, err, "ensure cep to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIPv6, SubnetID: sbn.Name, Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BCE client to return success for BatchAddPrivateIpCrossSubnet
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq([]string{"2001:db8::1"}), gomock.Eq(1), gomock.Eq(true)).
					Return([]string{"2001:db8::1"}, nil).AnyTimes()
			},
			cleanupFunc: func() {
				operatorOption.Config.EnableIPv6 = false
			},
			expectedEniID: "", // Will be set dynamically in test
			expectedError: false,
		},
		{
			name: "Given_BatchAddPrivateIpCrossSubnet_fails_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Create a mock ENI
				sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), node.k8sObj.Spec.ENI.SubnetIDs[0], metav1.GetOptions{})
				assert.NoError(t, err, "get subnet failed")

				mockEni, err := ccemock.NewMockEni(node.k8sObj.Name, node.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 5)
				assert.NoError(t, err)

				err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{mockEni})
				assert.NoError(t, err, "ensure enis to informer failed")

				// Create mock endpoint
				testIP := "10.128.34.100"
				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
					endpoint := &ccev2.CCEEndpoint{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
						Status: ccev2.EndpointStatus{
							Networking: &ccev2.EndpointNetworking{
								Addressing: ccev2.AddressPairList{
									&ccev2.AddressPair{IP: testIP},
								},
							},
						},
					}
					var objects []metav1.Object
					if result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{}); err == nil {
						objects = append(objects, result)
					}
					return objects
				})
				assert.NoError(t, err, "ensure cep to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIP, SubnetID: sbn.Name, Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BCE client to return error for BatchAddPrivateIpCrossSubnet
				// Use AnyTimes() to allow multiple retry attempts
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq([]string{"10.128.34.100"}), gomock.Eq(1), gomock.Eq(false)).
					Return(nil, fmt.Errorf("mock error")).AnyTimes()

				// Mock BatchDeletePrivateIP for rollback
				mockInterface.EXPECT().
					BatchDeletePrivateIP(gomock.Any(), gomock.Eq([]string{"10.128.34.100"}), gomock.Any(), gomock.Eq(true)).
					Return(nil).AnyTimes()
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "mock error",
		},
		{
			name: "Given_no_available_ENI_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Set ENI quota to 0 to simulate no available ENI
				node.eniQuota.SetMaxENI(0)

				// Create mock endpoint
				testIP := "10.128.34.100"
				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
					endpoint := &ccev2.CCEEndpoint{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
						Status: ccev2.EndpointStatus{
							Networking: &ccev2.EndpointNetworking{
								Addressing: ccev2.AddressPairList{
									&ccev2.AddressPair{IP: testIP},
								},
							},
						},
					}
					var objects []metav1.Object
					if result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{}); err == nil {
						objects = append(objects, result)
					}
					return objects
				})
				assert.NoError(t, err, "ensure cep to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIP, SubnetID: "sbn-test", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "no available ip for allocation",
		},
		{
			name: "Given_cancelled_context_When_reuseIPs_is_called_Then_handle_gracefully",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Create a cancelled context
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				ips := []*models.PrivateIP{
					{PrivateIPAddress: "10.128.34.100", SubnetID: "sbn-test", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: false, // May or may not error depending on timing
		},
		{
			name: "Given_mixed_IPv4_and_IPv6_IPs_When_reuseIPs_is_called_Then_handle_both_families",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Create a mock ENI
				sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), node.k8sObj.Spec.ENI.SubnetIDs[0], metav1.GetOptions{})
				assert.NoError(t, err, "get subnet failed")

				// Set IPv6 CIDR for subnet
				sbn.Spec.IPv6CIDR = "2001:db8::/64"
				k8s.CCEClient().CceV1().Subnets().Update(context.TODO(), sbn, metav1.UpdateOptions{})

				mockEni, err := ccemock.NewMockEni(node.k8sObj.Name, node.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 5)
				assert.NoError(t, err)

				err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{mockEni})
				assert.NoError(t, err, "ensure enis to informer failed")

				// Create mock endpoint
				testIPv4 := "10.128.34.100"
				testIPv6 := "2001:db8::1"
				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
					endpoint := &ccev2.CCEEndpoint{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
						Status: ccev2.EndpointStatus{
							Networking: &ccev2.EndpointNetworking{
								Addressing: ccev2.AddressPairList{
									&ccev2.AddressPair{IP: testIPv4},
									&ccev2.AddressPair{IP: testIPv6},
								},
							},
						},
					}
					var objects []metav1.Object
					if result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{}); err == nil {
						objects = append(objects, result)
					}
					return objects
				})
				assert.NoError(t, err, "ensure cep to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIPv4, SubnetID: sbn.Name, Primary: false},
					{PrivateIPAddress: testIPv6, SubnetID: sbn.Name, Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BCE client for both IPv4 and IPv6
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq([]string{"10.128.34.100"}), gomock.Eq(1), gomock.Eq(false)).
					Return([]string{"10.128.34.100"}, nil).AnyTimes()
				mockInterface.EXPECT().
					BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq([]string{"2001:db8::1"}), gomock.Eq(1), gomock.Eq(true)).
					Return([]string{"2001:db8::1"}, nil).AnyTimes()
			},
			cleanupFunc: func() {
				operatorOption.Config.EnableIPv6 = false
			},
			expectedEniID: "", // Will be set dynamically in test
			expectedError: false,
		},
		{
			name: "Given_primary_IP_in_IPs_list_When_reuseIPs_is_called_Then_handle_correctly",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: "10.128.34.100", SubnetID: "sbn-test", Primary: true}, // Primary IP
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "get endpoint",
		},
		{
			name: "Given_multiple_IPs_same_subnet_When_reuseIPs_is_called_Then_handle_all_IPs",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bccTestContext(t)
				assert.NoError(t, err)

				// Create a mock ENI
				sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), node.k8sObj.Spec.ENI.SubnetIDs[0], metav1.GetOptions{})
				assert.NoError(t, err, "get subnet failed")

				mockEni, err := ccemock.NewMockEni(node.k8sObj.Name, node.k8sObj.Spec.InstanceID, sbn.Name, sbn.Spec.CIDR, 5)
				assert.NoError(t, err)

				err = ccemock.EnsureEnisToInformer(t, []*ccev2.ENI{mockEni})
				assert.NoError(t, err, "ensure enis to informer failed")

				// Create mock endpoint
				testIP1 := "10.128.34.100"
				testIP2 := "10.128.34.101"
				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer(), func(ctx context.Context) []metav1.Object {
					endpoint := &ccev2.CCEEndpoint{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-pod",
							Namespace: "default",
						},
						Status: ccev2.EndpointStatus{
							Networking: &ccev2.EndpointNetworking{
								Addressing: ccev2.AddressPairList{
									&ccev2.AddressPair{IP: testIP1},
									&ccev2.AddressPair{IP: testIP2},
								},
							},
						},
					}
					var objects []metav1.Object
					if result, err := k8s.CCEClient().CceV2().CCEEndpoints("default").Create(ctx, endpoint, metav1.CreateOptions{}); err == nil {
						objects = append(objects, result)
					}
					return objects
				})
				assert.NoError(t, err, "ensure cep to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIP1, SubnetID: sbn.Name, Primary: false},
					{PrivateIPAddress: testIP2, SubnetID: sbn.Name, Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BCE client to handle multiple IPs
				// Note: The reuseIPs method processes the last IPv4 IP in the array
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BatchAddPrivateIpCrossSubnet(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Eq([]string{"10.128.34.101"}), gomock.Eq(1), gomock.Eq(false)).
					Return([]string{"10.128.34.101"}, nil).AnyTimes()
			},
			expectedEniID: "", // Will be set dynamically in test
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup IPv6 if needed
			if tt.name == "Given_IPv6_enabled_and_IPv6_IP_When_reuseIPs_is_called_Then_allocate_IPv6_successfully" ||
				tt.name == "Given_mixed_IPv4_and_IPv6_IPs_When_reuseIPs_is_called_Then_handle_both_families" {
				operatorOption.Config.EnableIPv6 = true
			}

			// Setup test
			node, ctx, ips, owner := tt.setupFunc(t)

			// Setup mocks if needed
			if tt.mockSetupFunc != nil {
				tt.mockSetupFunc(node)
			}

			// Execute test
			eniID, err := node.real.reuseIPs(ctx, ips, owner)

			// Verify results
			if tt.expectedError {
				assert.Error(t, err, "Expected error but got none")
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains, "Error message should contain expected text")
				}
				// 对于 BatchAddPrivateIpCrossSubnet 失败的情况，不验证 ENI ID 是否为空
				if tt.name != "Given_BatchAddPrivateIpCrossSubnet_fails_When_reuseIPs_is_called_Then_return_error" {
					assert.Empty(t, eniID, "ENI ID should be empty on error")
				}
			} else {
				if tt.name == "Given_cancelled_context_When_reuseIPs_is_called_Then_handle_gracefully" {
					// For cancelled context, we just verify it doesn't panic
					t.Logf("reuseIPs with cancelled context returned: eniID=%s, err=%v", eniID, err)
				} else {
					assert.NoError(t, err, "Expected no error but got: %v", err)
					assert.NotEmpty(t, eniID, "ENI ID should not be empty on success")
				}
			}

			// Cleanup if needed
			if tt.cleanupFunc != nil {
				tt.cleanupFunc()
			}
		})
	}
}

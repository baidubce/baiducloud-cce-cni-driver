package vpceni

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sutilnet "k8s.io/utils/net"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/bcesync"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	ipamTypes "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/mock/ccemock"
	"github.com/baidubce/bce-sdk-go/services/bbc"
)

// Test_bbcNode_prepareIPAllocation 测试bbcNode的prepareIPAllocation方法，包括使用主网卡和指定子网的场景。
// 该函数会在两个测试用例中运行：
// - 使用主网卡的场景，应该返回可用接口、可分配IP地址和接口ID等信息；
// - 使用用户指定子网的场景，应该根据用户指定的子网更新节点的可用子网列表，并返回可用接口、可分配IP地址和接口ID等信息。
func Test_bbcNode_prepareIPAllocation(t *testing.T) {
	t.Run("use primary subnet of bbc eni", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}
		operatorOption.Config.EnableNodeAnnotationSync = false

		_, err = k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}

		logfield := logging.DefaultLogger.WithField("component", "test").WithField("testcase", "use primary subnet of bbc eni")
		allocation, err := node.PrepareIPAllocation(logfield)
		if assert.NoError(t, err) {
			assert.Equalf(t, 0, allocation.AvailableInterfaces, "should have available interfaces")
			assert.Equalf(t, 19, allocation.AvailableForAllocationIPv4, "should have available ips")
			assert.Equalf(t, "eni-bbcprimary", allocation.InterfaceID, "should have available interface")
		}
	})

	t.Run("use user specifical subnet of bbc eni", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		// wait eni to be created
		eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}
		ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
			return []metav1.Object{
				eni,
			}
		})

		// update annotation to nrs
		operatorOption.Config.EnableNodeAnnotationSync = true
		defer func() {
			operatorOption.Config.EnableNodeAnnotationSync = false
		}()
		node.k8sObj.Annotations[k8s.AnnotationNodeAnnotationSynced] = "true"
		node.k8sObj.Annotations[k8s.AnnotationNodeEniSubnetIDs] = "sbn-vxda1,sbn-vxda2"
		k8s.CCEClient().CceV2().NetResourceSets().Update(context.TODO(), node.k8sObj, metav1.UpdateOptions{})
		wait.PollImmediate(time.Microsecond, 5*time.Second, func() (bool, error) {
			obj, err := node.manager.nrsGetterUpdater.Get(node.k8sObj.Name)
			if err != nil {
				return false, err
			}
			if len(obj.Annotations) > 0 && obj.Annotations[k8s.AnnotationNodeAnnotationSynced] == "true" {
				return true, nil
			}
			return false, nil
		})

		exceptSubnetIDs := []string{"sbn-vxda1", "sbn-vxda2"}
		ccemock.EnsureSubnetIDsToInformer(t, node.k8sObj.Spec.ENI.VpcID, exceptSubnetIDs)

		// Refresh available subnets to initialize n.availableSubnets
		err = node.refreshAvailableSubnets()
		if !assert.NoError(t, err) {
			return
		}
		logfield := logging.DefaultLogger.WithField("component", "test").WithField("testcase", "use user specifical subnet of bbc eni")
		allocation, err := node.PrepareIPAllocation(logfield)
		if assert.NoError(t, err) {
			assert.Equalf(t, 0, allocation.AvailableInterfaces, "should have available interfaces")
			assert.Equalf(t, 19, allocation.AvailableForAllocationIPv4, "should have available ips")
			assert.Equalf(t, "eni-bbcprimary", allocation.InterfaceID, "should have available interface")

			// 验证子网配置是否正确更新
			assert.Equalf(t, exceptSubnetIDs, node.k8sObj.Spec.ENI.SubnetIDs, "should subnet ids equal user specified subnet")
		}
	})
}

// Test_bbcNode_allocateIPs 测试bbcNode_allocateIPs函数，分别针对主网络和跨子网的情况进行测试。
// 该函数会调用mock接口来模拟云服务器的IP分配操作，并根据测试结果判定是否分配成功。
// 如果测试通过，则返回nil；如果测试失败，则返回错误信息。
func Test_bbcNode_allocateIPs(t *testing.T) {
	t.Run("allocateIP from primary subnet", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}
		operatorOption.Config.EnableNodeAnnotationSync = false

		// wait eni to be created
		eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}
		ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
			return []metav1.Object{
				eni,
			}
		})
		if !assert.Equalf(t, 21, len(eni.Spec.PrivateIPSet), "should have 21 ips") {
			return
		}

		sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), eni.Spec.SubnetID, metav1.GetOptions{})
		if !assert.NoErrorf(t, err, "failed to get subnet") {
			return
		}

		// mock bce openapi to allocate ip from bbc primary subnet
		mockInterface := node.manager.GetMockCloudInterface()
		first := mockInterface.EXPECT().
			BBCBatchAddIP(gomock.Any(), gomock.Eq(&bbc.BatchAddIpArgs{
				InstanceId:                     node.instanceID,
				SecondaryPrivateIpAddressCount: 10,
			})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 23, 10)}, nil).Times(1)
		second := mockInterface.EXPECT().BBCBatchAddIP(gomock.Any(), gomock.Eq(&bbc.BatchAddIpArgs{
			InstanceId:                     node.instanceID,
			SecondaryPrivateIpAddressCount: 1,
		})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 33, 1)}, nil).Times(1)
		gomock.InOrder(first, second)

		ctx := context.TODO()
		allocation := &ipam.AllocationAction{
			InterfaceID:                eni.Name,
			AvailableForAllocationIPv4: 11,
			PoolID:                     ipamTypes.PoolID(eni.Spec.SubnetID),
		}
		err = node.AllocateIPs(ctx, allocation)
		if assert.NoErrorf(t, err, "allocate ips failed") {
			assert.Equalf(t, 11, allocation.AvailableForAllocationIPv4, "should have available ips")
			eni, err = k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), eni.Name, metav1.GetOptions{})
			if !assert.NoError(t, err) {
				return
			}
			assert.Equalf(t, 32, len(eni.Spec.PrivateIPSet), "should have 32 secondary IPs")
			for i := range eni.Spec.PrivateIPSet {
				assert.Equalf(t, eni.Spec.PrivateIPSet[i].SubnetID, sbn.Name, "%s should use the primary subnet id", eni.Spec.PrivateIPSet[i].PrivateIPAddress)
			}
		}
	})

	t.Run("allocateIP cross subnet", func(t *testing.T) {
		node, err := bbcTestContext(t)
		if !assert.NoError(t, err) {
			return
		}

		// wait eni to be created
		eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
		if !assert.NoError(t, err) {
			return
		}
		ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
			return []metav1.Object{
				eni,
			}
		})

		// update annotation to nrs
		operatorOption.Config.EnableNodeAnnotationSync = true
		defer func() {
			operatorOption.Config.EnableNodeAnnotationSync = false
		}()
		node.k8sObj.Annotations[k8s.AnnotationNodeAnnotationSynced] = "true"
		node.k8sObj.Annotations[k8s.AnnotationNodeEniSubnetIDs] = "sbn-vxda1,sbn-vxda2"
		k8s.CCEClient().CceV2().NetResourceSets().Update(context.TODO(), node.k8sObj, metav1.UpdateOptions{})
		wait.PollImmediate(time.Microsecond, 5*time.Second, func() (bool, error) {
			obj, err := node.manager.nrsGetterUpdater.Get(node.k8sObj.Name)
			if err != nil {
				return false, err
			}
			if len(obj.Annotations) > 0 && obj.Annotations[k8s.AnnotationNodeAnnotationSynced] == "true" {
				return true, nil
			}
			return false, nil
		})

		exceptSubnetIDs := []string{"sbn-vxda1", "sbn-vxda2"}
		ccemock.EnsureSubnetIDsToInformer(t, node.k8sObj.Spec.ENI.VpcID, exceptSubnetIDs)
		sbn, err := k8s.CCEClient().CceV1().Subnets().Get(context.TODO(), exceptSubnetIDs[0], metav1.GetOptions{})
		if !assert.NoErrorf(t, err, "failed to get subnet") {
			return
		}

		// Refresh available subnets to initialize node.availableSubnets
		err = node.refreshAvailableSubnets()
		if !assert.NoError(t, err) {
			return
		}

		// mock bce openapi to allocate ip from bbc primary subnet
		mockInterface := node.manager.GetMockCloudInterface()
		first := mockInterface.EXPECT().
			BBCBatchAddIPCrossSubnet(gomock.Any(), gomock.Eq(&bbc.BatchAddIpCrossSubnetArgs{
				InstanceId: node.instanceID,
				SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
					{
						EniId:                          eni.Name,
						SubnetId:                       exceptSubnetIDs[0],
						SecondaryPrivateIpAddressCount: 10,
					},
				},
			})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 23, 10)}, nil).Times(1)
		second := mockInterface.EXPECT().BBCBatchAddIPCrossSubnet(gomock.Any(), gomock.Eq(&bbc.BatchAddIpCrossSubnetArgs{
			InstanceId: node.instanceID,
			SingleEniAndSubentIps: []bbc.SingleEniAndSubentIp{
				{
					EniId:                          eni.Name,
					SubnetId:                       exceptSubnetIDs[0],
					SecondaryPrivateIpAddressCount: 1,
				},
			},
		})).Return(&bbc.BatchAddIpResponse{PrivateIps: allocateIPsFromSubnet(sbn, 33, 1)}, nil).Times(1)
		gomock.InOrder(first, second)

		ctx := context.TODO()
		allocation := &ipam.AllocationAction{
			InterfaceID:                eni.Name,
			AvailableForAllocationIPv4: 11,
			PoolID:                     ipamTypes.PoolID(exceptSubnetIDs[0]),
		}
		err = node.AllocateIPs(ctx, allocation)
		if assert.NoErrorf(t, err, "allocate ips failed") {
			assert.Equalf(t, 11, allocation.AvailableForAllocationIPv4, "should have available ips")
			eni, err = k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), eni.Name, metav1.GetOptions{})
			if !assert.NoError(t, err) {
				return
			}
			assert.Equalf(t, 32, len(eni.Spec.PrivateIPSet), "should have 32 secondary IPs")
			for i := 21; i < len(eni.Spec.PrivateIPSet); i++ {
				assert.Equalf(t, exceptSubnetIDs[0], eni.Spec.PrivateIPSet[i].SubnetID, "%s should use the primary subnet id", eni.Spec.PrivateIPSet[i].PrivateIPAddress)
			}
		}
	})
}

func Test_bbcNetworkResourceSet_reuseIPs(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string)
		mockSetupFunc func(node *bceNetworkResourceSet)
		cleanupFunc   func()
		expectedEniID string
		expectedError bool
		errorContains string
	}{
		{
			name: "TestReuseIPs_Given_BBC_ENI_not_ready_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				// 创建一个特殊的测试上下文，不包含 ENI 或 ENI 没有正确的标签
				ccemock.InitMockEnv()
				mockCtl := gomock.NewController(t)
				im := newMockInstancesManager(mockCtl)

				k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "bbc")
				err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj})
				assert.NoError(t, err, "ensure nrs to informer failed")

				k8sNode := ccemock.NewMockNodeFromNrs(k8sObj)
				err = ccemock.EnsureNodeToInformer(t, []*corev1.Node{k8sNode})
				assert.NoError(t, err, "ensure node to informer failed")

				sbn := ccemock.NewMockSubnet("sbn-bbcprimary", "10.128.34.0/24")
				err = ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn})
				assert.NoError(t, err, "ensure subnet to informer failed")
				ccemock.EnsureSubnetIDsToInformer(t, k8sObj.Spec.ENI.VpcID, k8sObj.Spec.ENI.SubnetIDs)

				// 不创建 ENI，这样 tryRefreshBBCENI 就会返回 nil

				// 创建 mock endpoint
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
									&ccev2.AddressPair{IP: "10.128.34.99"}, // 不同的 IP
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

				bcesync.InitBSM()
				node := NewBCENetworkResourceSet(nil, k8sObj, im)
				assert.NotNil(t, node)

				node.eniQuota = newCustomerIPQuota(log, k8s.Client(), k8sObj.Name, k8sObj.Spec.InstanceID, im.bceclientd)
				node.eniQuota.SetMaxENI(1)
				node.eniQuota.SetMaxIP(40)

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: testIP, SubnetID: "sbn-bbcprimary", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// 不需要额外的 mock 设置，因为没有 ENI
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "bbc eni",
		},
		{
			name: "TestReuseIPs_Given_invalid_owner_format_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bbcTestContext(t)
				assert.NoError(t, err)

				// 创建 BBC ENI
				eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
				assert.NoError(t, err)

				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
					return []metav1.Object{eni}
				})
				assert.NoError(t, err, "ensure enis to informer failed")

				ctx := context.Background()
				ips := []*models.PrivateIP{
					{PrivateIPAddress: "10.128.34.100", SubnetID: "sbn-bbcprimary", Primary: false},
				}
				owner := "invalid-owner-format"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// No additional mocks needed for this test
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "get endpoint",
		},
		{
			name: "TestReuseIPs_Given_local_IP_When_reuseIPs_is_called_Then_return_success",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bbcTestContext(t)
				assert.NoError(t, err)

				// 创建 BBC ENI
				eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
				assert.NoError(t, err)

				// 添加要测试的 IP 到 ENI 中
				testIP := "10.128.34.100"
				eni.Spec.ENI.PrivateIPSet = append(eni.Spec.ENI.PrivateIPSet, &models.PrivateIP{
					PrivateIPAddress: testIP,
					Primary:          false,
					SubnetID:         "sbn-bbcprimary",
				})

				// 更新 ENI
				_, err = k8s.CCEClient().CceV2().ENIs().Update(context.TODO(), eni, metav1.UpdateOptions{})
				assert.NoError(t, err)

				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
					return []metav1.Object{eni}
				})
				assert.NoError(t, err, "ensure enis to informer failed")

				// 创建 mock endpoint，包含要重用的 IP（本地 IP）
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
					{PrivateIPAddress: testIP, SubnetID: "sbn-bbcprimary", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// 本地 IP 不需要调用 BBC API，所以不设置 mock
			},
			expectedEniID: "eni-bbcprimary",
			expectedError: false,
		},
		{
			name: "TestReuseIPs_Given_valid_IP_When_reuseIPs_is_called_Then_return_success",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bbcTestContext(t)
				assert.NoError(t, err)

				// 创建 BBC ENI
				eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
				assert.NoError(t, err)

				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
					return []metav1.Object{eni}
				})
				assert.NoError(t, err, "ensure enis to informer failed")

				// 创建 mock endpoint，但不包含要重用的 IP
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
									&ccev2.AddressPair{IP: "10.128.34.99"}, // 不同的 IP
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
					{PrivateIPAddress: testIP, SubnetID: "sbn-bbcprimary", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BBC client to return success
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BBCBatchAddIPCrossSubnet(gomock.Any(), gomock.Any()).
					Return(&bbc.BatchAddIpResponse{PrivateIps: []string{"10.128.34.100"}}, nil).Times(1)
			},
			expectedEniID: "eni-bbcprimary",
			expectedError: false,
		},
		{
			name: "TestReuseIPs_Given_BBC_API_error_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bbcTestContext(t)
				assert.NoError(t, err)

				// 创建 BBC ENI
				eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
				assert.NoError(t, err)

				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
					return []metav1.Object{eni}
				})
				assert.NoError(t, err, "ensure enis to informer failed")

				// 创建 mock endpoint，但不包含要重用的 IP
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
									&ccev2.AddressPair{IP: "10.128.34.99"}, // 不同的 IP
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
					{PrivateIPAddress: testIP, SubnetID: "sbn-bbcprimary", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BBC client to return error for BBCBatchAddIPCrossSubnet
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BBCBatchAddIPCrossSubnet(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("BBC API error")).Times(1)
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "BBC API error",
		},
		{
			name: "TestReuseIPs_Given_empty_response_When_reuseIPs_is_called_Then_return_error",
			setupFunc: func(t *testing.T) (*bceNetworkResourceSet, context.Context, []*models.PrivateIP, string) {
				node, err := bbcTestContext(t)
				assert.NoError(t, err)

				// 创建 BBC ENI
				eni, err := k8s.CCEClient().CceV2().ENIs().Get(context.TODO(), "eni-bbcprimary", metav1.GetOptions{})
				assert.NoError(t, err)

				err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
					return []metav1.Object{eni}
				})
				assert.NoError(t, err, "ensure enis to informer failed")

				// 创建 mock endpoint，但不包含要重用的 IP
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
									&ccev2.AddressPair{IP: "10.128.34.99"}, // 不同的 IP
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
					{PrivateIPAddress: testIP, SubnetID: "sbn-bbcprimary", Primary: false},
				}
				owner := "default/test-pod"

				return node, ctx, ips, owner
			},
			mockSetupFunc: func(node *bceNetworkResourceSet) {
				// Mock BBC client to return empty response
				mockInterface := node.manager.GetMockCloudInterface()
				mockInterface.EXPECT().
					BBCBatchAddIPCrossSubnet(gomock.Any(), gomock.Any()).
					Return(&bbc.BatchAddIpResponse{PrivateIps: []string{}}, nil).Times(1)
			},
			expectedEniID: "",
			expectedError: true,
			errorContains: "failed to reuse ip cross subnet without any error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 设置测试环境
			node, ctx, ips, owner := tt.setupFunc(t)

			// 设置 mock
			if tt.mockSetupFunc != nil {
				tt.mockSetupFunc(node)
			}

			// 执行测试
			eniID, err := node.real.(*bbcNetworkResourceSet).reuseIPs(ctx, ips, owner)

			// 验证结果
			if tt.expectedError {
				if err == nil {
					t.Errorf("期望有错误但没有错误，测试用例: %s", tt.name)
				} else if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("错误信息不包含预期内容，期望包含: %s, 实际错误: %v", tt.errorContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("不期望有错误但有错误，测试用例: %s, 错误: %v", tt.name, err)
				}
			}

			if tt.expectedEniID != "" && eniID != tt.expectedEniID {
				t.Errorf("ENI ID 不匹配，期望: %s, 实际: %s", tt.expectedEniID, eniID)
			}

			// 清理
			if tt.cleanupFunc != nil {
				tt.cleanupFunc()
			}

			t.Logf("测试 %s 完成: eniID=%s, err=%v", tt.name, eniID, err)
		})
	}
}

// 准备 BCC 测试上下文环境
// 包含初始化 mock 对象，保存到 clientgo缓存中，并返回 BCCNode 实例
func bbcTestContext(t *testing.T) (*bceNetworkResourceSet, error) {
	ccemock.InitMockEnv()
	mockCtl := gomock.NewController(t)
	im := newMockInstancesManager(mockCtl)

	k8sObj := ccemock.NewMockSimpleNrs("10.128.34.56", "bbc")
	err := ccemock.EnsureNrsToInformer(t, []*ccev2.NetResourceSet{k8sObj})
	if !assert.NoError(t, err, "ensure nrs to informer failed") {
		return nil, err
	}

	k8sNode := ccemock.NewMockNodeFromNrs(k8sObj)
	err = ccemock.EnsureNodeToInformer(t, []*corev1.Node{k8sNode})
	if !assert.NoError(t, err, "ensure node to informer failed") {
		return nil, err
	}

	sbn := ccemock.NewMockSubnet("sbn-bbcprimary", "10.128.34.0/24")
	err = ccemock.EnsureSubnetsToInformer(t, []*ccev1.Subnet{sbn})
	if !assert.NoError(t, err, "ensure subnet to informer failed") {
		return nil, err
	}
	ccemock.EnsureSubnetIDsToInformer(t, k8sObj.Spec.ENI.VpcID, k8sObj.Spec.ENI.SubnetIDs)

	// 创建 BBC ENI 对象
	bbcEniResult := newMockBBCEniWithMultipleIPs(k8sObj, sbn)
	eni := &ccev2.ENI{
		ObjectMeta: metav1.ObjectMeta{
			Name: bbcEniResult.Id,
			Labels: map[string]string{
				k8s.LabelInstanceID: k8sObj.Spec.InstanceID,
				k8s.LabelNodeName:   k8sObj.Name,
			},
		},
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				ID:           bbcEniResult.Id,
				Name:         bbcEniResult.Name,
				InstanceID:   k8sObj.Spec.InstanceID,
				VpcID:        bbcEniResult.VpcId,
				ZoneName:     bbcEniResult.ZoneName,
				SubnetID:     bbcEniResult.SubnetId,
				MacAddress:   bbcEniResult.MacAddress,
				PrivateIPSet: []*models.PrivateIP{},
			},
			NodeName:                  k8sObj.Name,
			UseMode:                   ccev2.ENIUseModeSecondaryIP,
			RouteTableOffset:          127,
			InstallSourceBasedRouting: true,
			Type:                      ccev2.ENIForBBC,
		},
		Status: ccev2.ENIStatus{
			CCEStatus: ccev2.ENIStatusReadyOnNode,
			VPCStatus: ccev2.VPCENIStatusInuse,
		},
	}

	// 添加 IP 地址到 ENI
	for _, ip := range bbcEniResult.PrivateIpSet {
		privateIP := &models.PrivateIP{
			PrivateIPAddress: ip.PrivateIpAddress,
			Primary:          ip.Primary,
			SubnetID:         ip.SubnetId,
		}
		eni.Spec.ENI.PrivateIPSet = append(eni.Spec.ENI.PrivateIPSet, privateIP)
	}

	// 将 ENI 添加到 informer
	err = ccemock.EnsureObjectToInformer(t, k8s.CCEClient().Informers.Cce().V2().ENIs().Informer(), func(ctx context.Context) []metav1.Object {
		result, err := k8s.CCEClient().CceV2().ENIs().Create(ctx, eni, metav1.CreateOptions{})
		if err != nil {
			return nil
		}
		return []metav1.Object{result}
	})
	if !assert.NoError(t, err, "ensure eni to informer failed") {
		return nil, err
	}

	im.GetMockCloudInterface().EXPECT().
		GetBBCInstanceENI(gomock.Any(), gomock.Eq(k8sObj.InstanceID())).Return(bbcEniResult, nil).AnyTimes()

	bcesync.InitBSM()
	node := NewBCENetworkResourceSet(nil, k8sObj, im)
	assert.NotNil(t, node)

	node.eniQuota = newCustomerIPQuota(log, k8s.Client(), k8sObj.Name, k8sObj.Spec.InstanceID, im.bceclientd)
	node.eniQuota.SetMaxENI(1)
	node.eniQuota.SetMaxIP(40)

	return node, nil
}

// newMockBBCEniWithMultipleIPs generates a mock BBC ENI instance with multiple IP addresses based on the given k8sObj and sbn.
//
// Parameters:
// k8sObj: a pointer to a ccev2.NetResourceSet, representing the Kubernetes network resource configuration object.
// sbn: a pointer to a ccev1.Subnet, representing the subnet configuration object.
//
// Returns:
// a pointer to a bbc.GetInstanceEniResult,
func newMockBBCEniWithMultipleIPs(k8sObj *ccev2.NetResourceSet, sbn *ccev1.Subnet) *bbc.GetInstanceEniResult {
	ips := allocateIPsFromSubnet(sbn, 2, 21)
	result := &bbc.GetInstanceEniResult{
		Id:         "eni-bbcprimary",
		Name:       "primary",
		ZoneName:   k8sObj.Spec.ENI.AvailabilityZone,
		VpcId:      k8sObj.Spec.ENI.VpcID,
		SubnetId:   sbn.Name,
		MacAddress: "02:16:3e:58:9a:4b",
		Status:     "inuse",
		PrivateIpSet: []bbc.PrivateIP{
			{
				PrivateIpAddress: ips[0],
				Primary:          true,
				SubnetId:         sbn.Name,
			},
		},
	}

	for i := 1; i < 21; i++ {
		result.PrivateIpSet = append(result.PrivateIpSet, bbc.PrivateIP{
			PrivateIpAddress: ips[i],
			Primary:          false,
			SubnetId:         sbn.Name,
		})
	}
	return result
}

// allocate count IPs from subnet
// start: 0-based index of subnet CIDR
func allocateIPsFromSubnet(sbn *ccev1.Subnet, start, count int) []string {
	var result []string
	sbnCIDR := cidr.MustParseCIDR(sbn.Spec.CIDR)
	baseIntIP := k8sutilnet.BigForIP(sbnCIDR.IP)

	for i := 0; i < count; i++ {
		result = append(result, k8sutilnet.AddIPOffset(baseIntIP, start+i).String())
	}
	return result
}

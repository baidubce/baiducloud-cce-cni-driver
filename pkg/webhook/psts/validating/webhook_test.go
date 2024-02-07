package validating

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"

	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/golang/mock/gomock"
	_ "github.com/golang/mock/mockgen/model"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sutilnet "k8s.io/utils/net"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func setupEnv(ctrl *gomock.Controller) (
	versioned.Interface,
	crdinformers.SharedInformerFactory,
	cloud.Interface,
) {
	crdClient := crdfake.NewSimpleClientset()
	crdInformer := crdinformers.NewSharedInformerFactory(crdClient, time.Minute)
	cloudClient := mockcloud.NewMockInterface(ctrl)

	return crdClient, crdInformer, cloudClient
}

func buildValidatingPSTSHandler(ctrl *gomock.Controller) *ValidatingPSTSHandler {
	crdClient, crdInformer, cloudClient := setupEnv(ctrl)
	handler := &ValidatingPSTSHandler{}
	handler.InjectCRDClient(crdClient)
	handler.InjectCloudClient(cloudClient)
	handler.InjectNetworkInformer(crdInformer)

	crdInformer.Start(wait.NeverStop)
	return handler
}

func TestValidatingPSTSHandler_validatePstsSpec(t *testing.T) {

	tests := []struct {
		name    string
		psts    func() *networkv1alpha1.PodSubnetTopologySpread
		preTest func(handler *ValidatingPSTSHandler)
		wantErr int
	}{
		{
			name: "right fixed ip mode",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-test")
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-aaaaa": {
						IPAllocationStrategy: networkv1alpha1.IPAllocationStrategy{
							Type:            networkv1alpha1.IPAllocTypeFixed,
							ReleaseStrategy: networkv1alpha1.ReleaseStrategyNever,
						},
						IPv4: []string{
							"10.178.245.1",
							"10.178.245.2",
							"10.178.245.3",
						},
					},
				}
				return obj
			},
		}, {
			name: "range in fixed ip mode",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-test")
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-aaaaa": {
						IPAllocationStrategy: networkv1alpha1.IPAllocationStrategy{
							Type:            networkv1alpha1.IPAllocTypeFixed,
							ReleaseStrategy: networkv1alpha1.ReleaseStrategyNever,
						},
						IPv4Range: []string{
							"10.178.245.1/25",
						},
					},
				}
				return obj
			},
		}, {
			name: "elastic ip mode",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-test")
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-aaaaa": {
						IPAllocationStrategy: networkv1alpha1.IPAllocationStrategy{
							Type:            networkv1alpha1.IPAllocTypeElastic,
							ReleaseStrategy: networkv1alpha1.ReleaseStrategyTTL,
						},
					},
				}
				return obj
			},
		}, {
			name: "cannot use fixedIP in elastic ip mode",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-test")
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-aaaaa": {
						IPAllocationStrategy: networkv1alpha1.IPAllocationStrategy{
							Type:            networkv1alpha1.IPAllocTypeElastic,
							ReleaseStrategy: networkv1alpha1.ReleaseStrategyTTL,
						},
						IPv4: []string{
							"10.178.245.1",
							"10.178.245.2",
							"10.178.245.3",
						},
					},
				}
				return obj
			},
			wantErr: 1,
		}, {
			name: "subnet is not exist",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-notexist")
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-notexist": {},
				}
				return obj
			},
			preTest: func(handler *ValidatingPSTSHandler) {},
			wantErr: 1,
		}, {
			name: "range in manual ip mode",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-test")
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-aaaaa": {
						IPAllocationStrategy: networkv1alpha1.IPAllocationStrategy{
							Type:            networkv1alpha1.IPAllocTypeManual,
							ReleaseStrategy: networkv1alpha1.ReleaseStrategyTTL,
						},

						IPv4Range: []string{
							"10.178.245.1/25",
						},
					},
				}
				return obj
			},
		}, {
			name: "range in custom ip mode",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-test")
				obj.Spec.Strategy = &networkv1alpha1.IPAllocationStrategy{
					Type:                 networkv1alpha1.IPAllocTypeCustom,
					ReleaseStrategy:      networkv1alpha1.ReleaseStrategyTTL,
					EnableReuseIPAddress: true,
					TTL:                  &metav1.Duration{Duration: time.Hour},
				}
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-aaaaa": {
						Custom: []networkv1alpha1.CustomAllocation{
							{Family: k8sutilnet.IPv4, CustomIPRange: []networkv1alpha1.CustomIPRange{{Start: "10.178.245.2", End: "10.178.245.5"}}},
						},
					},
				}
				return obj
			},
		},
		{
			name: "range not in custom ip mode",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-test")
				obj.Spec.Strategy = &networkv1alpha1.IPAllocationStrategy{
					Type:                 networkv1alpha1.IPAllocTypeCustom,
					ReleaseStrategy:      networkv1alpha1.ReleaseStrategyTTL,
					EnableReuseIPAddress: true,
					TTL:                  &metav1.Duration{Duration: time.Hour},
				}
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-aaaaa": {
						Custom: []networkv1alpha1.CustomAllocation{
							{Family: k8sutilnet.IPv4, CustomIPRange: []networkv1alpha1.CustomIPRange{{Start: "10.0.245.2", End: "10.178.245.5"}}},
						},
					},
				}
				return obj
			},
			wantErr: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctl := gomock.NewController(t)
			handler := buildValidatingPSTSHandler(ctl)
			// get mock interface, mock a subnet
			mockBCE := handler.bceClient.(*mockcloud.MockInterface)
			MockBCEObject(mockBCE)
			got := handler.validatePstsSpec(&tt.psts().Spec, field.NewPath("spec"))
			if len(got) != 0 {
				t.Logf("with return value: %v", got)
			}
			if len(got) != tt.wantErr {
				t.Errorf("ValidatingPSTSHandler.validatePstsSpec() = %v", got)
			}
		})
	}
}

// MockPSTS Create a mock PodSubnetTopologySpread object
func MockPSTS(name string) *networkv1alpha1.PodSubnetTopologySpread {
	return &networkv1alpha1.PodSubnetTopologySpread{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      name,
		},
		Spec: networkv1alpha1.PodSubnetTopologySpreadSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "foo"},
			},
			Subnets: map[string]networkv1alpha1.SubnetAllocation{},
		},
	}
}

// MockBCEObject Generate dependent BCE objects, such as subnet objects.
// This method mainly uses gomock to generate unit test objects
// generate object list:
// 1. vpc subnet object sbn-aaaaa
func MockBCEObject(mockBCE *mockcloud.MockInterface) {
	// mock vpc subnet sbn-aaaaa
	mockBCE.EXPECT().DescribeSubnet(gomock.Any(), gomock.Eq("sbn-aaaaa")).Return(
		&vpc.Subnet{
			SubnetId:    "sbn-aaaaa",
			Name:        "subnet-test-0",
			ZoneName:    "cn-bj-d",
			Cidr:        "10.178.245.0/24",
			VPCId:       "vhnqd6imivjq",
			SubnetType:  vpc.SUBNET_TYPE_BCC,
			CreatedTime: time.Now().Format(time.RFC3339),
			AvailableIp: 254,
		}, nil).AnyTimes()

	// mock not exist subnet
	mockBCE.EXPECT().DescribeSubnet(gomock.Any(), gomock.Eq("sbn-notexist")).Return(
		nil, errors.New("ErrorReasonNoSuchObject")).AnyTimes()
}

func TestValidatingPSTSHandler_Handle(t *testing.T) {
	type fields struct {
	}
	type args struct {
		obj  func() interface{}
		kind *metav1.GroupVersionKind
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "create psts",
			args: args{
				obj: func() interface{} {
					psts := MockPSTS("psts-test")
					psts.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
						"sbn-aaaaa": {
							IPAllocationStrategy: networkv1alpha1.IPAllocationStrategy{
								Type:            networkv1alpha1.IPAllocTypeManual,
								ReleaseStrategy: networkv1alpha1.ReleaseStrategyTTL,
							},
							IPv4Range: []string{
								"10.178.245.1/25",
							},
						},
					}
					return psts
				},
				kind: &pstsKind,
			},
			want: true,
		},
		{
			name: "create pstt",
			args: args{
				obj: func() interface{} {
					pstt := &networkv1alpha1.PodSubnetTopologySpreadTable{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "default",
							Name:      "pstt-test",
						},
						Spec: []networkv1alpha1.PodSubnetTopologySpreadSpec{
							{
								Selector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "foo"},
								},
								Name: "psts-test",
								Subnets: map[string]networkv1alpha1.SubnetAllocation{
									"sbn-aaaaa": {
										IPAllocationStrategy: networkv1alpha1.IPAllocationStrategy{
											Type:            networkv1alpha1.IPAllocTypeManual,
											ReleaseStrategy: networkv1alpha1.ReleaseStrategyTTL,
										},
										IPv4Range: []string{
											"10.178.245.1/25",
										},
									},
								},
							},
						},
					}
					return pstt
				},
				kind: &psttKind,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			raw, _ := json.Marshal(tt.args.obj())
			schema := runtime.NewScheme()
			networkv1alpha1.SchemeBuilder.AddToScheme(schema)
			ctl := gomock.NewController(t)
			handler := buildValidatingPSTSHandler(ctl)
			// get mock interface, mock a subnet
			mockBCE := handler.bceClient.(*mockcloud.MockInterface)
			MockBCEObject(mockBCE)

			decoder, _ := admission.NewDecoder(schema)
			handler.InjectDecoder(decoder)

			req := admission.Request{
				AdmissionRequest: admissionv1beta1.AdmissionRequest{
					Object: runtime.RawExtension{
						Raw: raw,
					},
					Operation: admissionv1beta1.Create,
					Kind:      *tt.args.kind,
				},
			}
			if got := handler.Handle(context.Background(), req); got.Allowed != tt.want {
				t.Errorf("ValidatingPSTSHandler.Handle() = %v, want %v", got.Allowed, tt.want)
			}
		})
	}
}

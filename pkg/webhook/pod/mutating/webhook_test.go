package mutating

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdfake "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned/fake"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	networkinformer "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions/networking/v1alpha1"
	"github.com/baidubce/bce-sdk-go/services/vpc"
	"github.com/golang/mock/gomock"
	_ "github.com/golang/mock/mockgen/model"
	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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

func buildMutatingPodHandler(ctrl *gomock.Controller) *MutatingPodHandler {
	crdClient, crdInformer, cloudClient := setupEnv(ctrl)
	handler := &MutatingPodHandler{}
	handler.InjectCRDClient(crdClient)
	handler.InjectCloudClient(cloudClient)
	handler.InjectNetworkInformer(crdInformer)
	handler.InjectCNIMode(types.CCEModeSecondaryIPVeth)

	crdInformer.Start(wait.NeverStop)
	return handler
}

func TestMutatingPodHandler_validatePstsSpec(t *testing.T) {

	tests := []struct {
		name    string
		psts    func() *networkv1alpha1.PodSubnetTopologySpread
		preTest func(handler *MutatingPodHandler)
		wantErr int
	}{
		{
			name: "pod without psts",
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
			name: "subnet is not exist",
			psts: func() *networkv1alpha1.PodSubnetTopologySpread {
				obj := MockPSTS("psts-notexist")
				obj.Spec.Subnets = map[string]networkv1alpha1.SubnetAllocation{
					"sbn-notexist": {},
				}
				return obj
			},
			preTest: func(handler *MutatingPodHandler) {},
			wantErr: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctl := gomock.NewController(t)
			handler := buildMutatingPodHandler(ctl)
			// get mock interface, mock a subnet
			mockBCE := handler.bceClient.(*mockcloud.MockInterface)
			MockBCEObject(mockBCE)
			// got := handler.validatePstsSpec(tt.psts(), field.NewPath("spec"))
			// if len(got) != 0 {
			// 	t.Logf("with return value: %v", got)
			// }
			// if len(got) != tt.wantErr {
			// 	t.Errorf("MutatingPodHandler.validatePstsSpec() = %v", got)
			// }
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

func TestMutatingPodHandler_Handle(t *testing.T) {
	type fields struct {
		crdClient      versioned.Interface
		subnetInformer networkinformer.SubnetInformer
		pstsInformer   networkinformer.PodSubnetTopologySpreadInformer
		bceClient      cloud.Interface
		Decoder        *admission.Decoder
	}
	type args struct {
		ctx context.Context
		req admission.Request
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   admission.Response
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &MutatingPodHandler{
				crdClient:      tt.fields.crdClient,
				subnetInformer: tt.fields.subnetInformer,
				pstsInformer:   tt.fields.pstsInformer,
				bceClient:      tt.fields.bceClient,
				Decoder:        tt.fields.Decoder,
			}
			if got := h.Handle(tt.args.ctx, tt.args.req); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MutatingPodHandler.Handle() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddPodMutatingWebhookFlags(t *testing.T) {
	type args struct {
		pflags *pflag.FlagSet
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "add flags",
			args: args{pflags: &pflag.FlagSet{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			RegisterPodMutatingWebhookFlags(tt.args.pflags)
		})
	}
}

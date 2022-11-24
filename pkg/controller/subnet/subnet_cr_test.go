package subnet

import (
	"context"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMarkExclusiveSubnet(t *testing.T) {
	ctx := context.TODO()
	tsc := MockSubnetController(t)
	sbn := data.MockSubnet("default", "sbn-test", "192.168.1.0/24")
	tsc.crdClient.CceV1alpha1().Subnets("default").Create(ctx, sbn, metav1.CreateOptions{})
	sbn, _ = MarkExclusiveSubnet(ctx, tsc.crdClient, "sbn-test", true)
	if !sbn.Spec.Exclusive {
		t.Errorf("subnet is not exclusive")
	}
}

func TestGetSubnet(t *testing.T) {
	ctx := context.TODO()
	tsc := MockSubnetController(t)
	GetSubnet(ctx, tsc.crdClient, "sbn-test")
}

func TestDeclareSubnetHasNoMoreIP(t *testing.T) {
	tsc := MockSubnetController(t)
	type args struct {
		ctx         context.Context
		crdClient   versioned.Interface
		crdInformer crdinformers.SharedInformerFactory
		subnetID    string
		hasNoMoreIP bool
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "mark have no more ip",
			args: args{
				ctx:         context.TODO(),
				crdClient:   tsc.crdClient,
				crdInformer: tsc.crdInformer,
				subnetID:    "sbn-test",
				hasNoMoreIP: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sbn := data.MockSubnet("default", "sbn-test", "192.168.1.0/24")
			tsc.crdClient.CceV1alpha1().Subnets("default").Create(tt.args.ctx, sbn, metav1.CreateOptions{})
			waitForCacheSync(tt.args.crdInformer)
			if err := DeclareSubnetHasNoMoreIP(tt.args.ctx, tt.args.crdClient, tt.args.crdInformer, tt.args.subnetID, tt.args.hasNoMoreIP); (err != nil) != tt.wantErr {
				t.Errorf("DeclareSubnetHasNoMoreIP() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

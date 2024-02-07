package podmutating

import (
	"context"
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
)

func TestMutatingPodHandler_addIPResource2Pod(t *testing.T) {
	containers := []corev1.Container{
		{
			Name: "bustbox",
		},
	}
	type fields struct {
	}
	type args struct {
		ctx context.Context
		pod *corev1.Pod
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "host network pod",
			args: args{
				pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						HostNetwork: true,
						Containers:  containers,
					},
				},
			},
		},
		{
			name: "host network pod",
			args: args{
				pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						HostNetwork: false,
						Containers:  containers,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addCustomerResource2Pod(tt.args.ctx, tt.args.pod, k8s.ResourceIPForNode)
		})
	}
}

package types

import (
	"testing"
)

const InvalidNetworkMode ContainerNetworkMode = "vpc-secondary-ip-vpc-route-invalid"

func TestIsCCECNIModeBasedOnVPCRoute(t *testing.T) {
	type fields struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "CCEModeRouteVeth",
			fields: fields{
				mode: CCEModeRouteVeth,
			},
			want: true,
		},
		{
			name: "CCEModeRouteIPVlan",
			fields: fields{
				mode: CCEModeRouteIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeRouteAutoDetect",
			fields: fields{
				mode: CCEModeRouteAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeBBCSecondaryIPAutoDetect,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeBBCSecondaryIPIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPVeth",
			fields: fields{
				mode: CCEModeBBCSecondaryIPVeth,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeSecondaryIPAutoDetect,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeSecondaryIPIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPVeth",
			fields: fields{
				mode: CCEModeSecondaryIPVeth,
			},
			want: false,
		},
		{
			name: "Invalid Input",
			fields: fields{
				mode: InvalidNetworkMode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCCECNIModeBasedOnVPCRoute(tt.fields.mode)
			if got != tt.want {
				t.Errorf("IsCCECNIModeBasedOnVPCRoute = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCCECNIModeAutoDetect(t *testing.T) {
	type fields struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "CCEModeRouteVeth",
			fields: fields{
				mode: CCEModeRouteVeth,
			},
			want: false,
		},
		{
			name: "CCEModeRouteIPVlan",
			fields: fields{
				mode: CCEModeRouteIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeRouteAutoDetect",
			fields: fields{
				mode: CCEModeRouteAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeBBCSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeBBCSecondaryIPIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPVeth",
			fields: fields{
				mode: CCEModeBBCSecondaryIPVeth,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeSecondaryIPIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPVeth",
			fields: fields{
				mode: CCEModeSecondaryIPVeth,
			},
			want: false,
		},
		{
			name: "Invalid Input",
			fields: fields{
				mode: InvalidNetworkMode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCCECNIModeAutoDetect(tt.fields.mode)
			if got != tt.want {
				t.Errorf("IsCCECNIModeAutoDetect = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCCECNIModeBasedOnBCCSecondaryIP(t *testing.T) {
	type fields struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "CCEModeRouteVeth",
			fields: fields{
				mode: CCEModeRouteVeth,
			},
			want: false,
		},
		{
			name: "CCEModeRouteIPVlan",
			fields: fields{
				mode: CCEModeRouteIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeRouteAutoDetect",
			fields: fields{
				mode: CCEModeRouteAutoDetect,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeBBCSecondaryIPAutoDetect,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeBBCSecondaryIPIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPVeth",
			fields: fields{
				mode: CCEModeBBCSecondaryIPVeth,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeSecondaryIPIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPVeth",
			fields: fields{
				mode: CCEModeSecondaryIPVeth,
			},
			want: true,
		},
		{
			name: "Invalid Input",
			fields: fields{
				mode: InvalidNetworkMode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCCECNIModeBasedOnBCCSecondaryIP(tt.fields.mode)
			if got != tt.want {
				t.Errorf("IsCCECNIModeBasedOnBCCSecondaryIP = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCCECNIModeBasedOnBBCSecondaryIP(t *testing.T) {
	type fields struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "CCEModeRouteVeth",
			fields: fields{
				mode: CCEModeRouteVeth,
			},
			want: false,
		},
		{
			name: "CCEModeRouteIPVlan",
			fields: fields{
				mode: CCEModeRouteIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeRouteAutoDetect",
			fields: fields{
				mode: CCEModeRouteAutoDetect,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeBBCSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeBBCSecondaryIPIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPVeth",
			fields: fields{
				mode: CCEModeBBCSecondaryIPVeth,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeSecondaryIPAutoDetect,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeSecondaryIPIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeSecondaryIPVeth",
			fields: fields{
				mode: CCEModeSecondaryIPVeth,
			},
			want: false,
		},
		{
			name: "Invalid Input",
			fields: fields{
				mode: InvalidNetworkMode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCCECNIModeBasedOnBBCSecondaryIP(tt.fields.mode)
			if got != tt.want {
				t.Errorf("IsCCECNIModeBasedOnBBCSecondaryIP = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCCECNIModeBasedOnSecondaryIP(t *testing.T) {
	type fields struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "CCEModeRouteVeth",
			fields: fields{
				mode: CCEModeRouteVeth,
			},
			want: false,
		},
		{
			name: "CCEModeRouteIPVlan",
			fields: fields{
				mode: CCEModeRouteIPVlan,
			},
			want: false,
		},
		{
			name: "CCEModeRouteAutoDetect",
			fields: fields{
				mode: CCEModeRouteAutoDetect,
			},
			want: false,
		},
		{
			name: "CCEModeBBCSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeBBCSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeBBCSecondaryIPIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPVeth",
			fields: fields{
				mode: CCEModeBBCSecondaryIPVeth,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeSecondaryIPIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPVeth",
			fields: fields{
				mode: CCEModeSecondaryIPVeth,
			},
			want: true,
		},
		{
			name: "Invalid Input",
			fields: fields{
				mode: InvalidNetworkMode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCCECNIModeBasedOnSecondaryIP(tt.fields.mode)
			if got != tt.want {
				t.Errorf("IsCCECNIModeBasedOnSecondaryIP = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCCECNIMode(t *testing.T) {
	type fields struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name   string
		fields fields
		want   bool
	}{
		{
			name: "CCEModeRouteVeth",
			fields: fields{
				mode: CCEModeRouteVeth,
			},
			want: true,
		},
		{
			name: "CCEModeRouteIPVlan",
			fields: fields{
				mode: CCEModeRouteIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeRouteAutoDetect",
			fields: fields{
				mode: CCEModeRouteAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeBBCSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeBBCSecondaryIPIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeBBCSecondaryIPVeth",
			fields: fields{
				mode: CCEModeBBCSecondaryIPVeth,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPAutoDetect",
			fields: fields{
				mode: CCEModeSecondaryIPAutoDetect,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPIPVlan",
			fields: fields{
				mode: CCEModeSecondaryIPIPVlan,
			},
			want: true,
		},
		{
			name: "CCEModeSecondaryIPVeth",
			fields: fields{
				mode: CCEModeSecondaryIPVeth,
			},
			want: true,
		},
		{
			name: "CCEModeCrossVPCEni",
			fields: fields{
				mode: CCEModeCrossVPCEni,
			},
			want: true,
		},
		{
			name: "CCEModeExclusiveCrossVPCEni",
			fields: fields{
				mode: CCEModeExclusiveCrossVPCEni,
			},
			want: true,
		},
		{
			name: "Invalid Input",
			fields: fields{
				mode: InvalidNetworkMode,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCCECNIMode(tt.fields.mode)
			if got != tt.want {
				t.Errorf("IsCCECNIMode = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCrossVPCEniMode(t *testing.T) {
	type args struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "CCEModeCrossVPCEni",
			args: args{
				mode: CCEModeCrossVPCEni,
			},
			want: true,
		},
		{
			name: "CCEModeExclusiveCrossVPCEni",
			args: args{
				mode: CCEModeExclusiveCrossVPCEni,
			},
			want: true,
		},
		{
			name: "CCEModeRouteVeth",
			args: args{
				mode: CCEModeRouteVeth,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCrossVPCEniMode(tt.args.mode); got != tt.want {
				t.Errorf("IsCrossVPCEniMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsKubenetMode(t *testing.T) {
	type args struct {
		mode ContainerNetworkMode
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "CCEModeCrossVPCEni",
			args: args{
				mode: CCEModeCrossVPCEni,
			},
			want: false,
		},
		{
			name: "K8sNetworkModeKubenet",
			args: args{
				mode: K8sNetworkModeKubenet,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsKubenetMode(tt.args.mode); got != tt.want {
				t.Errorf("IsKubenetMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

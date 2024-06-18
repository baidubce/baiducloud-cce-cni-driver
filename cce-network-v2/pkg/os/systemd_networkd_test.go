package os

import (
	"testing"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/test/testdata"
)

func TestUpdateSystemdConfigOption(t *testing.T) {

	type args struct {
		linkPath string
		key      string
		value    string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "Update systemd config option",
			args: args{
				linkPath: testdata.Path("os/ubuntu/systemd/default.link"),
				key:      macAddressPolicyKey,
				value:    "none",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := UpdateSystemdConfigOption(tt.args.linkPath, tt.args.key, tt.args.value); (err != nil) != tt.wantErr {
				t.Errorf("updateSystemdConfigOption() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

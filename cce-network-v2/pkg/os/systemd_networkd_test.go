package os

import (
	"io/ioutil"
	"os"
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
		UpdateSystemdConfigOption(tt.args.linkPath, tt.args.key, tt.args.value)
	}
}

func TestPrintFileContent(t *testing.T) {
	type args struct {
		filename string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "File exists and can be read",
			args: args{
				filename: "testfile.txt",
			},
			wantErr: false,
		},
		{
			name: "File does not exist",
			args: args{
				filename: "nonexistent.txt",
			},
			wantErr: true,
		},
	}

	// Create a temporary valid file for the test
	tempFileContent := "Hello, World!"
	tempFile, err := ioutil.TempFile("", "testfile.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tempFile.Name()) // Clean up the file afterwards

	if _, err := tempFile.WriteString(tempFileContent); err != nil {
		t.Fatal(err)
	}
	tempFile.Close()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.args.filename == "testfile.txt" {
				err = PrintFileContent(tempFile.Name())
			} else {
				err = PrintFileContent(tt.args.filename)
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("PrintFileContent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckIfLinkOptionConfigured(t *testing.T) {
	type args struct {
		linkPath string
		key      string
		value    string
	}
	tests := []struct {
		name    string
		args    args
		want    bool
		wantErr bool
	}{
		{
			name: "check systemd config option",
			args: args{
				linkPath: testdata.Path("os/ubuntu/systemd/default.link"),
				key:      macAddressPolicyKey,
				value:    "none",
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "file does not exist",
			args: args{
				linkPath: testdata.Path(""),
				key:      macAddressPolicyKey,
				value:    "none",
			},
			want:    false,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CheckIfLinkOptionConfigured(tt.args.linkPath, tt.args.key, tt.args.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckIfLinkOptionConfigured() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("CheckIfLinkOptionConfigured() got = %v, want %v", got, tt.want)
			}
		})
	}
}

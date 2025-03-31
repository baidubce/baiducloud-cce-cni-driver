//go:build !privileged_tests

/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package option

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	. "gopkg.in/check.v1"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
)

func (s *OptionSuite) TestValidateIPv6ClusterAllocCIDR(c *C) {
	valid1 := &DaemonConfig{IPv6ClusterAllocCIDR: "fdfd::/64"}
	c.Assert(valid1.validateIPv6ClusterAllocCIDR(), IsNil)
	c.Assert(valid1.IPv6ClusterAllocCIDRBase, Equals, "fdfd::")

	valid2 := &DaemonConfig{IPv6ClusterAllocCIDR: "fdfd:fdfd:fdfd:fdfd:aaaa::/64"}
	c.Assert(valid2.validateIPv6ClusterAllocCIDR(), IsNil)
	c.Assert(valid2.IPv6ClusterAllocCIDRBase, Equals, "fdfd:fdfd:fdfd:fdfd::")

	invalid1 := &DaemonConfig{IPv6ClusterAllocCIDR: "foo"}
	c.Assert(invalid1.validateIPv6ClusterAllocCIDR(), Not(IsNil))

	invalid2 := &DaemonConfig{IPv6ClusterAllocCIDR: "fdfd"}
	c.Assert(invalid2.validateIPv6ClusterAllocCIDR(), Not(IsNil))

	invalid3 := &DaemonConfig{IPv6ClusterAllocCIDR: "fdfd::/32"}
	c.Assert(invalid3.validateIPv6ClusterAllocCIDR(), Not(IsNil))

	invalid4 := &DaemonConfig{}
	c.Assert(invalid4.validateIPv6ClusterAllocCIDR(), Not(IsNil))
}

func TestGetEnvName(t *testing.T) {
	type args struct {
		option string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Normal option",
			args: args{
				option: "foo",
			},
			want: "CCE_FOO",
		},
		{
			name: "Capital option",
			args: args{
				option: "FOO",
			},
			want: "CCE_FOO",
		},
		{
			name: "with numbers",
			args: args{
				option: "2222",
			},
			want: "CCE_2222",
		},
		{
			name: "mix numbers small letters",
			args: args{
				option: "22ada22",
			},
			want: "CCE_22ADA22",
		},
		{
			name: "mix numbers small letters and dashes",
			args: args{
				option: "22ada2------2",
			},
			want: "CCE_22ADA2______2",
		},
		{
			name: "normal option",
			args: args{
				option: "conntrack-garbage-collector-interval",
			},
			want: "CCE_CONNTRACK_GARBAGE_COLLECTOR_INTERVAL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getEnvName(tt.args.option); got != tt.want {
				t.Errorf("getEnvName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func (s *OptionSuite) TestReadDirConfig(c *C) {
	var dirName string
	type args struct {
		dirName string
	}
	type want struct {
		allSettings        map[string]interface{}
		allSettingsChecker Checker
		err                error
		errChecker         Checker
	}
	tests := []struct {
		name        string
		setupArgs   func() args
		setupWant   func() want
		preTestRun  func()
		postTestRun func()
	}{
		{
			name: "empty configuration",
			preTestRun: func() {
				dirName = c.MkDir()

				fs := flag.NewFlagSet("empty configuration", flag.ContinueOnError)
				viper.BindPFlags(fs)
			},
			setupArgs: func() args {
				return args{
					dirName: dirName,
				}
			},
			setupWant: func() want {
				return want{
					allSettings:        map[string]interface{}{},
					allSettingsChecker: DeepEquals,
					err:                nil,
					errChecker:         Equals,
				}
			},
			postTestRun: func() {
				os.RemoveAll(dirName)
			},
		},
		{
			name: "single file configuration",
			preTestRun: func() {
				dirName = c.MkDir()

				fullPath := filepath.Join(dirName, "test")
				err := os.WriteFile(fullPath, []byte(`"1"
`), os.FileMode(0644))
				c.Assert(err, IsNil)
				fs := flag.NewFlagSet("single file configuration", flag.ContinueOnError)
				fs.String("test", "", "")
				BindEnv("test")
				viper.BindPFlags(fs)

				fmt.Println(fullPath)
			},
			setupArgs: func() args {
				return args{
					dirName: dirName,
				}
			},
			setupWant: func() want {
				return want{
					allSettings:        map[string]interface{}{"test": `"1"`},
					allSettingsChecker: DeepEquals,
					err:                nil,
					errChecker:         Equals,
				}
			},
			postTestRun: func() {
				os.RemoveAll(dirName)
			},
		},
	}
	for _, tt := range tests {
		tt.preTestRun()
		args := tt.setupArgs()
		want := tt.setupWant()
		m, err := ReadDirConfig(args.dirName)
		c.Assert(err, want.errChecker, want.err, Commentf("Test Name: %s", tt.name))
		err = MergeConfig(m)
		c.Assert(err, IsNil)
		c.Assert(viper.AllSettings(), want.allSettingsChecker, want.allSettings, Commentf("Test Name: %s", tt.name))
		tt.postTestRun()
	}
}

func (s *OptionSuite) TestBindEnv(c *C) {
	optName1 := "foo-bar"
	os.Setenv("LEGACY_FOO_BAR", "legacy")
	os.Setenv(getEnvName(optName1), "new")
	BindEnvWithLegacyEnvFallback(optName1, "LEGACY_FOO_BAR")
	c.Assert(viper.GetString(optName1), Equals, "new")

	optName2 := "bar-foo"
	BindEnvWithLegacyEnvFallback(optName2, "LEGACY_FOO_BAR")
	c.Assert(viper.GetString(optName2), Equals, "legacy")

	viper.Reset()
}

func (s *OptionSuite) TestEnabledFunctions(c *C) {
	d := &DaemonConfig{}
	c.Assert(d.IPv4Enabled(), Equals, false)
	c.Assert(d.IPv6Enabled(), Equals, false)
	d = &DaemonConfig{EnableIPv4: true}
	c.Assert(d.IPv4Enabled(), Equals, true)
	c.Assert(d.IPv6Enabled(), Equals, false)
	d = &DaemonConfig{EnableIPv6: true}
	c.Assert(d.IPv4Enabled(), Equals, false)
	c.Assert(d.IPv6Enabled(), Equals, true)
	d = &DaemonConfig{}
	c.Assert(d.IPAMMode(), Equals, "")
	d = &DaemonConfig{IPAM: ipamOption.IPAMVpcEni}
	c.Assert(d.IPAMMode(), Equals, ipamOption.IPAMVpcEni)
}

func (s *OptionSuite) TestLocalAddressExclusion(c *C) {
	d := &DaemonConfig{}
	err := d.parseExcludedLocalAddresses([]string{"1.1.1.1/32", "3.3.3.0/24", "f00d::1/128"})
	c.Assert(err, IsNil)

	c.Assert(d.IsExcludedLocalAddress(net.ParseIP("1.1.1.1")), Equals, true)
	c.Assert(d.IsExcludedLocalAddress(net.ParseIP("1.1.1.2")), Equals, false)
	c.Assert(d.IsExcludedLocalAddress(net.ParseIP("3.3.3.1")), Equals, true)
	c.Assert(d.IsExcludedLocalAddress(net.ParseIP("f00d::1")), Equals, true)
	c.Assert(d.IsExcludedLocalAddress(net.ParseIP("f00d::2")), Equals, false)
}
func TestCheckIPAMDelegatedPlugin(t *testing.T) {
	tests := []struct {
		name      string
		d         *DaemonConfig
		expectErr error
	}{
		{
			name: "IPAMDelegatedPlugin with local router IPv4 set and endpoint health checking disabled",
			d: &DaemonConfig{
				IPAM:            ipamOption.IPAMDelegatedPlugin,
				EnableIPv4:      true,
				LocalRouterIPv4: "169.254.0.0",
			},
			expectErr: nil,
		},
		{
			name: "IPAMDelegatedPlugin with local router IPv6 set and endpoint health checking disabled",
			d: &DaemonConfig{
				IPAM:            ipamOption.IPAMDelegatedPlugin,
				EnableIPv6:      true,
				LocalRouterIPv6: "fe80::1",
			},
			expectErr: nil,
		},
		{
			name: "IPAMDelegatedPlugin with health checking enabled",
			d: &DaemonConfig{
				IPAM:                         ipamOption.IPAMDelegatedPlugin,
				EnableHealthChecking:         true,
				EnableEndpointHealthChecking: true,
			},
			expectErr: fmt.Errorf("--enable-endpoint-health-checking must be disabled with --ipam=delegated-plugin"),
		},
		{
			name: "IPAMDelegatedPlugin without local router IPv4",
			d: &DaemonConfig{
				IPAM:       ipamOption.IPAMDelegatedPlugin,
				EnableIPv4: true,
			},
			expectErr: fmt.Errorf("--local-router-ipv4 must be provided when IPv4 is enabled with --ipam=delegated-plugin"),
		},
		{
			name: "IPAMDelegatedPlugin without local router IPv6",
			d: &DaemonConfig{
				IPAM:       ipamOption.IPAMDelegatedPlugin,
				EnableIPv6: true,
			},
			expectErr: fmt.Errorf("--local-router-ipv6 must be provided when IPv6 is enabled with --ipam=delegated-plugin"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.d.checkIPAMDelegatedPlugin()
			if tt.expectErr != nil && err == nil {
				t.Errorf("expected error but got none")
			} else if tt.expectErr == nil && err != nil {
				t.Errorf("expected no error but got %q", err)
			} else if tt.expectErr != nil && tt.expectErr.Error() != err.Error() {
				t.Errorf("expected error %q but got %q", tt.expectErr, err)
			}
		})
	}
}

func (s *OptionSuite) TestGetDefaultMonitorQueueSize(c *C) {
	c.Assert(getDefaultMonitorQueueSize(4), Equals, 4*defaults.MonitorQueueSizePerCPU)
	c.Assert(getDefaultMonitorQueueSize(1000), Equals, defaults.MonitorQueueSizePerCPUMaximum)
}

const (
	_   = iota
	KiB = 1 << (10 * iota)
	MiB
	GiB
)

func (s *OptionSuite) Test_backupFiles(c *C) {
	tempDir := c.MkDir()
	fileNames := []string{"test.json", "test-1.json", "test-2.json"}

	backupFiles(tempDir, fileNames)
	files, err := os.ReadDir(tempDir)
	c.Assert(err, IsNil)
	// No files should have been created
	c.Assert(len(files), Equals, 0)

	_, err = os.Create(filepath.Join(tempDir, "test.json"))
	c.Assert(err, IsNil)

	backupFiles(tempDir, fileNames)
	files, err = os.ReadDir(tempDir)
	c.Assert(err, IsNil)
	c.Assert(len(files), Equals, 1)
	c.Assert(files[0].Name(), Equals, "test-1.json")

	backupFiles(tempDir, fileNames)
	files, err = os.ReadDir(tempDir)
	c.Assert(err, IsNil)
	c.Assert(len(files), Equals, 1)
	c.Assert(files[0].Name(), Equals, "test-2.json")

	_, err = os.Create(filepath.Join(tempDir, "test.json"))
	c.Assert(err, IsNil)

	backupFiles(tempDir, fileNames)
	files, err = os.ReadDir(tempDir)
	c.Assert(err, IsNil)
	c.Assert(len(files), Equals, 2)
	c.Assert(files[0].Name(), Equals, "test-1.json")
	c.Assert(files[1].Name(), Equals, "test-2.json")
}

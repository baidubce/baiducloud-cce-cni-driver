/*
 * Copyright (c) 2021 Baidu, Inc. All Rights Reserved.
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

package roce

import (
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/golang/mock/gomock"
)

type MockDirEntry1 struct {
	name string

	fs.DirEntry
}

func (entry MockDirEntry1) Name() string {
	return entry.name
}

func TestController_RoCEHasMellanoxModuleLoaded(t *testing.T) {
	type fields struct {
		ctrl *gomock.Controller
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name           string
		mockStdlibFunc func()
		fields         fields
		args           args
		want           bool
		wantErr        bool
	}{
		/** normal */
		{
			mockStdlibFunc: func() {

				OSReadFile = func(name string) ([]byte, error) {
					var v string
					var b bool
					var pvmap map[string]string

					pvmap = make(map[string]string)
					pvmap["/proc/modules"] = `ablk_helper 13597 1 aesni_intel, Live 0xffffffffc04ca000
cryptd 21190 3 ghash_clmulni_intel,aesni_intel,ablk_helper, Live 0xffffffffc039a000
serio_raw 13434 0 - Live 0xffffffffc0337000
mlx5_core 1448990 1 mlx5_ib, Live 0xffffffffc0521000 (OE)
auxiliary 13942 2 mlx5_ib,mlx5_core, Live 0xffffffffc0425000 (OE)
mlx_compat 55269 11 rdma_ucm,rdma_cm,iw_cm,ib_ipoib,ib_cm,ib_umad,mlx5_ib,ib_uverbs,ib_core,mlx5_core,auxiliary, Live 0xffffffffc04f8000 (OE)
mlxfw 22321 1 mlx5_core, Live 0xffffffffc049e000 (OE)
psample 13526 1 mlx5_core, Live 0xffffffffc042c000
ast 55467 1 - Live 0xffffffffc050c000
devlink 60067 1 mlx5_core, Live 0xffffffffc04ae000
i2c_algo_bit 13413 1 ast, Live 0xffffffffc04a9000`

					if v, b = pvmap[name]; b != true {
						return nil, os.ErrNotExist
					}

					return []byte(v), nil
				}
			},
			fields: func() fields {
				return fields{}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    true,
			wantErr: false,
		},
		/** normal[1]: no mlx module loaded */
		{
			mockStdlibFunc: func() {

				OSReadFile = func(name string) ([]byte, error) {
					var v string
					var b bool
					var pvmap map[string]string

					pvmap = make(map[string]string)
					pvmap["/proc/modules"] = `ablk_helper 13597 1 aesni_intel, Live 0xffffffffc04ca000
cryptd 21190 3 ghash_clmulni_intel,aesni_intel,ablk_helper, Live 0xffffffffc039a000
serio_raw 13434 0 - Live 0xffffffffc0337000
mlx5_core 1448990 1 mlx5_ib, Live 0xffffffffc0521000 (OE)
auxiliary 13942 2 mlx5_ib,mlx5_core, Live 0xffffffffc0425000 (OE)
mlxfw 22321 1 mlx5_core, Live 0xffffffffc049e000 (OE)
psample 13526 1 mlx5_core, Live 0xffffffffc042c000
ast 55467 1 - Live 0xffffffffc050c000
devlink 60067 1 mlx5_core, Live 0xffffffffc04ae000
i2c_algo_bit 13413 1 ast, Live 0xffffffffc04a9000`

					if v, b = pvmap[name]; b != true {
						return nil, os.ErrNotExist
					}

					return []byte(v), nil
				}
			},
			fields: func() fields {
				return fields{}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    false,
			wantErr: false,
		},
		/** normal[2]: no ib flags set */
		{
			mockStdlibFunc: func() {

				OSReadFile = func(name string) ([]byte, error) {
					var v string
					var b bool
					var pvmap map[string]string

					pvmap = make(map[string]string)
					pvmap["/proc/modules"] = `ablk_helper 13597 1 aesni_intel, Live 0xffffffffc04ca000
cryptd 21190 3 ghash_clmulni_intel,aesni_intel,ablk_helper, Live 0xffffffffc039a000
serio_raw 13434 0 - Live 0xffffffffc0337000
mlx5_core 1448990 1 mlx5_ib, Live 0xffffffffc0521000 (OE)
auxiliary 13942 2 mlx5_ib,mlx5_core, Live 0xffffffffc0425000 (OE)
mlxfw 22321 1 mlx5_core, Live 0xffffffffc049e000 (OE)
mlx_compat 55269 11 rdma_ucm,rdma_cm,iw_cm,ib_ipoib,ib_cm,ib_umad,mlx5_ib,ib_core,mlx5_core,auxiliary, Live 0xffffffffc04f8000 (OE)
psample 13526 1 mlx5_core, Live 0xffffffffc042c000
ast 55467 1 - Live 0xffffffffc050c000
devlink 60067 1 mlx5_core, Live 0xffffffffc04ae000
i2c_algo_bit 13413 1 ast, Live 0xffffffffc04a9000`

					if v, b = pvmap[name]; b != true {
						return nil, os.ErrNotExist
					}

					return []byte(v), nil
				}
			},
			fields: func() fields {
				return fields{}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    false,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bret bool
			var err error

			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}

			if tt.mockStdlibFunc != nil {
				tt.mockStdlibFunc()
			}

			roce := ProbeNew()
			if bret, err = roce.RoCEHasMellanoxModuleLoaded(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("RoCEHasMellanoxDevOnBoard() error = %v, wantErr %v", err, tt.wantErr)
			}

			if bret != tt.want {
				t.Errorf("RoCEHasMellanoxDevOnBoard() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestController_RoCEHasMellanoxDevOnBoard(t *testing.T) {
	type fields struct {
		ctrl *gomock.Controller
	}
	type args struct {
		ctx context.Context
	}
	tests := []struct {
		name           string
		mockStdlibFunc func()
		fields         fields
		args           args
		want           bool
		wantErr        bool
	}{
		/** normal */
		{
			mockStdlibFunc: func() {
				OSReadDir = func(name string) ([]os.DirEntry, error) {
					var res []os.DirEntry
					var entry MockDirEntry1

					res = make([]os.DirEntry, 0, 128)

					//non-RoCE
					entry.name = "0000:ff:0e.0"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.1"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.2"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.3"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.4"
					res = append(res, entry)
					//non-RoCE
					entry.name = "0000:ff:0f.5"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.6"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.7"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.8"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.9"
					res = append(res, entry)
					//non-RoCE
					entry.name = "0000:ff:0f.a"
					res = append(res, entry)

					return res, nil
				}

				OSReadFile = func(name string) ([]byte, error) {
					var v string
					var b bool
					var pvmap map[string]string

					pvmap = make(map[string]string)
					//non-RoCE
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/vendor"] = "0x8086\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/device"] = "0x0a53\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/subsystem_vendor"] = "0x8086\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/subsystem_device"] = "0x0a53\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/subsystem_device"] = "0x0016\n"

					//non-RoCE
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/vendor"] = "0x1002\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/device"] = "0x5245\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/subsystem_vendor"] = "0x1002\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/subsystem_device"] = "0x5245\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/subsystem_device"] = "0x0016\n"

					//non-RoCE
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/vendor"] = "0x10de\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/device"] = "0x0100\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/subsystem_vendor"] = "0x10de\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/subsystem_device"] = "0x0100\n"

					if v, b = pvmap[name]; b != true {
						return nil, os.ErrNotExist
					}

					return []byte(v), nil
				}
			},
			fields: func() fields {

				return fields{}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    true,
			wantErr: false,
		},
		/** exceptional 1: readdir error */
		{
			mockStdlibFunc: func() {
				OSReadDir = func(name string) ([]os.DirEntry, error) {
					return nil, os.ErrNotExist
				}

				OSReadFile = func(name string) ([]byte, error) {
					var v string
					var b bool
					var pvmap map[string]string

					pvmap = make(map[string]string)
					//non-RoCE
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/vendor"] = "0x8086\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/device"] = "0x0a53\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/subsystem_vendor"] = "0x8086\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.0/subsystem_device"] = "0x0a53\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.1/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.2/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.3/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0e.4/subsystem_device"] = "0x0016\n"

					//non-RoCE
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/vendor"] = "0x1002\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/device"] = "0x5245\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/subsystem_vendor"] = "0x1002\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.5/subsystem_device"] = "0x5245\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.6/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.7/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.8/subsystem_device"] = "0x0016\n"

					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/device"] = "0x101d\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/subsystem_vendor"] = "0x15b3\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.9/subsystem_device"] = "0x0016\n"

					//non-RoCE
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/class"] = "0x020000\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/vendor"] = "0x10de\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/device"] = "0x0100\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/subsystem_vendor"] = "0x10de\n"
					pvmap["/sys/bus/pci/devices/0000:ff:0f.a/subsystem_device"] = "0x0100\n"

					if v, b = pvmap[name]; b != true {
						return nil, os.ErrNotExist
					}

					return []byte(v), nil
				}
			},
			fields: func() fields {

				return fields{}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    false,
			wantErr: true,
		},
		/** exceptional 2: readfile error */
		{
			mockStdlibFunc: func() {
				OSReadDir = func(name string) ([]os.DirEntry, error) {
					var res []os.DirEntry
					var entry MockDirEntry1

					res = make([]os.DirEntry, 0, 128)

					//non-RoCE
					entry.name = "0000:ff:0e.0"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.1"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.2"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.3"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0e.4"
					res = append(res, entry)
					//non-RoCE
					entry.name = "0000:ff:0f.5"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.6"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.7"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.8"
					res = append(res, entry)
					//RoCE
					entry.name = "0000:ff:0f.9"
					res = append(res, entry)
					//non-RoCE
					entry.name = "0000:ff:0f.a"
					res = append(res, entry)

					return res, nil
				}

				OSReadFile = func(name string) ([]byte, error) {
					return nil, fs.ErrInvalid
				}
			},
			fields: func() fields {

				return fields{}
			}(),
			args: args{
				ctx: context.TODO(),
			},
			want:    false,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bret bool
			var err error

			if tt.fields.ctrl != nil {
				defer tt.fields.ctrl.Finish()
			}

			if tt.mockStdlibFunc != nil {
				tt.mockStdlibFunc()
			}

			roce := ProbeNew()
			if bret, err = roce.RoCEHasMellanoxDevOnBoard(tt.args.ctx); (err != nil) != tt.wantErr {
				t.Errorf("RoCEHasMellanoxDevOnBoard() error = %v, wantErr %v", err, tt.wantErr)
			}

			if bret != tt.want {
				t.Errorf("RoCEHasMellanoxDevOnBoard() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

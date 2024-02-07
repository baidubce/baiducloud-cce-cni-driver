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

package link

import (
	"net"
	"syscall"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestIsNotExistError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			args: args{
				err: syscall.ENOENT,
			},
			want: true,
		},
		{
			args: args{
				err: net.ErrWriteToConnected,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotExistError(tt.args.err); got != tt.want {
				t.Errorf("IsNotExistError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsExistsError(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			args: args{
				err: &net.DNSError{},
			},
			want: false,
		},
		{
			args: args{
				err: syscall.EEXIST,
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsExistsError(tt.args.err); got != tt.want {
				t.Errorf("IsExistsError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsLinkNotFound(t *testing.T) {
	type args struct {
		err error
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			args: args{
				err: netlink.LinkNotFoundError{},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLinkNotFound(tt.args.err); got != tt.want {
				t.Errorf("IsLinkNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

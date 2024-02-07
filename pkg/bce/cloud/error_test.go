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

package cloud

import (
	"fmt"
	"testing"
)

func TestIsENINotFound(t *testing.T) {
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
				err: nil,
			},
			want: false,
		},
		{
			args: args{
				err: fmt.Errorf("Error Message: \"The param eniId is invalid\", Error Code: \"EniIdException\", Status Code: 400, Request Id: \"03de3d24-be9b-42ee-9f0f-d42304cdf60d\""),
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsErrorENINotFound(tt.args.err); got != tt.want {
				t.Errorf("IsErrorENINotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsErrorBBCENIPrivateIPNotFound(t *testing.T) {
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
				err: fmt.Errorf("Error Message: \"The ips [192.168.24.51] is invalid\", Error Code: \"NoSuchObject\", Status Code: 400, Request Id: \"4a3e96c4-e57c-42bc-b479-bf96f4ac12c9\""),
			},
			want: true,
		},
		{
			args: args{
				err: fmt.Errorf("Error Message: \"The param eniId is invalid\", Error Code: \"EniIdException\", Status Code: 400, Request Id: \"03de3d24-be9b-42ee-9f0f-d42304cdf60d\""),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsErrorBBCENIPrivateIPNotFound(tt.args.err); got != tt.want {
				t.Errorf("IsErrorBBCENIPrivateIPNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}

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

package grpc

import (
	"context"

	"google.golang.org/grpc"
)

type Interface interface {
	Dial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)
	DialContext(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error)
}

type cniGRPC struct{}

func New() Interface {
	return &cniGRPC{}
}

func (*cniGRPC) Dial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.Dial(target, opts...)
}

func (*cniGRPC) DialContext(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, target, opts...)
}

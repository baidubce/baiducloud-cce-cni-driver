// Copyright Authors of Baidu AI Cloud
// SPDX-License-Identifier: Apache-2.0

package eniprovider

import (
	"context"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// ENIProvider manages ENI configuration for different platforms
type ENIProvider interface {
	// AllocateENI select an available ENI from the ENI list and assign it
	// to the network endpoint
	// Warning that the provider must implement the resource competition mechanism
	// to ensure that the resource application can be processed correctly when the
	// allocate and deallocate are concurrent.
	AllocateENI(ctx context.Context, endpoint *ccev2.ObjectReference) (*ccev2.ENI, error)

	// ReleaseENI frees up a resource assigned to a network endpoint
	// If no endpoint uses ENI, the provider is also required to return success
	//
	// Before calling this function, please make sure netlink is already in the
	// initial namespace.
	ReleaseENI(ctx context.Context, endpoint *ccev2.ObjectReference) error
}

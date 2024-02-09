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

package allocator

import (
	"context"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam"
	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
)

// AllocatorProvider defines the functions of IPAM provider front-end
// these are implemented by e.g. pkg/ipam/allocator/{aws,azure}.
type AllocatorProvider interface {
	Init(ctx context.Context) error
	Start(ctx context.Context, getterUpdater ipam.NetResourceSetGetterUpdater) (NetResourceSetEventHandler, error)
}

// NetResourceSetEventHandler should implement the behavior to handle NetResourceSet
type NetResourceSetEventHandler interface {
	Create(resource *v2.NetResourceSet) error
	Update(resource *v2.NetResourceSet) error
	Delete(netResourceSetName string) error
	Resync(context.Context, time.Time)
}

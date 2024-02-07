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
package endpoint

import "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/watchers/subscriber"

// K8sEventRegister is used to register and handle events as they are processed
// by K8s controllers.
type K8sEventRegister interface {
	// K8sEventReceived is called to do metrics accounting for received
	// Kubernetes events, as well as calculating timeouts for k8s watcher
	// cache sync.
	K8sEventReceived(apiGroupResourceName string, scope string, action string, valid, equal bool)

	// K8sEventProcessed is called to do metrics accounting for each processed
	// Kubernetes event.
	K8sEventProcessed(scope string, action string, status bool)

	// RegisterNetResourceSetSubscriber allows registration of subscriber.NetResourceSet
	// implementations. Events for all NetResourceSet events (not just the local one)
	// will be sent to the subscriber.
	RegisterNetResourceSetSubscriber(s subscriber.NetResourceSet)
}

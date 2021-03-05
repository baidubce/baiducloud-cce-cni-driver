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

package app

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	nodeagentconfig "github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/node-agent/v1alpha1"
	clientset "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
)

// Options contains everything necessary to create and run a cce-cni-node-agent.
type Options struct {
	// configFile is the location of the node agent's configuration file.
	configFile string
	// config is the node agent's configuration object.
	config *nodeagentconfig.NodeAgentConfiguration
	// hostName is the name of the host that agent runs
	hostName string
	// instanceID is the BCE instanceID of this node.
	instanceID string
	// subnetID is the subnetID of this node.
	subnetID string
	// errCh is the channel that errors will be sent.
	errCh chan error

	metaClient metadata.Interface

	kubeClient kubernetes.Interface

	bceClient cloud.Interface
}

// nodeAgent represents all the parameters required to start the CCE nodeAgent
type nodeAgent struct {
	kubeClient  kubernetes.Interface
	crdClient   clientset.Interface
	cloudClient cloud.Interface
	metaClient  metadata.Interface
	broadcaster record.EventBroadcaster
	recorder    record.EventRecorder
	options     *Options
}

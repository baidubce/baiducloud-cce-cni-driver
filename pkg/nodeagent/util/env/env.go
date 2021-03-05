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

package env

import (
	"context"
	"os"

	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

// nodeNameEnvKey is environment variable.
const (
	nodeNameEnvKey     = "NODE_NAME"
	podNameEnvKey      = "POD_NAME"
	podNamespaceEnvKey = "POD_NAMESPACE"
)

// GetNodeName returns the node's name used in Kubernetes, based on the priority:
// - environment variable NODE_NAME, which should be set by Downward API
// - OS's hostname
func GetNodeName(ctx context.Context) (string, error) {
	nodeName := os.Getenv(nodeNameEnvKey)
	if nodeName != "" {
		return nodeName, nil
	}
	log.Infof(ctx, "environment variable %s not found, using hostname instead", nodeNameEnvKey)
	var err error
	nodeName, err = os.Hostname()
	if err != nil {
		log.Errorf(ctx, "failed to get local hostname: %v", err)
		return "", err
	}
	return nodeName, nil
}

// GetPodName returns name of the Pod where the code executes.
func GetPodName(ctx context.Context) string {
	podName := os.Getenv(podNameEnvKey)
	if podName == "" {
		log.Warningf(ctx, "environment variable %s not found", podNameEnvKey)
	}
	return podName
}

// GetPodNamespace returns Namespace of the Pod where the code executes.
func GetPodNamespace(ctx context.Context) string {
	podNamespace := os.Getenv(podNamespaceEnvKey)
	if podNamespace == "" {
		log.Warningf(ctx, "environment variable %s not found", podNamespaceEnvKey)
	}
	return podNamespace
}

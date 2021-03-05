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

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/cni"
)

const (
	DefaultSysCtlAnnotationPrefix = "net.sysctl.cce.io"
)

// SysCtlConf represents the network sysctl configuration.
type SysCtlConf struct {
	types.NetConf

	SysCtl                      map[string]string `json:"sysctl"`
	KubeConfig                  string            `json:"kubeconfig"`
	SysCtlAnnotationPrefix      string            `json:"sysctlAnnotationPrefix"`
	TuneNonPersistentConnection bool              `json:"tuneNonPersistentConnection"`
}

// K8SArgs k8s pod args
type K8SArgs struct {
	types.CommonArgs `json:"commonArgs"`
	// IP is pod's ip address
	IP net.IP `json:"ip"`
	// K8S_POD_NAME is pod's name
	K8S_POD_NAME types.UnmarshallableString `json:"k8s_pod_name"`
	// K8S_POD_NAMESPACE is pod's namespace
	K8S_POD_NAMESPACE types.UnmarshallableString `json:"k8s_pod_namespace"`
	// K8S_POD_INFRA_CONTAINER_ID is pod's container ID
	K8S_POD_INFRA_CONTAINER_ID types.UnmarshallableString `json:"k8s_pod_infra_container_id"`
}

func loadConf(data []byte) (*SysCtlConf, error) {
	conf := SysCtlConf{}
	if err := json.Unmarshal(data, &conf); err != nil {
		return nil, fmt.Errorf("failed to load netconf: %v", err)
	}

	if conf.SysCtlAnnotationPrefix == "" {
		conf.SysCtlAnnotationPrefix = DefaultSysCtlAnnotationPrefix
	}

	return &conf, nil
}

func loadK8SArgs(envArgs string) (*K8SArgs, error) {
	k8sArgs := K8SArgs{}
	if envArgs != "" {
		err := types.LoadArgs(envArgs, &k8sArgs)
		if err != nil {
			return nil, err
		}
	}
	return &k8sArgs, nil
}

func cmdAdd(args *skel.CmdArgs) error {
	sysctlConf, err := loadConf(args.StdinData)
	if err != nil {
		return err
	}

	k8sArgs, err := loadK8SArgs(args.Args)
	if err != nil {
		return err
	}

	// parse previous result.
	if sysctlConf.RawPrevResult == nil {
		return fmt.Errorf("required prevResult is missing")
	}

	if err := version.ParsePrevResult(&sysctlConf.NetConf); err != nil {
		return err
	}

	// Users can pass sysctl params with cni conf and pod annotations if kubeconfig not empty.
	// Pod annotations will override params in cni conf.
	sysctlResult := map[string]string{}

	if sysctlConf.TuneNonPersistentConnection {
		sysctlResult["net.ipv4.tcp_tw_reuse"] = "1"
		sysctlResult["net.ipv4.ip_local_port_range"] = "1024 65000"
		sysctlResult["net.core.somaxconn"] = "8192"
		sysctlResult["net.ipv4.tcp_max_syn_backlog"] = "8192"
	}

	for k, v := range sysctlConf.SysCtl {
		sysctlResult[k] = v
	}

	name, namespace := string(k8sArgs.K8S_POD_NAME), string(k8sArgs.K8S_POD_NAMESPACE)
	if sysctlConf.KubeConfig != "" {
		kubeClient, err := newKubeClient(sysctlConf.KubeConfig)
		if err != nil {
			return fmt.Errorf("failed to create k8s client with kubeconfig %v: %v", sysctlConf.KubeConfig, err)
		}

		pod, err := kubeClient.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod (%v/%v): %v", namespace, name, err)
		}

		annotations := pod.Annotations
		for k, v := range annotations {
			if isSysCtlKey(k, sysctlConf.SysCtlAnnotationPrefix) {
				sk := extractSysCtlKey(k, sysctlConf.SysCtlAnnotationPrefix)
				// update sysctlResult
				sysctlResult[sk] = v
			}
		}
	}

	// The directory /proc/sys/net is per network namespace. Enter in the
	// network namespace before writing on it.
	err = ns.WithNetNSPath(args.Netns, func(_ ns.NetNS) error {
		for key, value := range sysctlResult {
			// refuse to modify sysctl parameters that don't belong to the network subsystem.
			if !strings.HasPrefix(key, "net") {
				return fmt.Errorf("invalid net sysctl key: %v", key)
			}
			_, err := sysctl.Sysctl(key, value)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	return types.PrintResult(sysctlConf.PrevResult, sysctlConf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// newKubeClient creates a k8s client
func newKubeClient(kubeconfig string) (kubernetes.Interface, error) {
	config, err := buildConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func isSysCtlKey(key string, prefix string) bool {
	return strings.HasPrefix(key, prefix)
}

func extractSysCtlKey(key string, prefix string) string {
	return strings.TrimPrefix(key, prefix+"/")
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, cni.PluginSupportedVersions, bv.BuildString("sysctl"))
}

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

package cmd

import "github.com/spf13/pflag"

type CommonOptions struct {
	KubeConfig      string
	AccessKeyID     string
	SecretAccessKey string
	Region          string
	ClusterID       string
}

func (o *CommonOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.KubeConfig, "kubeconfig", o.KubeConfig, "Path to kubeconfig file with authorization information or empty if in cluster")
	fs.StringVar(&o.AccessKeyID, "access-key", o.AccessKeyID, "BCE OpenApi AccessKeyID")
	fs.StringVar(&o.SecretAccessKey, "secret-access-key", o.AccessKeyID, "BCE OpenApi SecretAccessKey")
	fs.StringVar(&o.Region, "region", o.Region, "BCE OpenApi Region")
	fs.StringVar(&o.ClusterID, "cluster-id", o.ClusterID, "CCE Cluster ID")
}

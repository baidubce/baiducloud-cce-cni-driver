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

package root

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/kubernetes/pkg/client/leaderelectionconfig"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/grpc"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	bbcipam "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/bbc"
	bccipam "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam/bcc"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/version"
)

func NewRootCommand() *cobra.Command {
	options := &Options{
		stopCh:                make(chan struct{}),
		ResyncPeriod:          15 * time.Second,
		ENISyncPeriod:         15 * time.Second,
		GCPeriod:              180 * time.Second,
		Port:                  9997,
		DebugPort:             9998,
		SubnetSelectionPolicy: string(bccipam.SubnetSelectionPolicyMostFreeIP),
		LeaderElection: componentbaseconfig.LeaderElectionConfiguration{
			LeaderElect:       true,
			LeaseDuration:     v1.Duration{time.Second * 15},
			RenewDeadline:     v1.Duration{time.Second * 10},
			RetryPeriod:       v1.Duration{time.Second * 2},
			ResourceLock:      "leases",
			ResourceName:      "cce-ipam",
			ResourceNamespace: "kube-system",
		},
		Debug:                      false,
		IPMutatingRate:             10,
		IPMutatingBurst:            5,
		BatchAddIPNum:              4,
		IdleIPMaxPoolSize:          0,
		IdleIPMinPoolSize:          0,
		AllocateIPConcurrencyLimit: 20,
		ReleaseIPConcurrencyLimit:  30,
	}

	ctx := log.NewContext()

	cmd := &cobra.Command{
		Use: "cce-ipam",
		Run: func(cmd *cobra.Command, args []string) {
			runCommand(ctx, cmd, args, options)
		},
	}

	options.addFlags(cmd.Flags())

	cmd.AddCommand(version.NewVersionCommand())

	return cmd
}

func runCommand(ctx context.Context, cmd *cobra.Command, args []string, opts *Options) {
	log.Info(ctx, "cce-ipam starts...")
	printFlags(cmd.Flags())

	config, err := k8s.BuildConfig(opts.KubeConfig)
	if err != nil {
		log.Fatalf(ctx, "failed to create k8s client config: %v", err)
	}
	kubeClient := kubernetes.NewForConfigOrDie(config)
	crdClient := versioned.NewForConfigOrDie(config)
	bceClient, err := cloud.New(
		opts.Region,
		opts.ClusterID,
		opts.AccessKeyID,
		opts.SecretAccessKey,
		kubeClient,
		opts.Debug,
	)
	if err != nil {
		log.Fatalf(ctx, "failed to create cloud client: %v", err)
	}

	run := func(ctx context.Context) {
		var ipamds [2]ipam.Interface
		var err error

		bccipamd, err := bccipam.NewIPAM(
			kubeClient,
			crdClient,
			bceClient,
			opts.CNIMode,
			opts.VPCID,
			opts.ClusterID,
			bccipam.SubnetSelectionPolicy(opts.SubnetSelectionPolicy),
			opts.IPMutatingRate,
			opts.IPMutatingBurst,
			opts.ResyncPeriod,
			opts.ENISyncPeriod,
			opts.GCPeriod,
			opts.Debug,
		)
		if err != nil {
			log.Fatalf(ctx, "failed to create bcc ipamd: %v", err)
		}

		bbcipamd, err := bbcipam.NewIPAM(
			kubeClient,
			crdClient,
			bceClient,
			opts.CNIMode,
			opts.VPCID,
			opts.ClusterID,
			opts.ResyncPeriod,
			opts.GCPeriod,
			opts.BatchAddIPNum,
			opts.IPMutatingRate,
			opts.IPMutatingBurst,
			opts.IdleIPMinPoolSize,
			opts.IdleIPMaxPoolSize,
			opts.Debug,
		)
		if err != nil {
			log.Fatalf(ctx, "failed to create bbc ipamd: %v", err)
		}

		log.Infof(ctx, "cni mode is: %v", opts.CNIMode)

		switch {
		case types.IsCCECNIModeBasedOnBCCSecondaryIP(opts.CNIMode):
			ipamds = [2]ipam.Interface{bccipamd, nil}
		case types.IsCCECNIModeBasedOnBBCSecondaryIP(opts.CNIMode):
			ipamds = [2]ipam.Interface{bccipamd, bbcipamd}
		default:
			log.Fatalf(ctx, "unsupported cni mode: %v", opts.CNIMode)
		}

		for _, ipamd := range ipamds {
			if ipamd != nil {
				go func(ipamd ipam.Interface) {
					ctx := log.NewContext()
					if err := ipamd.Run(ctx, opts.stopCh); err != nil {
						log.Fatalf(ctx, "ipamd failed to run: %v", err)
					}
				}(ipamd)
			}
		}

		ipamGrpcBackend := grpc.New(
			ipamds[0],
			ipamds[1],
			opts.Port,
			opts.AllocateIPConcurrencyLimit,
			opts.ReleaseIPConcurrencyLimit,
			opts.Debug)

		// run metric server
		go func() {
			runMetricServer(ctx, opts)
		}()

		// run grpc server
		err = ipamGrpcBackend.RunRPCHandler(ctx)
		if err != nil {
			log.Fatalf(ctx, "failed to run ipam grpc server: %v", err)
		}
	}

	if !opts.LeaderElection.LeaderElect {
		run(ctx)
	}

	hostName, err := os.Hostname()
	if err != nil {
		log.Fatalf(ctx, "failed to get hostname: %v", err)
	}

	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id := hostName + "_" + uuid.NewV4().String()

	rlock, err := resourcelock.New(opts.LeaderElection.ResourceLock,
		opts.LeaderElection.ResourceNamespace,
		opts.LeaderElection.ResourceName,
		kubeClient.CoreV1(),
		kubeClient.CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity: id,
		})
	if err != nil {
		log.Fatalf(ctx, "error creating lock: %v", err)
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:          rlock,
		LeaseDuration: opts.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: opts.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   opts.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				log.Fatalf(ctx, "leader election lost")
			},
		},
		Name: "cce-ipam",
	})
}

func runMetricServer(ctx context.Context, opts *Options) {
	address := fmt.Sprintf(":%d", opts.DebugPort)
	log.Infof(ctx, "ipam metrics serving handler on %v", address)

	metric.SetMetricMetaInfo(opts.ClusterID, opts.VPCID)

	metric.RegisterPrometheusMetrics()
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(address, nil)
	if err != nil {
		log.Fatalf(ctx, "failed to run ipam metrics server: %v", err)
	}
}

func (o *Options) addFlags(fs *pflag.FlagSet) {
	// Global
	fs.StringVar(&o.KubeConfig, "kubeconfig", o.KubeConfig, "Path to kubeconfig file with authorization information or empty if in cluster")
	fs.DurationVar(&o.ResyncPeriod, "resync-period", o.ResyncPeriod, "How often configuration from the apiserver is refreshed.  Must be greater than 0")
	// BCE
	fs.StringVar(&o.AccessKeyID, "access-key", o.AccessKeyID, "BCE OpenApi AccessKeyID")
	fs.StringVar(&o.SecretAccessKey, "secret-access-key", o.AccessKeyID, "BCE OpenApi SecretAccessKey")
	fs.StringVar(&o.Region, "region", o.Region, "BCE OpenApi Region")
	fs.StringVar(&o.VPCID, "vpc-id", o.VPCID, "Cluster VPC ID")
	fs.StringVar(&o.ClusterID, "cluster-id", o.ClusterID, "CCE Cluster ID")
	// IPAM
	fs.StringVar((*string)(&o.CNIMode), "cni-mode", string(o.CNIMode), "CNI Mode")
	fs.DurationVar(&o.ENISyncPeriod, "eni-sync-period", o.ENISyncPeriod, "How often to rebuild ENI cache")
	fs.DurationVar(&o.GCPeriod, "gc-period", o.GCPeriod, "How often to gc orphaned pod IP")
	fs.IntVar(&o.Port, "port", o.Port, "gRPC server listen port")
	fs.IntVar(&o.DebugPort, "debug-port", o.DebugPort, "debug server listen port")
	fs.StringVar(&o.SubnetSelectionPolicy, "subnet-selection-policy", o.SubnetSelectionPolicy, "Subnet Selection Policy when creating new ENI. Must be MostFreeIP or LeastENI")
	fs.Float64Var(&o.IPMutatingRate, "ip-mutating-rate", o.IPMutatingRate, "Private IP Mutating Rate")
	fs.Int64Var(&o.IPMutatingBurst, "ip-mutating-burst", o.IPMutatingBurst, "Private IP Mutating Burst")
	fs.IntVar(&o.AllocateIPConcurrencyLimit, "allocate-ip-concurrency-limit", o.AllocateIPConcurrencyLimit, "Allocate IP Concurrency Limit")
	fs.IntVar(&o.ReleaseIPConcurrencyLimit, "release-ip-concurrency-limit", o.ReleaseIPConcurrencyLimit, "Release IP Concurrency Limit")
	fs.IntVar(&o.BatchAddIPNum, "batch-add-ip-num", o.BatchAddIPNum, "Batch Add Private IP Num")
	fs.IntVar(&o.IdleIPMaxPoolSize, "idle-ip-max-pool-size", o.IdleIPMaxPoolSize, "Idle IP Max Pool Size")
	fs.IntVar(&o.IdleIPMinPoolSize, "idle-ip-min-pool-size", o.IdleIPMinPoolSize, "Idle IP Min Pool Size")
	fs.BoolVar(&o.Debug, "debug", o.Debug, "Debug mode")
	leaderelectionconfig.BindFlags(&o.LeaderElection, fs)
}

// printFlags logs the flags in the flagset
func printFlags(flags *pflag.FlagSet) {
	flags.VisitAll(func(flag *pflag.Flag) {
		log.Infof(context.TODO(), "FLAG: --%s=%q", flag.Name, flag.Value)
	})
}

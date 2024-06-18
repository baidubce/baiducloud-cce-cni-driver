//go:build go1.18

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

// Ensure build fails on versions of Go that are not supported by CCE.
// This build tag should be kept in sync with the version specified in go.mod.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/syncer"

	gops "github.com/google/gops/agent"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sys/unix"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/cmd"
	operatorMetrics "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/metrics"
	operatorOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	operatorWatchers "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/components"
	ipamOption "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ipam/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/client"
	k8sversion "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/version"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/pprof"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rand"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/rate"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/version"
)

var (
	leaderElectionResourceLockName = "cce-operator-resource-lock"

	binaryName = filepath.Base(os.Args[0])

	log = logging.DefaultLogger.WithField(logfields.LogSubsys, binaryName)

	rootCmd = &cobra.Command{
		Use:   binaryName,
		Short: "Run " + binaryName,
		Run: func(cmd *cobra.Command, args []string) {
			cmdRefDir := viper.GetString(option.CMDRef)
			if cmdRefDir != "" {
				genMarkdown(cmd, cmdRefDir)
				os.Exit(0)
			}

			// Open socket for using gops to get stacktraces of the agent.
			addr := fmt.Sprintf("127.0.0.1:%d", viper.GetInt(option.GopsPort))
			addrField := logrus.Fields{"address": addr}
			if err := gops.Listen(gops.Options{
				Addr:                   addr,
				ReuseSocketAddrAndPort: true,
			}); err != nil {
				log.WithError(err).WithFields(addrField).Fatal("Cannot start gops server")
			}
			log.WithFields(addrField).Info("Started gops server")

			initEnv()
			runOperator()
		},
	}

	shutdownSignal = make(chan struct{})

	cceK8sClient *k8s.K8sCCEClient

	// identityRateLimiter is a rate limiter to rate limit the number of
	// identities being GCed by the operator. See the documentation of
	// rate.Limiter to understand its difference than 'x/time/rate.Limiter'.
	//
	// With our rate.Limiter implementation CCE will be able to handle bursts
	// of identities being garbage collected with the help of the functionality
	// provided by the 'policy-trigger-interval' in the cce-agent. With the
	// policy-trigger even if we receive N identity changes over the interval
	// set, CCE will only need to process all of them at once instead of
	// processing each one individually.
	identityRateLimiter *rate.Limiter
	// Use a Go context so we can tell the leaderelection code when we
	// want to step down
	leaderElectionCtx, leaderElectionCtxCancel = context.WithCancel(context.Background())

	// isLeader is an atomic boolean value that is true when the Operator is
	// elected leader. Otherwise, it is false.
	isLeader atomic.Value

	doOnce sync.Once
)

func init() {
	rootCmd.AddCommand(cmd.MetricsCmd)
	cmd.Populate()
}

func initEnv() {
	// Prepopulate option.Config with options from CLI.
	option.Config.Populate()
	operatorOption.Config.Populate()

	// add hooks after setting up metrics in the option.Confog
	logging.DefaultLogger.Hooks.Add(metrics.NewLoggingHook(components.CCEOperatortName))

	// Logging should always be bootstrapped first. Do not add any code above this!
	if err := logging.SetupLogging(option.Config.LogDriver, logging.LogOptions(option.Config.LogOpt), binaryName, option.Config.Debug); err != nil {
		log.Fatal(err)
	}

	option.LogRegisteredOptions(log)
	// Enable fallback to direct API probing to check for support of Leases in
	// case Discovery API fails.
	option.Config.EnableK8sLeasesFallbackDiscovery()
}

func initK8s(k8sInitDone chan struct{}) {
	k8s.Configure(
		option.Config.K8sAPIServer,
		option.Config.K8sKubeConfigPath,
		float32(option.Config.K8sClientQPSLimit),
		option.Config.K8sClientBurst,
	)

	if err := k8s.Init(option.Config); err != nil {
		log.WithError(err).Fatal("Unable to connect to Kubernetes apiserver")
	}

	close(k8sInitDone)
}

func doCleanup(exitCode int) {
	// We run the cleanup logic only once. The operator is assumed to exit
	// once the cleanup logic is executed.
	doOnce.Do(func() {
		isLeader.Store(false)
		gops.Close()
		close(shutdownSignal)

		// Cancelling this conext here makes sure that if the operator hold the
		// leader lease, it will be released.
		leaderElectionCtxCancel()

		// If the exit code is set to 0, then we assume that the operator will
		// exit gracefully once the lease has been released.
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	})
}

func main() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, unix.SIGINT, unix.SIGTERM)

	go func() {
		<-signals
		doCleanup(0)
	}()

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getAPIServerAddr() []string {
	if operatorOption.Config.OperatorAPIServeAddr == "" {
		return []string{"127.0.0.1:0", "[::1]:0"}
	}
	return []string{operatorOption.Config.OperatorAPIServeAddr}
}

// checkStatus checks the connection status to the kvstore and
// k8s apiserver and returns an error if any of them is unhealthy
func checkStatus() error {

	if _, err := k8s.Client().Discovery().ServerVersion(); err != nil {
		return err
	}

	return nil
}

// runOperator implements the logic of leader election for cce-operator using
// built-in leader election capbility in kubernetes.
// See: https://github.com/kubernetes/client-go/blob/master/examples/leader-election/main.go
func runOperator() {
	log.Infof("cce ipam v2 Operator %s", version.Version)
	k8sInitDone := make(chan struct{})
	isLeader.Store(false)

	// Configure API server for the operator.
	srv, err := api.NewServer(shutdownSignal, k8sInitDone, getAPIServerAddr()...)
	if err != nil {
		log.WithError(err).Fatalf("Unable to create operator apiserver")
	}

	go func() {
		err = srv.WithStatusCheckFunc(checkStatus).StartServer()
		if err != nil {
			log.WithError(err).Fatalf("Unable to start operator apiserver")
		}
	}()

	if operatorOption.Config.EnableMetrics {
		operatorMetrics.Register()
	}

	if operatorOption.Config.PProf {
		pprof.Enable(operatorOption.Config.PProfPort)
	}

	initK8s(k8sInitDone)

	capabilities := k8sversion.Capabilities()
	if !capabilities.MinimalVersionMet {
		log.Fatalf("Minimal kubernetes version not met: %s < %s",
			k8sversion.Version(), k8sversion.MinimalVersionConstraint)
	}

	// Register the CRDs after validating that we are running on a supported
	// version of K8s.
	if !operatorOption.Config.SkipCRDCreation {
		if err := client.RegisterCRDs(); err != nil {
			log.WithError(err).Fatal("Unable to register CRDs")
		}
	} else {
		log.Info("Skipping creation of CRDs")
	}

	// We only support Operator in HA mode for Kubernetes Versions having support for
	// LeasesResourceLock.
	// See docs on capabilities.LeasesResourceLock for more context.
	if !capabilities.LeasesResourceLock {
		log.Info("Support for coordination.k8s.io/v1 not present, fallback to non HA mode")
		onOperatorStartLeading(leaderElectionCtx)
		return
	}

	// Get hostname for identity name of the lease lock holder.
	// We identify the leader of the operator cluster using hostname.
	operatorID, err := os.Hostname()
	if err != nil {
		log.WithError(err).Fatal("Failed to get hostname when generating lease lock identity")
	}
	operatorID = rand.RandomStringWithPrefix(operatorID+"-", 10)

	ns := option.Config.K8sNamespace
	// If due to any reason the CILIUM_K8S_NAMESPACE is not set we assume the operator
	// to be in default namespace.
	if ns == "" {
		ns = metav1.NamespaceDefault
	}

	leResourceLock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      leaderElectionResourceLockName,
			Namespace: ns,
		},
		Client: k8s.Client().CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			// Identity name of the lock holder
			Identity: operatorID,
		},
	}

	// Start the leader election for running cce-operators
	leaderelection.RunOrDie(leaderElectionCtx, leaderelection.LeaderElectionConfig{
		Name: leaderElectionResourceLockName,

		Lock:            leResourceLock,
		ReleaseOnCancel: true,

		LeaseDuration: operatorOption.Config.LeaderElectionLeaseDuration,
		RenewDeadline: operatorOption.Config.LeaderElectionRenewDeadline,
		RetryPeriod:   operatorOption.Config.LeaderElectionRetryPeriod,

		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: onOperatorStartLeading,
			OnStoppedLeading: func() {
				log.WithField("operator-id", operatorID).Info("Leader election lost")
				// Cleanup everything here, and exit.
				doCleanup(1)
			},
			OnNewLeader: func(identity string) {
				if identity == operatorID {
					log.Info("Leading the operator HA deployment")
				} else {
					log.WithFields(logrus.Fields{
						"newLeader":  identity,
						"operatorID": operatorID,
					}).Info("Leader re-election complete")
				}
			},
		},
	})
}

func startEthernetOperator(ctx context.Context) {
	var (
		netResourceSetManager operatorWatchers.NodeEventHandler
		cceEndpointManager    operatorWatchers.EndpointEventHandler
		sbnHandler            syncer.SubnetEventHandler
		eniHandler            syncer.ENIEventHandler
	)

	log.WithField(logfields.Mode, option.Config.IPAM).Info("Initializing Ethernet IPAM")

	switch ipamMode := option.Config.IPAM; ipamMode {
	case ipamOption.IPAMClusterPool,
		ipamOption.IPAMClusterPoolV2,
		ipamOption.IPAMVpcEni,
		ipamOption.IPAMPrivateCloudBase,
		ipamOption.IPAMVpcRoute:
		alloc, providerBuiltin := allocatorProviders[ipamMode]
		if !providerBuiltin {
			log.Fatalf("%s allocator is not supported by this version of %s", ipamMode, binaryName)
		}

		if err := alloc.Init(ctx); err != nil {
			log.WithError(err).Fatalf("Unable to init %s allocator", ipamMode)
		}

		nrsm, err := alloc.Start(ctx, operatorWatchers.NetResourceSetClient)
		if err != nil {
			log.WithError(err).Fatalf("Unable to start %s allocator", ipamMode)
		}

		netResourceSetManager = nrsm

		// Specify fixed IP assignment
		if ceh, ok := alloc.(endpoint.DirectAllocatorStarter); ok {
			em, err := ceh.StartEndpointManager(ctx, operatorWatchers.CCEEndpointClient)
			if err != nil {
				log.WithError(err).Fatalf("Unable to start %s cce endpoint allocator", ipamMode)
			}
			cceEndpointManager = em
		}

		// task to sync subnet between ipam and cloud
		subnetSyncer, providerBuiltin := subnetSyncerProviders[ipamMode]
		if providerBuiltin {
			err = subnetSyncer.Init(ctx)
			if err != nil {
				log.WithError(err).Fatalf("Unable to init %s cce subnet syncer", ipamMode)
			}
			sbnHandler = subnetSyncer.StartSubnetSyncher(ctx, operatorWatchers.CCESubnetClient)
		}

		// task to sync ENI between ipam and cloud
		eniSyncer, providerBuiltin := eniSyncerProviders[ipamMode]
		if providerBuiltin {
			err = eniSyncer.Init(ctx)
			if err != nil {
				log.WithError(err).Fatalf("Unable to init %s cce eni syncer", ipamMode)
			}
			eniHandler = eniSyncer.StartENISyncer(ctx, operatorWatchers.ENIClient)
		}

	}

	if k8s.IsEnabled() &&
		(operatorOption.Config.RemoveNetResourceSetTaints || operatorOption.Config.SetCCEIsUpCondition) {
		stopCh := make(chan struct{})

		log.WithFields(logrus.Fields{
			logfields.K8sNamespace:               operatorOption.Config.CCEK8sNamespace,
			"label-selector":                     operatorOption.Config.CCEPodLabels,
			"remove-network-resource-set-taints": operatorOption.Config.RemoveNetResourceSetTaints,
			"set-cce-is-up-condition":            operatorOption.Config.SetCCEIsUpCondition,
		}).Info("Removing CCE Node Taints or Setting CCE Is Up Condition for Kubernetes Nodes")

		operatorWatchers.HandleNodeTolerationAndTaints(stopCh)
	}

	if err := operatorWatchers.StartSynchronizingNetResourceSets(ctx, netResourceSetManager); err != nil {
		log.WithError(err).Fatal("Unable to setup node watcher")
	}

	if err := operatorWatchers.StartSynchronizingCCEEndpoint(ctx, cceEndpointManager); err != nil {
		log.WithError(err).Fatal("Unable to setup endpoint watcher")
	}

	if err := operatorWatchers.StartSynchronizingSubnet(ctx, sbnHandler); err != nil {
		log.WithError(err).Fatal("Unable to setup subnet watcher")
	}

	if err := operatorWatchers.StartSynchronizingENI(ctx, eniHandler); err != nil {
		log.WithError(err).Fatal("Unable to setup eni watcher")
	}

	if option.Config.IPAM == ipamOption.IPAMClusterPool ||
		option.Config.IPAM == ipamOption.IPAMClusterPoolV2 ||
		option.Config.IPAM == ipamOption.IPAMVpcRoute {
		// We will use NetResourceSets as the source of truth for the podCIDRs.
		// Once the NetResourceSets are synchronized with the operator we will
		// be able to watch for K8s Node events which they will be used
		// to create the remaining NetResourceSets.
		operatorWatchers.WaitForCacheSync(shutdownSignal)

		// We don't want NetResourceSets that don't have podCIDRs to be
		// allocated with a podCIDR already being used by another node.
		// For this reason we will call Resync after all NetResourceSets are
		// synced with the operator to signal the node manager, since it
		// knows all podCIDRs that are currently set in the cluster, that
		// it can allocate podCIDRs for the nodes that don't have a podCIDR
		// set.
		netResourceSetManager.Resync(ctx, time.Time{})
	}

	if option.Config.IPAM == ipamOption.IPAMVpcEni {
		err := operatorWatchers.StartSynchronizingPSTS(ctx)
		if err != nil {
			log.WithError(err).Fatal("Unable to setup PSTS watcher")
		}
		err = operatorWatchers.StartSynchronizingCPSTS(ctx)
		if err != nil {
			log.WithError(err).Fatal("Unable to setup CPSTS watcher")
		}
	}

	log.Info("Initialization Ethernet IPAM complete")
}

func startRdmaOperator(ctx context.Context) {
	var (
		netResourceSetManager operatorWatchers.NodeEventHandler
		cceEndpointManager    operatorWatchers.EndpointEventHandler
		eniHandler            syncer.ENIEventHandler
	)

	log.WithField(logfields.Mode, option.Config.IPAM).Info("Initializing RDMA IPAM")

	ipamMode := ipamOption.IPAMRdma
	alloc, providerBuiltin := allocatorProviders[ipamMode]
	if !providerBuiltin {
		log.Fatalf("%s allocator is not supported by this version of %s", ipamMode, binaryName)
	}

	if err := alloc.Init(ctx); err != nil {
		log.WithError(err).Fatalf("Unable to init %s allocator", ipamMode)
	}

	nrsm, err := alloc.Start(ctx, operatorWatchers.NetResourceSetClient)
	if err != nil {
		log.WithError(err).Fatalf("Unable to start %s allocator", ipamMode)
	}

	netResourceSetManager = nrsm

	// Specify fixed IP assignment
	if ceh, ok := alloc.(endpoint.DirectAllocatorStarter); ok {
		em, err := ceh.StartEndpointManager(ctx, operatorWatchers.CCEEndpointClient)
		if err != nil {
			log.WithError(err).Fatalf("Unable to start %s cce endpoint allocator", ipamMode)
		}
		cceEndpointManager = em
	}

	// task to sync ENI between ipam and cloud
	eniSyncer, providerBuiltin := eniSyncerProviders[ipamMode]
	if providerBuiltin {
		err = eniSyncer.Init(ctx)
		if err != nil {
			log.WithError(err).Fatalf("Unable to init %s cce eni syncer", ipamMode)
		}
		eniHandler = eniSyncer.StartENISyncer(ctx, operatorWatchers.ENIClient)
	}

	if k8s.IsEnabled() &&
		(operatorOption.Config.RemoveNetResourceSetTaints || operatorOption.Config.SetCCEIsUpCondition) {
		stopCh := make(chan struct{})

		log.WithFields(logrus.Fields{
			logfields.K8sNamespace:               operatorOption.Config.CCEK8sNamespace,
			"label-selector":                     operatorOption.Config.CCEPodLabels,
			"remove-network-resource-set-taints": operatorOption.Config.RemoveNetResourceSetTaints,
			"set-cce-is-up-condition":            operatorOption.Config.SetCCEIsUpCondition,
		}).Info("Removing CCE Node Taints or Setting CCE Is Up Condition for Kubernetes Nodes")

		operatorWatchers.HandleNodeTolerationAndTaints(stopCh)
	}

	if err := operatorWatchers.StartSynchronizingNetResourceSets(ctx, netResourceSetManager); err != nil {
		log.WithError(err).Fatal("Unable to setup node watcher")
	}

	if err := operatorWatchers.StartSynchronizingCCEEndpoint(ctx, cceEndpointManager); err != nil {
		log.WithError(err).Fatal("Unable to setup endpoint watcher")
	}

	if err := operatorWatchers.StartSynchronizingENI(ctx, eniHandler); err != nil {
		log.WithError(err).Fatal("Unable to setup eni watcher")
	}

	log.Info("Initialization RDMA IPAM complete")
}

// onOperatorStartLeading is the function called once the operator starts leading
// in HA mode.
func onOperatorStartLeading(ctx context.Context) {
	isLeader.Store(true)

	// Restart kube-dns as soon as possible since it helps etcd-operator to be
	// properly setup. If kube-dns is not managed by CCE it can prevent
	// etcd from reaching out kube-dns in EKS.
	// If this logic is modified, make sure the operator's clusterrole logic for
	// pods/delete is also up-to-date.
	if option.Config.DisableCCEEndpointCRD {
		log.Infof("KubeDNS unmanaged pods controller disabled as %q option is set to 'disabled' in CCE ConfigMap", option.DisableCCEEndpointCRDName)
	}

	// wait all informer synced
	operatorWatchers.StartWatchers(shutdownSignal)
	operatorWatchers.WaitForCacheSync(shutdownSignal)

	log.WithField(logfields.Mode, option.Config.IPAM).Info("Initializing IPAM")
	startEthernetOperator(ctx)
	// if enabled rdma, start rdma-ipam-operator
	if option.Config.EnableRDMA {
		startRdmaOperator(ctx)
	}

	if operatorOption.Config.NodeGCInterval != 0 {
		operatorWatchers.RunNetResourceSetGC(ctx, operatorOption.Config.NodeGCInterval)
	}
	log.Info("Initialization complete")

	<-shutdownSignal
	// graceful exit
	log.Info("Received termination signal. Shutting down")
}

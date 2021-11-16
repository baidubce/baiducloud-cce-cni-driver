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
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/controller/cniconf"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/controller/eni"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/controller/gc"
	ippoolctrl "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/controller/ippool"
	routectrl "github.com/baidubce/baiducloud-cce-cni-driver/pkg/nodeagent/controller/route"
	utilk8s "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8s"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/k8swatcher"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/version"
)

// NewAgentCommand creates agent command
func NewAgentCommand() *cobra.Command {
	ctx := log.NewContext()
	opts := newOptions()

	cmd := &cobra.Command{
		Use:   "cni-node-agent",
		Short: "cni-node-agent runs on each node and manages container network.",
		Run: func(cmd *cobra.Command, args []string) {
			log.Info(ctx, "cni-node-agent starts...")
			printFlags(cmd.Flags())

			if err := opts.complete(ctx, args); err != nil {
				log.Fatalf(ctx, "failed to complete: %v", err)
			}

			if err := opts.validate(); err != nil {
				log.Fatalf(ctx, "failed to validate: %v", err)
			}

			if err := opts.run(ctx); err != nil {
				log.Fatalf(ctx, "failed to run: %v", err)
			}
		},
	}

	opts.addFlags(cmd.Flags())

	cmd.AddCommand(version.NewVersionCommand())

	return cmd
}

func newNodeAgent(o *Options) (*nodeAgent, error) {
	// k8s Client
	kubeClient, err := utilk8s.NewKubeClient(o.config.Kubeconfig)
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "cni-node-agent", Host: o.hostName})

	// CRD Client
	crdClient, err := utilk8s.NewCRDClient(o.config.Kubeconfig)
	if err != nil {
		return nil, err
	}

	cloudClient, err := cloud.New(
		o.config.CCE.Region,
		o.config.CCE.ClusterID,
		o.config.CCE.AccessKeyID,
		o.config.CCE.SecretAccessKey,
		kubeClient,
		false,
	)
	if err != nil {
		return nil, err
	}

	s := &nodeAgent{
		kubeClient:  kubeClient,
		crdClient:   crdClient,
		cloudClient: cloudClient,
		metaClient:  metadata.NewClient(),
		broadcaster: eventBroadcaster,
		recorder:    recorder,
		options:     o,
	}

	return s, nil
}

func (s *nodeAgent) run(ctx context.Context) error {
	log.Info(ctx, "node agent starts to run...")

	var (
		informerFactory    informers.SharedInformerFactory
		crdInformerFactory crdinformers.SharedInformerFactory
	)

	cniMode := s.options.config.CNIMode
	informerResyncPeriod := time.Duration(s.options.config.ResyncPeriod)

	if s.options.config.CCE.RouteController.EnableStaticRoute {
		// list watch all nodes
		informerFactory = informers.NewSharedInformerFactory(s.kubeClient, informerResyncPeriod)
	} else {
		// only list watch local node
		informerFactory = informers.NewSharedInformerFactoryWithOptions(s.kubeClient, informerResyncPeriod,
			informers.WithTweakListOptions(func(options *metav1.ListOptions) {
				options.FieldSelector = "metadata.name=" + s.options.hostName
			}))
	}

	crdInformerFactory = crdinformers.NewSharedInformerFactoryWithOptions(s.crdClient, informerResyncPeriod,
		crdinformers.WithNamespace(v1.NamespaceDefault),
		crdinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.Kind = "IPPool"
			options.APIVersion = "v1alpha1"
		}),
	)

	// add Informer to factory for future start
	crdInformerFactory.Cce().V1alpha1().IPPools().Informer()

	// Create watcher to watch for k8s resources
	nodeWatcher := k8swatcher.NewNodeWatcher(informerFactory.Core().V1().Nodes(), informerResyncPeriod)

	// check cni mode
	if !types.IsKubenetMode(cniMode) && !types.IsCCECNIMode(cniMode) {
		return fmt.Errorf("unknown cni mode: %v", cniMode)
	}

	// all modes need cni config file except kubenet.
	if !types.IsKubenetMode(cniMode) {
		// cni config controller
		cniConfigCtrl := cniconf.New(
			s.kubeClient,
			crdInformerFactory.Cce().V1alpha1().IPPools().Lister(),
			cniMode,
			s.options.hostName,
			&s.options.config.CNIConfig,
		)
		nodeWatcher.RegisterEventHandler(cniConfigCtrl)
		go cniConfigCtrl.ReconcileCNIConfig()
	}

	// all modes needs ippool controller
	ippoolCtrl := ippoolctrl.New(
		s.kubeClient,
		s.cloudClient,
		s.crdClient,
		s.options.config.CNIMode,
		s.options.hostName,
		s.options.instanceID,
		s.options.instanceType,
		s.options.config.CCE.ENIController.ENISubnetList,
		s.options.config.CCE.ENIController.SecurityGroupList,
		s.options.config.CCE.PodSubnetController.SubnetList,
	)
	nodeWatcher.RegisterEventHandler(ippoolCtrl)

	// all modes runs gc
	gcCtrl := gc.New()
	go gcCtrl.GC()

	// eni controller
	eniCtrl := eni.New(
		s.cloudClient,
		s.metaClient,
		s.kubeClient,
		s.crdClient,
		s.recorder,
		s.options.config.CCE.ClusterID,
		s.options.hostName,
		s.options.instanceID,
		s.options.config.CCE.VPCID,
		s.options.config.CCE.ENIController.RouteTableOffset,
		time.Duration(s.options.config.CCE.ENIController.ENISyncPeriod),
	)

	switch {
	case types.IsKubenetMode(cniMode), types.IsCCECNIModeBasedOnVPCRoute(cniMode):
		s.runCCEModeBasedOnVPCRoute(ctx, nodeWatcher)
	case types.IsCCECNIModeBasedOnBCCSecondaryIP(cniMode):
		s.runCCEModeBasedOnBCCSecondaryIP(ctx, nodeWatcher, eniCtrl)
	case types.IsCCECNIModeBasedOnBBCSecondaryIP(cniMode):
		s.runCCEModeBasedOnBBCSecondaryIP(ctx, nodeWatcher, eniCtrl)
	}

	// This has to start after the calls to NewXXXWatcher  because those
	// functions must configure their shared informer event handlers first.
	informerFactory.Start(wait.NeverStop)
	crdInformerFactory.Start(wait.NeverStop)

	log.Infof(ctx, "waiting for informer caches to sync")

	// WaitCacheSync
	informerFactory.WaitForCacheSync(wait.NeverStop)
	crdInformerFactory.WaitForCacheSync(wait.NeverStop)

	log.Infof(ctx, "informer caches are synced")

	nodeWatcher.Run(s.options.config.Workers, wait.NeverStop)

	return nil
}

func (s *nodeAgent) runCCEModeBasedOnVPCRoute(ctx context.Context, nodeWatcher *k8swatcher.NodeWatcher) {
	routeController, err := routectrl.NewRouteController(
		s.kubeClient,
		s.recorder,
		s.cloudClient,
		s.crdClient,
		s.options.hostName,
		s.options.instanceID,
		s.options.config.CCE.ClusterID,
		s.options.config.CCE.RouteController.EnableVPCRoute,
		s.options.config.CCE.RouteController.EnableStaticRoute,
		s.options.config.CCE.ContainerNetworkCIDRIPv4,
		s.options.config.CCE.ContainerNetworkCIDRIPv6,
	)
	if err != nil {
		log.Errorf(ctx, "failed to create route controller: %v", err)
	} else {
		nodeWatcher.RegisterEventHandler(routeController)
	}
}

func (s *nodeAgent) runCCEModeBasedOnBCCSecondaryIP(
	ctx context.Context,
	nodeWatcher *k8swatcher.NodeWatcher,
	eniController *eni.Controller,
) {
	go eniController.ReconcileENIs()
}

func (s *nodeAgent) runCCEModeBasedOnBBCSecondaryIP(
	ctx context.Context,
	nodeWatcher *k8swatcher.NodeWatcher,
	eniController *eni.Controller,
) {
	if s.options.instanceType == metadata.InstanceTypeExBCC {
		log.Infof(ctx, "instance type is %v via metadata, will run eni controller", s.options.instanceType)
		go eniController.ReconcileENIs()
	}

	if s.options.instanceType == metadata.InstanceTypeExBBC {
		patchErr := utilk8s.UpdateNetworkingCondition(
			ctx,
			s.kubeClient,
			s.options.hostName,
			true,
			"BBCReady",
			"BBCNotReady",
			"CCE Controller reconciles BBC",
			"CCE Controller failed to reconcile BBC",
		)
		if patchErr != nil {
			log.Errorf(ctx, "bbc: update networking condition for node %v error: %v", s.options.hostName, patchErr)
		}
	}
}

// printFlags logs the flags in the flagset
func printFlags(flags *pflag.FlagSet) {
	flags.VisitAll(func(flag *pflag.Flag) {
		log.Infof(context.TODO(), "FLAG: --%s=%q", flag.Name, flag.Value)
	})
}

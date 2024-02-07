/*
Copyright (c) 2023 Baidu, Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-ext-eip/api/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-ext-eip/internal/controller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ipamv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v2.AddToScheme(scheme))
	utilruntime.Must(ipamv2.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var region string
	var cceClusterID string
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&region, "region", "", "Region")
	flag.StringVar(&cceClusterID, "cce-cluster-id", "", "CCE Cluster ID")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", true,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	LeaderElectionID, err := generateLeaderElectionID()
	if err != nil {
		setupLog.Error(err, "failed to generate LeaderElectionID")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       LeaderElectionID,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := initK8sClient(); err != nil {
		setupLog.Error(err, "failed to initialize k8s client")
		os.Exit(1)
	}

	if err := v2.InitCert(); err != nil {
		setupLog.Error(err, "failed to initialize cert for webhook")
		os.Exit(1)
	}

	// TODO: eip GC(eip was deleted from PEBS.Status.StaticPool)
	if err = (&controller.PodEIPBindStrategyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PodEIPBindStrategy")
		os.Exit(1)
	}

	if err = (&controller.EIPReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		Region:       region,
		CCEClusterID: cceClusterID,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "EIP")
		os.Exit(1)
	}

	mgr.GetWebhookServer().Register("/mutate-cce-baidubce-com-v2-cceendpoint",
		&webhook.Admission{Handler: &v2.CepAnnotator{Client: mgr.GetClient()}})

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func generateLeaderElectionID() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	id := fmt.Sprintf("%06d", rand.Intn(1000000))

	return hostname + "-" + id, nil
}

func initK8sClient() error {
	// TODO: this is for debug in local, remove this when run in a cluster
	// var kubeconfigPath string
	// if home := homedir.HomeDir(); home != "" {
	// 	kubeconfigPath = filepath.Join(home, ".kube", "config")
	// } else {
	// 	return fmt.Errorf("failed to get HOME dir")
	// }
	// k8s.Configure(
	// 	"",             /*K8sAPIServer*/
	// 	kubeconfigPath, /*K8sKubeConfigPath*/
	// 	5,              /*float32(option.Config.K8sClientQPSLimit)*/
	// 	10,             /*K8sClientBurst*/
	// )

	_, _, err := k8s.InitNewK8sClient()
	if err != nil {
		return err
	}

	return nil
}

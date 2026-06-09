package main

import (
	goflag "flag"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"
	controllerruntime "sigs.k8s.io/controller-runtime"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/webhook/webhook"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/webhook/webhook/podmutating"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/webhook/webhook/pstsmutating"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/webhook/webhookcontroller"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var metricsPort int = 18902

func NewRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use: "webhook",
		Run: func(cmd *cobra.Command, args []string) {
			klog.Info("cce-ipam-webhook starts...")
			cmd.Flags().VisitAll(func(flag *pflag.Flag) {
				klog.Infof("FLAG: --%s=%q", flag.Name, flag.Value)
			})
			k8s.Configure(webhookcontroller.APIServerURL, webhookcontroller.Kubeconfig, float32(webhookcontroller.Qps), webhookcontroller.Burst)
			_, _, err := k8s.InitNewK8sClient()
			if err != nil {
				klog.Fatalf("create k8s client failed: %v", err)
			}
			if err := pstsmutating.Init(); err != nil {
				klog.Fatalf("init psts webhook failed: %v", err)
			}
			go runMetricServer()

			controllerruntime.SetLogger(klog.NewKlogr())
			if err := webhook.RunWebhookServer(); err != nil {
				klog.Fatalf("webhook to run: %v", err)
			}
		},
	}

	webhook.RegisterWebhookFlags(cmd.Flags())
	podmutating.RegisterCustomerFlags(cmd.Flags())
	cmd.Flags().IntVar(&metricsPort, "metrics-port", metricsPort, "port for webhook server")
	return cmd
}

func runMetricServer() {
	address := fmt.Sprintf(":%d", metricsPort)
	klog.Infof("webhook metrics serving handler on %v", address)

	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(address, nil)
	if err != nil {
		klog.Errorf("failed to run webhook metrics server: %v", err)
	}
}

func main() {
	rand.Seed(time.Now().UnixNano())
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	command := NewRootCommand()
	if err := command.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	klog.InitFlags(nil)
}

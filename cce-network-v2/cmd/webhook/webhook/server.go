// Copyright 2022 Baidu Inc. All Rights Reserved.
// ~/pkg/webhook/server.go - webhook server

// modification history
// --------------------
// 2022/05/23, by wangeweiwei22, create server

// initialize webhook server for kubernetes

package webhook

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/webhook/webhookcontroller"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type GateFunc func() (enabled bool)

var (
	// HandlerMap contains all admission webhook handlers.
	HandlerMap   = map[string]admission.Handler{}
	handlerGates = map[string]GateFunc{}
	// flags for webhook
	port = 18921

	scheme = runtime.NewScheme()

	// The global webhook server is used to inject various controllers
	// into the webhhok processor
	globalWebookServer *webhook.Server
	onece              sync.Once
)

func RegisterWebhookFlags(pset *pflag.FlagSet) {
	pset.IntVar(&port, "webhook-port", port, "port for webhook server")
	webhookcontroller.RegisterWebhookFlags(pset)
}

func addHandlers(m map[string]admission.Handler) {
	addHandlersWithGate(m, nil)
}

func addHandlersWithGate(m map[string]admission.Handler, fn GateFunc) {
	for path, handler := range m {
		if len(path) == 0 {
			klog.Warningf("Skip handler with empty path.")
			continue
		}
		if path[0] != '/' {
			path = "/" + path
		}
		_, found := HandlerMap[path]
		if found {
			klog.V(1).Infof("conflicting webhook builder path %v in handler map", path)
		}
		HandlerMap[path] = handler
		if fn != nil {
			handlerGates[path] = fn
		}
	}
}

// RunWebhookServer Run a new stand-alone webhook server
// The webhook server has a matching controller, which can automatically
// update the certificate configuration of webhook. Including updating
// the configuration information of webhookconfiguration and secret

// Running this function will block the current coroutine
func RunWebhookServer() error {
	server := standaloneWebhookServer()
	err := initialize()
	if err != nil {
		return err
	}
	_ = clientgoscheme.AddToScheme(scheme)
	logger := klog.NewKlogr()
	injectFunc := func(i interface{}) error {
		if _, err := inject.SchemeInto(scheme, i); err != nil {
			return err
		}
		if _, err = inject.LoggerInto(logger, i); err != nil {
			return err
		}
		return nil
	}

	server.InjectFunc(func(i interface{}) error {
		if _, err := inject.InjectorInto(injectFunc, i); err != nil {
			return err
		}
		return injectFunc(i)
	})
	onece.Do(func() {
		if globalWebookServer != nil {
			globalWebookServer = server
		}
	})

	return server.Start(context.TODO())
}

// standaloneWebhookServer runs a webhook server without
// a controller manager.
func standaloneWebhookServer() *webhook.Server {
	server := &webhook.Server{
		Port:    port,
		Host:    "0.0.0.0",
		CertDir: webhookcontroller.CertDir,
	}
	// register admission handlers
	filterActiveHandlers()
	for path, handler := range HandlerMap {
		server.Register(path, &webhook.Admission{Handler: handler})
		klog.V(3).Infof("Registered webhook handler %s", path)
	}
	return server
}

func filterActiveHandlers() {
	disablePaths := sets.NewString()
	for path := range HandlerMap {
		if fn, ok := handlerGates[path]; ok {
			if !fn() {
				disablePaths.Insert(path)
			}
		}
	}
	for _, path := range disablePaths.List() {
		delete(HandlerMap, path)
	}
}

func initialize() error {
	// add secret cache for webhook
	if port == 0 {
		return nil
	}

	k8s.CCEClient().Informers.Cce().V2().CCEEndpoints().Informer()
	k8s.CCEClient().Informers.Cce().V2().PodSubnetTopologySpreads().Informer()
	k8s.CCEClient().Informers.Cce().V1().Subnets().Informer()
	k8s.CCEClient().Informers.Start(wait.NeverStop)
	ctx, cancel := context.WithTimeout(context.TODO(), time.Minute*5)
	k8s.CCEClient().Informers.WaitForCacheSync(ctx.Done())
	cancel()
	c, err := webhookcontroller.New(HandlerMap)
	if err != nil {
		return err
	}
	go func() {
		c.Start(context.Background())
	}()

	timer := time.NewTimer(time.Second * 30)
	defer timer.Stop()
	select {
	case <-webhookcontroller.Inited():
		return nil
	case <-timer.C:
		return fmt.Errorf("failed to start webhook controller for waiting more than 30s")
	}
}

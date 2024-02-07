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

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/runtime/inject"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/webhookcontroller"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/webhook/health"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/webhook/injector"
	podmutating "github.com/baidubce/baiducloud-cce-cni-driver/pkg/webhook/pod/mutating"
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
	podmutating.RegisterPodMutatingWebhookFlags(pset)
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
func RunWebhookServer(
	config *rest.Config,
	kubeClient kubernetes.Interface,
	crdClient versioned.Interface,
	kubeInformer informers.SharedInformerFactory,
	crdInformer crdinformers.SharedInformerFactory,
	bceClient cloud.Interface,
	stopChan chan struct{},
	cniMode types.ContainerNetworkMode,
) error {
	server := standaloneWebhookServer()

	_ = clientgoscheme.AddToScheme(scheme)
	_ = networkingv1alpha1.AddToScheme(scheme)

	injectFunc := func(i interface{}) error {
		if _, err := inject.SchemeInto(scheme, i); err != nil {
			return err
		}
		if _, err := injector.CrdClientInto(crdClient, i); err != nil {
			return err
		}
		if _, err := injector.CloudClientInto(bceClient, i); err != nil {
			return err
		}
		if _, err := injector.KubeClientInto(kubeClient, i); err != nil {
			return err
		}
		if _, err := injector.KubeInformerInto(kubeInformer, i); err != nil {
			return err
		}
		if _, err := injector.NetworkInformerInto(crdInformer, i); err != nil {
			return err
		}
		if _, err := injector.CNIModeInto(cniMode, i); err != nil {
			return err
		}
		return nil
	}
	// decoder, _ := admission.NewDecoder(scheme)
	// inject crd client
	server.InjectFunc(func(i interface{}) error {
		if _, err := inject.InjectorInto(injectFunc, i); err != nil {
			return err
		}
		return injectFunc(i)
	})
	err := initialize(config)
	if err != nil {
		return err
	}

	onece.Do(func() {
		if globalWebookServer != nil {
			globalWebookServer = server
		}
	})
	return server.Start(stopChan)
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

	// register conversion webhook
	server.Register("/convert", &conversion.Webhook{})

	// register health handler
	server.Register("/healthz", &health.Handler{})
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

func initialize(config *rest.Config) error {
	// add secret cache for webhook
	if port == 0 {
		return nil
	}

	c, err := webhookcontroller.New(config, HandlerMap)
	if err != nil {
		return err
	}
	go func() {
		c.Start(context.Background())
	}()

	timer := time.NewTimer(time.Second * 20)
	defer timer.Stop()
	select {
	case <-webhookcontroller.Inited():
		return nil
	case <-timer.C:
		return fmt.Errorf("failed to start webhook controller for waiting more than 20s")
	}
}

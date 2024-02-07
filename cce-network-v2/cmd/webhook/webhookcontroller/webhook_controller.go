/*
Copyright 2020 The Kruise Authors.

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

package webhookcontroller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/webhook/webhookcontroller/writer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"

	"github.com/spf13/pflag"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	defaultResyncPeriod   = time.Minute
	webhookControllerName = "webhook-controller"
)

var (
	Namespace = "kube-system"

	serviceName = "cce-ipam-v2"
	secretName  = "cce-ipam-v2-webhook-certs"
	CertDir     = "/home/cce/config/certs"

	uninit   = make(chan struct{})
	onceInit = sync.Once{}
)

func RegisterWebhookFlags(pset *pflag.FlagSet) {
	pset.StringVar(&CertDir, "webhook-certs", CertDir, "cert folder for webhook server")
	pset.StringVar(&Namespace, "namespace", Namespace, "service namespace")
	pset.StringVar(&serviceName, "webhook-service-name", serviceName, "service name for webhook server")
	pset.StringVar(&secretName, "webhook-secret-name", secretName, "secret name for webhook server to store certs")
	pset.StringVar(&mutatingWebhookConfigurationName, "mutating-webhook-configuration-name", mutatingWebhookConfigurationName, "name of mutating configuration object for webhook server")
	pset.StringVar(&validatingWebhookConfigurationName, "validting-webhook-configuration-name", validatingWebhookConfigurationName, "name of validting configuration object for webhook server")
}

func Inited() chan struct{} {
	return uninit
}

type Controller struct {
	kubeClient clientset.Interface
	handlers   map[string]admission.Handler

	informerFactory informers.SharedInformerFactory
	synced          []cache.InformerSynced

	queue workqueue.RateLimitingInterface
}

func New(handlers map[string]admission.Handler) (*Controller, error) {
	cs := k8s.WatcherClient()

	c := &Controller{
		kubeClient: cs,
		handlers:   handlers,
		queue:      workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "cce-eni-ipam-webhook-controller"),
	}

	c.informerFactory = cs.Informers

	secretInformer := cs.Informers.Core().V1().Secrets()
	admissionRegistrationInformer := cs.Informers.Admissionregistration().V1()
	//c.secretLister = secretInformer.Lister().Secrets(namespace)
	//c.mutatingWCLister = admissionRegistrationInformer.MutatingWebhookConfigurations().Lister()
	//c.validatingWCLister = admissionRegistrationInformer.ValidatingWebhookConfigurations().Lister()

	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			secret := obj.(*v1.Secret)
			if secret.Name == secretName {
				klog.Infof("Secret %s added", secretName)
				c.queue.Add("")
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			secret := cur.(*v1.Secret)
			if secret.Name == secretName {
				klog.Infof("Secret %s updated", secretName)
				c.queue.Add("")
			}
		},
	})

	admissionRegistrationInformer.MutatingWebhookConfigurations().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			conf := obj.(*admissionregistrationv1.MutatingWebhookConfiguration)
			if conf.Name == mutatingWebhookConfigurationName {
				klog.Infof("MutatingWebhookConfiguration %s added", mutatingWebhookConfigurationName)
				c.queue.Add("")
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			conf := cur.(*admissionregistrationv1.MutatingWebhookConfiguration)
			if conf.Name == mutatingWebhookConfigurationName {
				klog.Infof("MutatingWebhookConfiguration %s update", mutatingWebhookConfigurationName)
				c.queue.Add("")
			}
		},
	})

	admissionRegistrationInformer.ValidatingWebhookConfigurations().Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			conf := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
			if validatingWebhookConfigurationName != "" && conf.Name == validatingWebhookConfigurationName {
				klog.Infof("ValidatingWebhookConfiguration %s added", validatingWebhookConfigurationName)
				c.queue.Add("")
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			conf := cur.(*admissionregistrationv1.ValidatingWebhookConfiguration)
			if validatingWebhookConfigurationName != "" && conf.Name == validatingWebhookConfigurationName {
				klog.Infof("ValidatingWebhookConfiguration %s updated", validatingWebhookConfigurationName)
				c.queue.Add("")
			}
		},
	})

	c.synced = []cache.InformerSynced{
		secretInformer.Informer().HasSynced,
		admissionRegistrationInformer.MutatingWebhookConfigurations().Informer().HasSynced,
		admissionRegistrationInformer.ValidatingWebhookConfigurations().Informer().HasSynced,
	}

	return c, nil
}

// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;update;patch

func (c *Controller) Start(ctx context.Context) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting webhook-controller")
	defer klog.Infof("Shutting down webhook-controller")

	c.informerFactory.Start(ctx.Done())
	if !cache.WaitForNamedCacheSync("webhook-controller", ctx.Done(), c.synced...) {
		return
	}

	go wait.Until(func() {
		for c.processNextWorkItem() {
		}
	}, time.Second, ctx.Done())
	klog.Infof("Started webhook-controller")

	<-ctx.Done()
}

func (c *Controller) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.sync()
	if err == nil {
		c.queue.AddAfter(key, defaultResyncPeriod)
		c.queue.Forget(key)
		return true
	}

	utilruntime.HandleError(fmt.Errorf("sync %q failed with %v", key, err))
	c.queue.AddRateLimited(key)

	return true
}

func (c *Controller) sync() error {
	klog.V(5).Infof("Starting to sync webhook certs and configurations")
	defer func() {
		klog.V(5).Infof("Finished to sync webhook certs and configurations")
	}()

	var dnsName string
	var certWriter writer.CertWriter
	var err error

	dnsName = fmt.Sprintf("%s.%s.svc", serviceName, Namespace)

	certWriter, err = writer.NewSecretCertWriter(writer.SecretCertWriterOptions{
		Clientset: c.kubeClient,
		Secret:    &types.NamespacedName{Namespace: Namespace, Name: secretName},
	})
	if err != nil {
		return fmt.Errorf("failed to ensure certs: %v", err)
	}

	certs, _, err := certWriter.EnsureCert(dnsName)
	if err != nil {
		return fmt.Errorf("failed to ensure certs: %v", err)
	}
	if err := writer.WriteCertsToDir(CertDir, certs); err != nil {
		return fmt.Errorf("failed to write certs to dir: %v", err)
	}

	if err := configurationEnsure(c.kubeClient, c.handlers, certs.CACert); err != nil {
		return fmt.Errorf("failed to ensure configuration: %v", err)
	}

	onceInit.Do(func() {
		close(uninit)
	})
	return nil
}

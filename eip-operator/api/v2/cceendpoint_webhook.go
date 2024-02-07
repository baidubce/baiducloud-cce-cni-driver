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

package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/cmd/webhook/webhookcontroller/writer"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ipamv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	xslices "golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	FeatureKeyOfPublicIP = "publicIP"

	namespace           = "kube-system"
	serviceName         = "eip-webhook-service"
	secretName          = "eip-webhook-server-cert"
	mutatingWebhookName = "eip-mutating-webhook-configuration"
	certDir             = "/tmp/k8s-webhook-server/serving-certs"
)

// log is for logging in this package.
var ceplog = logf.Log.WithName("CCEEndpoint")

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:webhook:path=/mutate-cce-baidubce-com-v2-cceendpoint,mutating=true,failurePolicy=fail,sideEffects=None,groups=cce.baidubce.com,resources=cceendpoints,verbs=create,versions=v2,name=mcceendpoint.kb.io,admissionReviewVersions=v1

//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:object:generate=false
type CepAnnotator struct {
	Client  client.Client
	decoder *admission.Decoder
}

func (a *CepAnnotator) Handle(ctx context.Context, req admission.Request) admission.Response {
	cep := &ipamv2.CCEEndpoint{}
	err := a.decoder.Decode(req, cep)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// ceplog.Info("mutate begin", "Operation", req.Operation, "CR", cep)
	a.mutate(ctx, req, cep)
	// ceplog.Info("mutate end", "CR", cep)

	marshaledCep, err := json.Marshal(cep)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledCep)
}

func (a *CepAnnotator) mutate(ctx context.Context, req admission.Request, cep *ipamv2.CCEEndpoint) {
	// get PodEIPBindStrategy for CCEEndpoint
	pebsList := PodEIPBindStrategyList{}
	if err := a.Client.List(ctx, &pebsList, client.InNamespace(req.Namespace)); err != nil {
		ceplog.Error(err, "mutate, failed to list PEBS")
		return
	}
	pebsIndex := -1
	for i := 0; i < len(pebsList.Items) && pebsIndex == -1; i++ {
		if PebsMatchCep(&pebsList.Items[i], cep) {
			pebsIndex = i
		}
	}
	if pebsIndex == -1 {
		ceplog.Info("mutate", "no matched PodEIPBindStrategy for CCEEndpoint", cep.Namespace+"/"+cep.Name)
		return
	}
	pebs := &pebsList.Items[pebsIndex]
	ceplog.Info("mutate", "got matched PodEIPBindStrategy", pebs.Namespace+"/"+pebs.Name,
		"CCEEndpoint", cep.Namespace+"/"+cep.Name)

	// add extFeatureGates: publicIP
	if !xslices.Contains(cep.Spec.ExtFeatureGates, FeatureKeyOfPublicIP) {
		cep.Spec.ExtFeatureGates = append(cep.Spec.ExtFeatureGates, FeatureKeyOfPublicIP)
		ceplog.Info("mutate, add publicIP to extFeatureGate")
	}

	// add finalizers to unbind EIP
	if !xslices.Contains(cep.Finalizers, FeatureKeyOfPublicIP) {
		cep.Finalizers = append(cep.Finalizers, FeatureKeyOfPublicIP)
		ceplog.Info("mutate, add publicIP finalizer")
	}
}

func (a *CepAnnotator) InjectDecoder(d *admission.Decoder) error {
	a.decoder = d
	return nil
}

func PebsMatchCep(pebs *PodEIPBindStrategy, cep *ipamv2.CCEEndpoint) bool {
	if pebs.Spec.Selector == nil {
		return false
	}

	selector, err := metav1.LabelSelectorAsSelector(pebs.Spec.Selector)
	if err != nil {
		return false
	}
	if selector.Empty() || !selector.Matches(labels.Set(cep.Labels)) {
		return false
	}
	return true
}

func InitCert() error {
	var dnsName string
	var certWriter writer.CertWriter
	var err error

	dnsName = fmt.Sprintf("%s.%s.svc", serviceName, namespace)

	certWriter, err = writer.NewSecretCertWriter(writer.SecretCertWriterOptions{
		Clientset: k8s.WatcherClient(),
		Secret:    &types.NamespacedName{Namespace: namespace, Name: secretName},
	})
	if err != nil {
		return fmt.Errorf("failed to ensure certs: %v", err)
	}

	certs, _, err := certWriter.EnsureCert(dnsName)
	if err != nil {
		return fmt.Errorf("failed to ensure certs: %v", err)
	}
	if err := writer.WriteCertsToDir(certDir, certs); err != nil {
		return fmt.Errorf("failed to write certs to dir: %v", err)
	}

	if err := setupBundle(k8s.WatcherClient(), certs.CACert); err != nil {
		return fmt.Errorf("failed to setup Bundle: %v", err)
	}

	return nil
}

func setupBundle(kubeClient clientset.Interface, caBundle []byte) error {
	mutatingConfig, err := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(),
		mutatingWebhookName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to find MutatingWebhookConfiguration %s", mutatingWebhookName)
	}

	mutatingConfig.Webhooks[0].ClientConfig.CABundle = caBundle
	if _, err := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.TODO(),
		mutatingConfig, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update Bundle for %s: %v", mutatingWebhookName, err)
	}

	return nil
}

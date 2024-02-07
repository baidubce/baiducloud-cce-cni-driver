package mutating

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	admissionv1 "k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/config/types"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	networkinformer "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/webhook/injector"
	"github.com/spf13/pflag"
)

var (
	// HandlerMap contains admission webhook handlers
	HandlerMap = map[string]admission.Handler{
		"mutating-pod-subnet-topology-spread": &MutatingPodHandler{},
	}

	enablePodSubnetTopologyWebhook    = true
	enablePodIPResourcesWebhook       = true
	enablePodTopologySchedulerWebhook = true
)

func RegisterPodMutatingWebhookFlags(pflags *pflag.FlagSet) {
	pflags.BoolVar(&enablePodSubnetTopologyWebhook, "enable-pod-subnet-topology-webhook", enablePodSubnetTopologyWebhook, "add psts to pod annotation while create pod")
	pflags.BoolVar(&enablePodIPResourcesWebhook, "enable-pod-ip-resources-webhook", enablePodIPResourcesWebhook, "add ip resources to pod requests and limits while create pod")
	pflags.BoolVar(&enablePodTopologySchedulerWebhook, "enable-pod-topology-scheduler-webhook", enablePodTopologySchedulerWebhook, "add topology spread and node affinity to pod annotation while create pod")
}

type MutatingPodHandler struct {
	crdClient      versioned.Interface
	subnetInformer networkinformer.SubnetInformer
	pstsInformer   networkinformer.PodSubnetTopologySpreadInformer
	bceClient      cloud.Interface
	// Decoder decodes objects
	Decoder *admission.Decoder

	cniMode types.ContainerNetworkMode
}

// Handle handles admission requests.
func (h *MutatingPodHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	obj := &corev1.Pod{}

	if len(req.AdmissionRequest.SubResource) > 0 ||
		(req.AdmissionRequest.Operation != admissionv1.Create && req.AdmissionRequest.Operation != admissionv1.Update) ||
		req.AdmissionRequest.Resource.Resource != "pods" {
		return admission.Allowed("")
	}
	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	klog.V(5).Infof("receive mutating %s pod (%s/%s)", req.Operation, obj.Namespace, obj.Name)

	if obj.Namespace == "" {
		obj.Namespace = req.Namespace
	}
	if obj.Annotations == nil {
		obj.Annotations = map[string]string{}
	}
	if obj.Labels == nil {
		obj.Labels = map[string]string{}
	}

	pod := obj.DeepCopy()
	switch req.Operation {
	case admissionv1.Create:
		if !types.IsCCECNIModeBasedOnBCCSecondaryIP(h.cniMode) &&
			!types.IsCCECNIModeBasedOnBBCSecondaryIP(h.cniMode) {
			break
		}

		// psts should not add affinity to pod which use hostNetwork
		if enablePodSubnetTopologyWebhook && !pod.Spec.HostNetwork {
			psts, err := h.addRelatedPodSubnetTopologySpread(ctx, pod)
			if err != nil {
				return admission.Errored(500, err)
			}
			if enablePodTopologySchedulerWebhook {
				if err = h.addPodTopologySpread(ctx, pod, psts); err != nil {
					return admission.Errored(500, err)
				}
			}
		}
		if enablePodIPResourcesWebhook {
			h.addIPResource2Pod(ctx, pod)
		}
	}

	if reflect.DeepEqual(obj, pod) {
		return admission.Allowed("")
	}
	klog.V(3).Infof("mutating %s pod (%s/%s) with psts success", req.Operation, obj.Namespace, obj.Name)
	marshalled, err := json.Marshal(pod)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}

var _ admission.DecoderInjector = &MutatingPodHandler{}

// InjectDecoder injects the decoder into the PodCreateHandler
func (h *MutatingPodHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}

var _ injector.CRDClient = &MutatingPodHandler{}

func (h *MutatingPodHandler) InjectCRDClient(crdClient versioned.Interface) error {
	h.crdClient = crdClient
	return nil
}

var _ injector.CloudClientInject = &MutatingPodHandler{}

func (h *MutatingPodHandler) InjectCloudClient(bceClient cloud.Interface) error {
	h.bceClient = bceClient
	return nil
}

var _ injector.NetworkInformerInject = &MutatingPodHandler{}

func (h *MutatingPodHandler) InjectNetworkInformer(networkInformer crdinformers.SharedInformerFactory) error {
	h.subnetInformer = networkInformer.Cce().V1alpha1().Subnets()
	h.pstsInformer = networkInformer.Cce().V1alpha1().PodSubnetTopologySpreads()
	return nil
}

var _ injector.CNIModeInject = &MutatingPodHandler{}

func (h *MutatingPodHandler) InjectCNIMode(cniMode types.ContainerNetworkMode) error {
	h.cniMode = cniMode
	return nil
}

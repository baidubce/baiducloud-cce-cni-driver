package podmutating

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/endpoint/ewebhook"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var (
	// HandlerMap contains admission webhook handlers
	HandlerMap = map[string]admission.Handler{
		"mutating-pod": &MutatingPodHandler{},
	}

	enablePodSubnetTopologyWebhook    = true
	enablePodIPResourcesWebhook       = true
	enablePodTopologySchedulerWebhook = true

	log = logging.NewSubysLogger("podmutating-webhook")
)

type MutatingPodHandler struct {
	epWebhook *ewebhook.EndpointWebhook
	// Decoder decodes objects
	Decoder *admission.Decoder
}

// Handle handles admission requests.
func (h *MutatingPodHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// If the endpoint webhook is nil, set it to a new endpoint webhook.
	if h.epWebhook == nil {
		h.epWebhook = &ewebhook.EndpointWebhook{}
	}

	// Decode the pod object.
	obj := &corev1.Pod{}
	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	klog.Infof("receive mutating %s pod (%s/%s)", req.Operation, obj.Namespace, obj.Name)

	// If the namespace is empty, set it to the request namespace.
	if obj.Namespace == "" {
		obj.Namespace = req.Namespace
	}
	// If the annotations are empty, set it to an empty map.
	if obj.Annotations == nil {
		obj.Annotations = map[string]string{}
	}
	// If the labels are empty, set it to an empty map.
	if obj.Labels == nil {
		obj.Labels = map[string]string{}
	}

	pod := obj.DeepCopy()
	switch req.Operation {
	case admissionv1.Create:
		// adds the ENI resource to the pod, and the pod will be scheduled to the node that has the ENI resource.
		if addENIResource2Pod(ctx, pod) {
			break
		}
		addIPResource2Pod(ctx, pod)

		h.epWebhook.AddNodeAffinity(pod)
	}

	// If the original pod is the same as the mutated pod, return allowed.
	if reflect.DeepEqual(obj, pod) {
		return admission.Allowed("")
	}
	klog.Infof("mutating %s pod (%s/%s) with psts success", req.Operation, obj.Namespace, obj.Name)
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

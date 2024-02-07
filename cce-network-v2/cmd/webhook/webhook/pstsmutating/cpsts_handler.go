package pstsmutating

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	ccev2alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2alpha1"
)

type mutatingCPSTSHandler struct {
	*MutatingPSTSHandler
}

// Handle handles admission requests.
func (h *mutatingCPSTSHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Decode the pod object.
	obj := &ccev2alpha1.ClusterPodSubnetTopologySpread{}
	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	klog.Infof("receive mutating %s cpsts (%s)", req.Operation, obj.Name)

	// If the annotations are empty, set it to an empty map.
	if obj.Annotations == nil {
		obj.Annotations = map[string]string{}
	}
	// If the labels are empty, set it to an empty map.
	if obj.Labels == nil {
		obj.Labels = map[string]string{}
	}

	cpsts := obj.DeepCopy()

	cpsts.Spec.Name = obj.Name
	h.defaultPstsSpec(&cpsts.Spec.PodSubnetTopologySpreadSpec)

	allErrs := h.validatePstsSpec(&cpsts.Spec.PodSubnetTopologySpreadSpec, field.NewPath("spec"))
	err = allErrs.ToAggregate()
	if err != nil {
		klog.Errorf("handler %s ClusterPodSubnetTopologySpread(%s) error: %v", req.Operation, obj.Name, err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// If the original psts is the same as the mutated psts, return allowed.
	if reflect.DeepEqual(obj, cpsts) {
		return admission.Allowed("")
	}
	klog.Infof("mutating %s cpsts(%s) success", req.Operation, obj.Name)
	marshalled, err := json.Marshal(cpsts)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}

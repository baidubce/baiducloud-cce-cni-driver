package pstsmutating

import (
	"context"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unversionedvalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	k8sutilnet "k8s.io/utils/net"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	operatorWatchers "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

var (
	// HandlerMap contains admission webhook handlers
	HandlerMap = map[string]admission.Handler{
		"mutating-psts": &MutatingPSTSHandler{},
	}

	log = logging.NewSubysLogger("pstsmutating-webhook")
)

type MutatingPSTSHandler struct {
	// Decoder decodes objects
	Decoder *admission.Decoder
}

// Handle handles admission requests.
func (h *MutatingPSTSHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	// Decode the pod object.
	obj := &ccev2.PodSubnetTopologySpread{}
	err := h.Decoder.Decode(req, obj)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}
	klog.Infof("receive mutating %s psts (%s/%s)", req.Operation, obj.Namespace, obj.Name)

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

	psts := obj.DeepCopy()
	h.defaultPstsSpec(&psts.Spec)
	allErrs := h.validatePstsSpec(&psts.Spec, field.NewPath("spec"))
	err = allErrs.ToAggregate()
	if err != nil {
		klog.Errorf("handler %s PodSubnetTopologySpread(%s/%s) error: %v", req.Operation, obj.Namespace, obj.Name, err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	// If the original psts is the same as the mutated psts, return allowed.
	if reflect.DeepEqual(obj, psts) {
		return admission.Allowed("")
	}
	klog.Infof("mutating %s psts(%s/%s) success", req.Operation, obj.Namespace, obj.Name)
	marshalled, err := json.Marshal(psts)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.AdmissionRequest.Object.Raw, marshalled)
}

func (h *MutatingPSTSHandler) defaultPstsSpec(spec *ccev2.PodSubnetTopologySpreadSpec) {
	if spec.Strategy == nil || spec.Strategy.Type == "" {
		spec.Strategy = &ccev2.IPAllocationStrategy{
			Type:                 ccev2.IPAllocTypeElastic,
			EnableReuseIPAddress: false,
			ReleaseStrategy:      ccev2.ReleaseStrategyTTL,
		}
	}
}

func (h *MutatingPSTSHandler) validatePstsSpec(spec *ccev2.PodSubnetTopologySpreadSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec.Priority < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("priority"), spec.Priority, "priority must be non-negative"))
	}
	if spec.MaxSkew < 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("maxSkew"), spec.MaxSkew, "maxSkew must be non-negative"))
	}

	if spec.Selector != nil {
		allErrs = append(allErrs, unversionedvalidation.ValidateLabelSelector(spec.Selector, unversionedvalidation.LabelSelectorValidationOptions{}, fldPath.Child("selector"))...)
		if len(spec.Selector.MatchLabels)+len(spec.Selector.MatchExpressions) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, "empty selector is invalid for psts"))
		}
		_, err := metav1.LabelSelectorAsSelector(spec.Selector)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, ""))
		}
	}

	// validate every subnet
	if len(spec.Subnets) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("subnets"), spec.Subnets, "length of subnets must greater than 0"))
	}

	if spec.Strategy != nil {
		if spec.Strategy.Type != ccev2.IPAllocTypeElastic && spec.Strategy.Type != ccev2.IPAllocTypeFixed {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("strategy.type"), spec.Strategy.Type, "strategy type must be elastic or fixed"))
		}
		if spec.Strategy.Type == ccev2.IPAllocTypeFixed {
			if spec.Strategy.ReleaseStrategy != ccev2.ReleaseStrategyNever {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("strategy.releaseStrategy"), spec.Strategy.ReleaseStrategy, "releaseStrategy must be Never when strategy type is Fixed"))
			}
			if !spec.Strategy.EnableReuseIPAddress {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("strategy.enableReuseIPAddress"), spec.Strategy.EnableReuseIPAddress, "enableReuseIPAddress must be true when strategy type is Fixed"))
			}
		} else {
			if spec.Strategy.EnableReuseIPAddress {
				if spec.Strategy.TTL == nil {
					allErrs = append(allErrs, field.Invalid(fldPath.Child("strategy.ttl"), spec.Strategy.TTL, "ttl must be set when enableReuseIPAddress is true"))
				} else if spec.Strategy.TTL.Seconds() <= 0 {
					allErrs = append(allErrs, field.Invalid(fldPath.Child("strategy.ttl"), spec.Strategy.TTL, "ttl must be positive when enableReuseIPAddress is true"))
				}
			}
		}
	}

	if len(allErrs) > 0 {
		return allErrs
	}
	return h.validateSubnet(spec.Strategy, spec.Subnets, fldPath.Child("subnets"))
}

// Verify the availability of the subnet
// For the subnet used by fixed IP, CCE requires that the subnet
// must be marked as a exclusive subnet. If the declared subnet
// does not exist, a private subnet will be automatically created.
// If the corresponding subnet already exists, but it is not a
// exclusive subnet, an error will be triggered
func (h *MutatingPSTSHandler) validateSubnet(
	strategy *ccev2.IPAllocationStrategy,
	sas map[string]ccev2.CustomAllocationList,
	fldPath *field.Path) field.ErrorList {

	var (
		allErrs = field.ErrorList{}
	)
	for sbnID, sbnSpec := range sas {
		sbn, err := operatorWatchers.CCESubnetClient.EnsureSubnet("", sbnID, strategy.EnableReuseIPAddress)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child(sbnID), sbnID, err.Error()))
			continue
		}
		if strategy.EnableReuseIPAddress {
			allErrs = h.validateReuseSubnet(sbn, sbnSpec, fldPath.Child(sbnID), allErrs)
		} else {
			if len(sbnSpec) != 0 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child(sbnID), sbnID, "subnet allocation is not empty"))
				continue
			}
		}

		if len(allErrs) > 0 {
			return allErrs
		}
	}
	return allErrs
}

func (h *MutatingPSTSHandler) validateReuseSubnet(
	sbn *ccev1.Subnet,
	customAllocation ccev2.CustomAllocationList,
	fldPath *field.Path,
	allErrs field.ErrorList,
) field.ErrorList {
	// mark subnet as exclusive
	if !sbn.Spec.Exclusive {
		return append(allErrs, field.Invalid(fldPath, sbn, "subnet is not exclusive"))
	}

	var (
		cidrV4, cidrV6 *net.IPNet
		err            error

		// indexRanges is used to record the range of the ip index
		// this is used to check whether the custom allocation ip is out of range
		indexRanges []*big.Int
	)
	_, cidrV4, err = net.ParseCIDR(sbn.Spec.CIDR)
	if err != nil {
		return append(allErrs, field.Invalid(fldPath, sbn, "cidr of subnet is invalide"))
	}
	_, cidrV6, _ = net.ParseCIDR(sbn.Spec.IPv6CIDR)

	for _, custom := range customAllocation {
		cidrNet := cidrV4
		if custom.Family == ccev2.IPv6Family {
			cidrNet = cidrV6
			if cidrNet == nil {
				return append(allErrs, field.Invalid(fldPath, sbn, "subnet does not support ipv6"))
			}
		}
		for _, r := range custom.Range {
			if r.Start == "" || r.End == "" {
				return append(allErrs, field.Invalid(fldPath, sbn, "ip range is empty"))
			}

			startIP := net.ParseIP(r.Start)
			endIP := net.ParseIP(r.End)
			if !cidrNet.Contains(startIP) || !cidrNet.Contains(endIP) {
				return append(allErrs, field.Invalid(fldPath, sbn, "ip range is not in subnet"))
			}
			first := k8sutilnet.BigForIP(startIP)
			last := k8sutilnet.BigForIP(endIP)
			if first == nil || last == nil || first.Cmp(last) > 1 {
				return append(allErrs, field.Invalid(fldPath, sbn, "ip range is invalid"))
			}
			indexRanges = append(indexRanges, first, last)
		}
	}

	if !overlappingCheck(indexRanges) {
		return append(allErrs, field.Invalid(fldPath, sbn, "ip range is overlapping"))
	}

	return allErrs
}

// Overlapping detection range
// Compare each group of ranges. The start and end of other groups cannot overlap
// return true if each goup does not overlap
func overlappingCheck(indexRanges []*big.Int) bool {
	for i := 0; i+1 < len(indexRanges); i = i + 2 {
		for j := i + 2; j+1 < len(indexRanges); j = j + 2 {
			a := indexRanges[i].Cmp(indexRanges[j])
			b := indexRanges[i+1].Cmp(indexRanges[j])
			c := indexRanges[i].Cmp(indexRanges[j+1])
			d := indexRanges[i+1].Cmp(indexRanges[j+1])
			if a != b || b != c || c != d {
				return false
			}
		}
	}
	return true
}

var _ admission.DecoderInjector = &MutatingPSTSHandler{}

// InjectDecoder injects the decoder into the PodCreateHandler
func (h *MutatingPSTSHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}

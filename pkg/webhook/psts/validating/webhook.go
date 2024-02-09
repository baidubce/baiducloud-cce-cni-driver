package validating

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unversionedvalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog"
	apivalidation "k8s.io/kubernetes/pkg/apis/core/validation"
	k8sutilnet "k8s.io/utils/net"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	networkv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/controller/subnet"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/clientset/versioned"
	crdinformers "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions"
	networkinformer "github.com/baidubce/baiducloud-cce-cni-driver/pkg/generated/informers/externalversions/networking/v1alpha1"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/iprange"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/cidr"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/webhook/injector"
)

var (
	// HandlerMap contains admission webhook handlers
	HandlerMap = map[string]admission.Handler{
		"validating-pod-subnet-topology-spread": &ValidatingPSTSHandler{},
	}

	pstsKind = metav1.GroupVersionKind{
		Group:   networkv1alpha1.SchemeGroupVersion.Group,
		Version: networkv1alpha1.SchemeGroupVersion.Version,
		Kind:    "PodSubnetTopologySpread",
	}
	psttKind = metav1.GroupVersionKind{
		Group:   networkv1alpha1.SchemeGroupVersion.Group,
		Version: networkv1alpha1.SchemeGroupVersion.Version,
		Kind:    "PodSubnetTopologySpreadTable",
	}
)

type ValidatingPSTSHandler struct {
	crdClient      versioned.Interface
	subnetInformer networkinformer.SubnetInformer
	bceClient      cloud.Interface
	// Decoder decodes objects
	Decoder *admission.Decoder
}

// Handle handles admission requests.
func (h *ValidatingPSTSHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	var (
		pstsList  []networkv1alpha1.PodSubnetTopologySpreadSpec
		name      string
		namespace string
	)

	// case1: table
	if reflect.DeepEqual(req.Kind, psttKind) {
		table := &networkv1alpha1.PodSubnetTopologySpreadTable{}

		err := h.Decoder.Decode(req, table)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		namespace = table.Namespace
		name = table.Name
		klog.V(5).Infof("receive %s PodSubnetTopologySpreadTable (%s/%s)", req.Operation, table.Namespace, table.Name)

		for _, v := range table.Spec {
			if v.Name == "" {
				return admission.Errored(http.StatusBadRequest, fmt.Errorf("table item name is empty"))
			}
		}
		if len(table.Spec) > 0 {
			pstsList = table.Spec
		} else {
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("table have none psts spec"))
		}

	} else {
		// case2: psts
		obj := &networkv1alpha1.PodSubnetTopologySpread{}

		err := h.Decoder.Decode(req, obj)
		if err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
		namespace = obj.Namespace
		name = obj.Name
		klog.V(5).Infof("receive %s PodSubnetTopologySpread (%s/%s)", req.Operation, obj.Namespace, obj.Name)
		pstsList = append(pstsList, obj.Spec)
	}

	for i := 0; i < len(pstsList); i++ {
		spec := &pstsList[i]
		pstsName := name
		if spec.Name != "" {
			pstsName = spec.Name
		}
		allErrs := h.validatePstsSpec(spec, field.NewPath("spec"))
		err := allErrs.ToAggregate()
		if err != nil {
			klog.Errorf("handler %s PodSubnetTopologySpread(%s/%s) error: %v", req.Operation, namespace, pstsName, err)
			return admission.Errored(http.StatusInternalServerError, err)
		}
	}

	return admission.Allowed("")
}

func (h *ValidatingPSTSHandler) validatePstsSpec(spec *networkv1alpha1.PodSubnetTopologySpreadSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(spec.Priority), fldPath.Child("priority"))...)
	allErrs = append(allErrs, apivalidation.ValidateNonnegativeField(int64(spec.MaxSkew), fldPath.Child("maxSkew"))...)

	if spec.Selector != nil {
		allErrs = append(allErrs, unversionedvalidation.ValidateLabelSelector(spec.Selector, fldPath.Child("selector"))...)
		if len(spec.Selector.MatchLabels)+len(spec.Selector.MatchExpressions) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("selector"), spec.Selector, "empty selector is invalid for cloneset"))
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
func (h *ValidatingPSTSHandler) validateSubnet(
	strategy *networkv1alpha1.IPAllocationStrategy,
	sas map[string]networkv1alpha1.SubnetAllocation,
	fldPath *field.Path) field.ErrorList {

	var (
		allErrs  = field.ErrorList{}
		lastType networkv1alpha1.IPAllocType
	)
	for sbnID, sbnSpec := range sas {
		newStrategy := sbnSpec.IPAllocationStrategy
		newType := sbnSpec.Type
		if strategy != nil {
			newType = strategy.Type
			newStrategy = *strategy
		}

		var sbn *networkv1alpha1.Subnet

		switch newType {
		case networkv1alpha1.IPAllocTypeFixed:
			sbn, allErrs = h.getORCreateSubnet(sbnID, true, fldPath.Child(sbnID), allErrs)
			if len(allErrs) > 0 {
				return allErrs
			}
			allErrs = h.validateExclusiveSubnet(sbn, &sbnSpec, fldPath.Child(sbnID), allErrs)
		case networkv1alpha1.IPAllocTypeManual:
			// manaual ip validating
			sbn, allErrs = h.getORCreateSubnet(sbnID, true, fldPath.Child(sbnID), allErrs)
			if len(allErrs) > 0 {
				return allErrs
			}
			if newStrategy.ReleaseStrategy != networkv1alpha1.ReleaseStrategyTTL && newStrategy.ReleaseStrategy != "" {
				allErrs = append(allErrs,
					field.Invalid(
						fldPath.Child(fmt.Sprintf("%s.%s", sbn.Name, "releaseStrategy")),
						sbnSpec, "releaseStrategy is not TTL on Elastic mode"),
				)
			}
			allErrs = h.validateExclusiveSubnet(sbn, &sbnSpec, fldPath.Child(sbnID), allErrs)
		case networkv1alpha1.IPAllocTypeCustom:
			sbn, allErrs = h.getORCreateSubnet(sbnID, true, fldPath.Child(sbnID), allErrs)
			if len(allErrs) > 0 {
				return allErrs
			}
			allErrs = h.validateCustomMode(sbn, &sbnSpec, fldPath.Child(sbnID), allErrs)
		case "":
			sbnSpec.Type = networkv1alpha1.IPAllocTypeElastic
			fallthrough
		case networkv1alpha1.IPAllocTypeElastic:
			sbn, allErrs = h.getORCreateSubnet(sbnID, false, fldPath.Child(sbnID), allErrs)
			if len(allErrs) > 0 {
				return allErrs
			}
			allErrs = h.validateElasticMode(sbnSpec, allErrs, fldPath, sbnID, sbn)
		default:
			return append(allErrs, field.Invalid(fldPath.Child(fmt.Sprintf("%s.%s", sbnID, "type")), sbnSpec, fmt.Sprintf("unknown subnet type %s, the value must be Fixed, Manual or Elastic", sbnSpec.Type)))
		}

		// mutipule type cannot be mixed
		if lastType == "" {
			lastType = sbnSpec.Type
		}
		if lastType != sbnSpec.Type {
			return append(allErrs, field.Invalid(fldPath.Child(fmt.Sprintf("%s.%s", sbnID, "type")), sbnSpec, fmt.Sprintf("%s type and %s type cannot be mixed", lastType, sbnSpec.Type)))
		}

		if len(allErrs) > 0 {
			return allErrs
		}
	}
	return allErrs
}

func (h *ValidatingPSTSHandler) validateElasticMode(sbnSpec networkv1alpha1.SubnetAllocation, allErrs field.ErrorList, fldPath *field.Path, sbnID string, sbn *networkv1alpha1.Subnet) field.ErrorList {
	if sbnSpec.Type == networkv1alpha1.IPAllocTypeElastic || sbnSpec.Type == "" {
		if len(sbnSpec.IPv4) != 0 || len(sbnSpec.IPv6) != 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child(fmt.Sprintf("%s.%s", sbnID, "ipv4")), sbnSpec, "can not use fixed ip on Elastic mode"))
		}
		if sbnSpec.ReleaseStrategy != networkv1alpha1.ReleaseStrategyTTL && sbnSpec.ReleaseStrategy != "" {
			allErrs = append(allErrs, field.Invalid(fldPath.Child(fmt.Sprintf("%s.%s", sbnID, "releaseStrategy")), sbnSpec, "releaseStrategy is not TTL on Elastic mode"))
		}
		if sbn.Spec.Exclusive {
			allErrs = append(allErrs, field.Invalid(fldPath.Child(sbnID), sbn, "can not use exclusive subnet on Elastic mode"))
		}
	}
	return allErrs
}

// For the subnet used by fixed IP, CCE requires that the subnet
// must be marked as a exclusive subnet. If the declared subnet
// does not exist, a private subnet will be automatically created.
// If the corresponding subnet already exists, but it is not a
// exclusive subnet, an error will be triggered
func (h *ValidatingPSTSHandler) getORCreateSubnet(
	sbnID string,
	exclusive bool,
	fldPath *field.Path,
	allErrs field.ErrorList,
) (*networkv1alpha1.Subnet,
	field.ErrorList) {
	sbn, err := h.subnetInformer.Lister().Subnets(corev1.NamespaceDefault).Get(sbnID)
	if err != nil {
		if errors.IsNotFound(err) {
			// 1. create a new subnet cr
			err = subnet.CreateSubnetCR(context.Background(), h.bceClient, h.crdClient, sbnID)
			if err != nil {
				return nil, append(allErrs, field.Invalid(fldPath, sbnID, fmt.Sprintf("create subnet %s error: %v", sbnID, err)))
			}
			if exclusive {
				// 2. mark subnet as exclusive
				sbn, err = subnet.MarkExclusiveSubnet(context.Background(), h.crdClient, sbnID, true)
				if err != nil {
					return nil, append(allErrs, field.Invalid(fldPath, sbn, fmt.Sprintf("mark subnet as exclude error: %v", err)))
				}
			}
			if sbn == nil {
				sbn, err = subnet.GetSubnet(context.Background(), h.crdClient, sbnID)
				if err != nil {
					return nil, append(allErrs, field.Invalid(fldPath, sbn, fmt.Sprintf("get subnet error: %v", err)))
				}
			}
		} else {
			return nil, append(allErrs, field.Invalid(fldPath.Child(sbnID), sbnID, fmt.Sprintf("get subnet %s error: %v", sbnID, err)))
		}
	}

	// subnet is not enable
	if !sbn.Status.Enable {
		allErrs = append(allErrs, field.Invalid(fldPath, sbn, fmt.Sprintf("subnet %s is not enable: %s", sbnID, sbn.Status.Reason)))
	}
	return sbn, allErrs
}

func (h *ValidatingPSTSHandler) validateExclusiveSubnet(sbn *networkv1alpha1.Subnet, sbnSpec *networkv1alpha1.SubnetAllocation, fldPath *field.Path, allErrs field.ErrorList) field.ErrorList {
	// mark subnet as exclusive
	if !sbn.Spec.Exclusive {
		allErrs = append(allErrs, field.Invalid(fldPath, sbn, "subnet is not exclusive"))
	}

	if len(sbnSpec.IPv4) == 0 && len(sbnSpec.IPv6) == 0 &&
		len(sbnSpec.IPv4Range) == 0 && len(sbnSpec.IPv6Range) == 0 {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("ipv4/ipv6"), sbnSpec, "no assignable IPS"))
	}

	// validate ipv4 in CIDR
	_, ipNet, err := net.ParseCIDR(sbn.Spec.CIDR)
	if err != nil {
		return append(allErrs, field.Invalid(fldPath, sbnSpec, fmt.Sprintf("parse subnet CIDR %s error %v", sbn.Spec.CIDR, err)))
	}
	for _, ipstr := range sbnSpec.IPv4 {
		if !ipNet.Contains(net.ParseIP(ipstr)) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("ipv4"), sbnSpec, fmt.Sprintf("ip %s not in subnet CIDR %s", ipstr, sbn.Spec.CIDR)))
		}
	}

	cidrs, err := k8sutilnet.ParseCIDRs(sbnSpec.IPv4Range)
	if err != nil {
		return append(allErrs, field.Invalid(fldPath.Child("ipv4Range"), sbnSpec, fmt.Sprintf("parse ipv4Range error: %v", err)))
	}
	for _, custerCIDR := range cidrs {
		ipList := cidr.ListIPsFromCIDR(custerCIDR)
		for _, ip := range ipList {
			if !ipNet.Contains(ip) {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("ipv4Range"), sbnSpec, fmt.Sprintf("ip %s not in subnet CIDR %s", custerCIDR.String(), sbn.Spec.CIDR)))
				break
			}
		}
	}
	return allErrs
}

func (h *ValidatingPSTSHandler) validateCustomMode(
	sbn *networkv1alpha1.Subnet,
	sbnSpec *networkv1alpha1.SubnetAllocation,
	fldPath *field.Path,
	allErrs field.ErrorList,
) field.ErrorList {
	// mark subnet as exclusive
	if !sbn.Spec.Exclusive {
		return append(allErrs, field.Invalid(fldPath, sbn, "subnet is not exclusive"))
	}

	_, cidr, err := net.ParseCIDR(sbn.Spec.CIDR)
	if err != nil {
		return append(allErrs, field.Invalid(fldPath, sbn, "cidr of subnet is invalide"))
	}
	_, err = iprange.ListAllCustomIPRangeIndexs(cidr, sbnSpec.Custom)
	if err != nil {
		return append(allErrs, field.Invalid(fldPath, sbnSpec.Custom, err.Error()))
	}

	return allErrs
}

var _ admission.DecoderInjector = &ValidatingPSTSHandler{}

// InjectDecoder injects the decoder into the PodCreateHandler
func (h *ValidatingPSTSHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}

var _ injector.CRDClient = &ValidatingPSTSHandler{}

func (h *ValidatingPSTSHandler) InjectCRDClient(crdClient versioned.Interface) error {
	h.crdClient = crdClient
	return nil
}

var _ injector.CloudClientInject = &ValidatingPSTSHandler{}

func (h *ValidatingPSTSHandler) InjectCloudClient(bceClient cloud.Interface) error {
	h.bceClient = bceClient
	return nil
}

var _ injector.NetworkInformerInject = &ValidatingPSTSHandler{}

func (h *ValidatingPSTSHandler) InjectNetworkInformer(networkInformer crdinformers.SharedInformerFactory) error {
	h.subnetInformer = networkInformer.Cce().V1alpha1().Subnets()
	return nil
}

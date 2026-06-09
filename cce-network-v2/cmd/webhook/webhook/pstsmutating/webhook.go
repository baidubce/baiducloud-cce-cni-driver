package pstsmutating

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unversionedvalidation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	k8sutilnet "k8s.io/utils/net"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	"github.com/baidubce/bce-sdk-go/services/vpc"

	operatorWatchers "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/watchers"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud/ccegateway"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/defaults"
	iputil "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/ip"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccev1lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
)

// SubnetClient defines the interface for subnet operations
type SubnetClient interface {
	Lister() ccev1lister.SubnetLister
}

type clusterConfig struct {
	VPCID           string
	Region          string
	ClusterID       string
	AccessKey       string
	SecretKey       string
	BaseHost        string
	ForceViaGateway bool
	EnableIPv6      bool
}

var (
	// HandlerMap contains admission webhook handlers
	HandlerMap = map[string]admission.Handler{
		"mutating-psts": &MutatingPSTSHandler{},
		"mutating-cluster-psts": &mutatingCPSTSHandler{
			MutatingPSTSHandler: &MutatingPSTSHandler{},
		},
	}

	getClusterConfig = defaultGetClusterConfig
	buildCloudClient = defaultBuildCloudClient
	describeSubnet   = defaultDescribeSubnet
	getK8sClient     = k8s.Client
	poll             = wait.Poll
	newCloudClient   = cloud.New
	newFlowControl   = cloud.NewFlowControlClient
	readConfigFile   = getClusterConfigFromFile
	readConfigMap    = getClusterConfigFromConfigMap

	// vpcID is the cluster VPC ID, populated once during Init.
	vpcID string
	// vpcClient is the shared VPC client created during Init and reused by webhook handlers.
	vpcClient cloud.Interface
	// clusterIPv6Enabled indicates whether cluster runs in dual-stack (IPv4+IPv6) mode.
	clusterIPv6Enabled bool
)

const defaultClusterConfigFile = "/etc/cce/network-v2-config.yaml"

// Init preloads cluster config and cloud client at startup.
func Init() error {
	cfg, err := getClusterConfig()
	if err != nil {
		return err
	}
	if cfg.VPCID == "" {
		return fmt.Errorf("cluster vpc id is empty")
	}
	client, err := buildCloudClient(cfg)
	if err != nil {
		return err
	}
	vpcID = cfg.VPCID
	vpcClient = client
	clusterIPv6Enabled = cfg.EnableIPv6
	return nil
}

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

	// For UPDATE operations, validate that deleted subnets/ranges/CIDRs have no IPs in use
	if req.Operation == "UPDATE" && len(req.OldObject.Raw) > 0 {
		oldObj := &ccev2.PodSubnetTopologySpread{}
		if err := json.Unmarshal(req.OldObject.Raw, oldObj); err != nil {
			klog.Errorf("failed to unmarshal old PSTS object %s/%s: %v", obj.Namespace, obj.Name, err)
			return admission.Errored(http.StatusBadRequest, fmt.Errorf("failed to unmarshal old object: %v", err))
		}
		if validationErr := h.validatePSTSUpdate(oldObj, obj); validationErr != nil {
			klog.Errorf("PSTS update validation failed for %s/%s: %v", obj.Namespace, obj.Name, validationErr)
			return admission.Denied(validationErr.Error())
		}
	}

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

	psts.Spec.Name = obj.Name
	h.defaultPstsSpec(&psts.Spec)

	allErrs := h.validatePstsSpec(&psts.Spec, field.NewPath("spec"))
	err = allErrs.ToAggregate()
	if err != nil {
		klog.Errorf("handler %s PodSubnetTopologySpread(%s/%s) error: %v", req.Operation, obj.Namespace, obj.Name, err)
		return admission.Errored(http.StatusBadRequest, err)
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

// validatePSTSUpdate validates PSTS update operations to prevent deletion of subnets/ranges/CIDRs with IPs in use
func (h *MutatingPSTSHandler) validatePSTSUpdate(oldObj, newObj *ccev2.PodSubnetTopologySpread) error {
	// Running PSTS cannot switch between Elastic and Fixed mode.
	oldType := effectiveStrategyType(oldObj.Spec.Strategy)
	newType := effectiveStrategyType(newObj.Spec.Strategy)
	if isElasticFixedSwitch(oldType, newType) && isPSTSRunning(oldObj) {
		return fmt.Errorf("cannot change strategy.type from %q to %q while PSTS is running; ensure no matched pods and no allocated IPs remain before switching", oldType, newType)
	}
	skipRangeCIDRInUseCheck := newType == ccev2.IPAllocTypeElastic

	// Calculate which subnets/ranges/CIDRs are being removed
	removedSubnets, removedRanges, removedCIDRs := calculatePSTSRemovals(oldObj, newObj)

	if len(removedSubnets) == 0 && len(removedRanges) == 0 && len(removedCIDRs) == 0 {
		return nil // No deletions, allow
	}

	// Check Status.IPAllocations for IPs in use
	conflicts := make(map[string][]string) // category -> list of "IP (pod)"

	// Check removed subnets
	for _, subnetID := range removedSubnets {
		if status, ok := oldObj.Status.AvailableSubnets[subnetID]; ok {
			for ip, podName := range status.IPAllocations {
				conflicts["subnet:"+subnetID] = append(conflicts["subnet:"+subnetID],
					fmt.Sprintf("%s (pod: %s)", ip, podName))
			}
		}
		if status, ok := oldObj.Status.UnavailableSubnets[subnetID]; ok {
			for ip, podName := range status.IPAllocations {
				conflicts["subnet:"+subnetID] = append(conflicts["subnet:"+subnetID],
					fmt.Sprintf("%s (pod: %s)", ip, podName))
			}
		}
	}

	if !skipRangeCIDRInUseCheck {
		// Check removed ranges
		for subnetID, ranges := range removedRanges {
			var allIPs map[string]string
			if status, ok := oldObj.Status.AvailableSubnets[subnetID]; ok {
				allIPs = status.IPAllocations
			} else if status, ok := oldObj.Status.UnavailableSubnets[subnetID]; ok {
				allIPs = status.IPAllocations
			}

			if allIPs != nil {
				for _, r := range ranges {
					startIP := net.ParseIP(r.Start)
					endIP := net.ParseIP(r.End)
					rangeKey := fmt.Sprintf("subnet %s range %s-%s", subnetID, r.Start, r.End)
					for ip, podName := range allIPs {
						parsedIP := net.ParseIP(ip)
						if ipInRange(parsedIP, startIP, endIP) {
							conflicts[rangeKey] = append(conflicts[rangeKey],
								fmt.Sprintf("%s (pod: %s)", ip, podName))
						}
					}
				}
			}
		}

		// Check removed CIDRs
		for subnetID, cidrs := range removedCIDRs {
			var allIPs map[string]string
			if status, ok := oldObj.Status.AvailableSubnets[subnetID]; ok {
				allIPs = status.IPAllocations
			} else if status, ok := oldObj.Status.UnavailableSubnets[subnetID]; ok {
				allIPs = status.IPAllocations
			}

			if allIPs != nil {
				for _, cidr := range cidrs {
					_, ipNet, err := net.ParseCIDR(cidr)
					if err != nil {
						continue
					}
					cidrKey := fmt.Sprintf("subnet %s CIDR %s", subnetID, cidr)
					for ip, podName := range allIPs {
						if ipNet.Contains(net.ParseIP(ip)) {
							conflicts[cidrKey] = append(conflicts[cidrKey],
								fmt.Sprintf("%s (pod: %s)", ip, podName))
						}
					}
				}
			}
		}
	}

	if len(conflicts) > 0 {
		var errMsgs []string
		for category, ips := range conflicts {
			errMsgs = append(errMsgs, fmt.Sprintf("%s has %d IP(s) in use: [%s]",
				category, len(ips), joinFirst5(ips)))
		}
		return fmt.Errorf("cannot delete subnet/range/CIDR with IPs in use: %s", joinFirst5(errMsgs))
	}

	return nil
}

func effectiveStrategyType(strategy *ccev2.IPAllocationStrategy) ccev2.IPAllocType {
	// Keep behavior consistent with defaulting: nil/empty means Elastic.
	if strategy == nil || strategy.Type == ccev2.IPAllocTypeNil {
		return ccev2.IPAllocTypeElastic
	}
	return strategy.Type
}

func isElasticFixedSwitch(oldType, newType ccev2.IPAllocType) bool {
	return (oldType == ccev2.IPAllocTypeElastic && newType == ccev2.IPAllocTypeFixed) ||
		(oldType == ccev2.IPAllocTypeFixed && newType == ccev2.IPAllocTypeElastic)
}

func isPSTSRunning(psts *ccev2.PodSubnetTopologySpread) bool {
	if psts == nil {
		return false
	}
	if psts.Status.PodMatchedCount > 0 {
		return true
	}
	for _, subnet := range psts.Status.AvailableSubnets {
		if len(subnet.IPAllocations) > 0 {
			return true
		}
	}
	for _, subnet := range psts.Status.UnavailableSubnets {
		if len(subnet.IPAllocations) > 0 {
			return true
		}
	}
	return false
}

// joinFirst5 joins the first 5 elements with ", " and adds "..." if there are more
func joinFirst5(items []string) string {
	if len(items) <= 5 {
		return fmt.Sprintf("%v", items)
	}
	return fmt.Sprintf("%v... (%d more)", items[:5], len(items)-5)
}

// RemovedRange represents a removed IP range with original start-end format
type RemovedRange struct {
	Start string
	End   string
}

// calculatePSTSRemovals calculates which subnets, ranges, and CIDRs are being removed
// Returns: (removedSubnets, removedRangesPerSubnet, removedCIDRsPerSubnet)
// removedRanges keeps original start-end format for user-friendly error messages
func calculatePSTSRemovals(oldObj, newObj *ccev2.PodSubnetTopologySpread) (
	[]string, map[string][]RemovedRange, map[string][]string) {

	removedSubnets := []string{}
	removedRanges := make(map[string][]RemovedRange) // subnetID -> removed ranges (original format)
	removedCIDRs := make(map[string][]string)        // subnetID -> removed CIDRs

	// Check for removed subnets
	for oldSubnetID := range oldObj.Spec.Subnets {
		if _, exists := newObj.Spec.Subnets[oldSubnetID]; !exists {
			removedSubnets = append(removedSubnets, oldSubnetID)
		}
	}

	// Check for removed ranges/CIDRs within existing subnets
	for subnetID, oldAllocs := range oldObj.Spec.Subnets {
		newAllocs, exists := newObj.Spec.Subnets[subnetID]
		if !exists {
			continue // Already counted as removed subnet
		}

		// Build set of new CIDRs for comparison
		newCIDRSet := make(map[string]bool)
		for _, alloc := range newAllocs {
			for _, cidr := range alloc.CIDRs {
				newCIDRSet[cidr] = true
			}
		}

		// Find removed ranges
		for _, alloc := range oldAllocs {
			for _, r := range alloc.Range {
				if !rangeCoveredByNewRanges(r, newAllocs) {
					removedRanges[subnetID] = append(removedRanges[subnetID], RemovedRange{
						Start: r.Start,
						End:   r.End,
					})
				}
			}
		}

		// Find removed CIDRs
		for _, alloc := range oldAllocs {
			for _, cidr := range alloc.CIDRs {
				if !newCIDRSet[cidr] {
					removedCIDRs[subnetID] = append(removedCIDRs[subnetID], cidr)
				}
			}
		}
	}

	return removedSubnets, removedRanges, removedCIDRs
}

// rangeCoveredByNewRanges returns true if any new range fully covers the old range.
func rangeCoveredByNewRanges(oldRange ccev2.CustomIPRange, newAllocs ccev2.CustomAllocationList) bool {
	oldStart := net.ParseIP(oldRange.Start)
	oldEnd := net.ParseIP(oldRange.End)
	if oldStart == nil || oldEnd == nil {
		return false
	}
	oldStartBI := k8sutilnet.BigForIP(oldStart)
	oldEndBI := k8sutilnet.BigForIP(oldEnd)
	if oldStartBI == nil || oldEndBI == nil {
		return false
	}

	for _, alloc := range newAllocs {
		for _, nr := range alloc.Range {
			newStart := net.ParseIP(nr.Start)
			newEnd := net.ParseIP(nr.End)
			if newStart == nil || newEnd == nil {
				continue
			}
			newStartBI := k8sutilnet.BigForIP(newStart)
			newEndBI := k8sutilnet.BigForIP(newEnd)
			if newStartBI == nil || newEndBI == nil {
				continue
			}
			if newStartBI.Cmp(oldStartBI) <= 0 && newEndBI.Cmp(oldEndBI) >= 0 {
				return true
			}
		}
	}
	return false
}

// ipInRange checks if an IP is within the range [start, end] (inclusive)
func ipInRange(ip, start, end net.IP) bool {
	if ip == nil || start == nil || end == nil {
		return false
	}
	// Normalize to same length
	ip = ip.To16()
	start = start.To16()
	end = end.To16()
	return bytes.Compare(ip, start) >= 0 && bytes.Compare(ip, end) <= 0
}

func (h *MutatingPSTSHandler) defaultPstsSpec(spec *ccev2.PodSubnetTopologySpreadSpec) {
	if spec.Strategy == nil || spec.Strategy.Type == "" {
		spec.Strategy = &ccev2.IPAllocationStrategy{
			Type: ccev2.IPAllocTypeElastic,
		}
	}

	// releaseStrategy / enableReuseIPAddress are deprecated since 2.13.9, strip to avoid confusion.
	if spec.Strategy != nil {
		spec.Strategy.ReleaseStrategy = ""
		spec.Strategy.EnableReuseIPAddress = false
	}

	// No default TTL for any mode:
	// - Elastic mode: TTL is ignored, always immediate release
	// - Fixed mode: TTL is optional, no TTL means permanent retention
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

	// Validate strategy
	if spec.Strategy != nil {
		allErrs = append(allErrs, h.validateStrategy(spec.Strategy, fldPath.Child("strategy"))...)
		allErrs = append(allErrs, validateFixedStrategyNoRangeSubnets(spec, fldPath)...)
	}

	if len(allErrs) > 0 {
		return allErrs
	}
	return h.validateSubnet(spec.Subnets, fldPath.Child("subnets"))
}

// validateStrategy validates the IP allocation strategy using switch case
func (h *MutatingPSTSHandler) validateStrategy(strategy *ccev2.IPAllocationStrategy, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	switch strategy.Type {
	case ccev2.IPAllocTypeElastic:
		// Elastic mode: IP is released immediately when pod is deleted
		// TTL is not allowed in Elastic mode
		if strategy.TTL != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("ttl"), strategy.TTL,
				"TTL is not allowed in Elastic mode, IP is always released immediately when pod is deleted"))
		}

	case ccev2.IPAllocTypeFixed:
		// Fixed mode: IP is retained until CEP is deleted or TTL expires
		// TTL is optional:
		// - If not set: permanent retention (CEP must be manually deleted)
		// - If set: CEP is automatically deleted after TTL expires
		if strategy.TTL != nil {
			if strategy.TTL.Seconds() < 0 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("ttl"), strategy.TTL,
					"TTL must be non-negative for Fixed mode"))
			}
			if strategy.TTL.Seconds() == 0 {
				allErrs = append(allErrs, field.Invalid(fldPath.Child("ttl"), strategy.TTL,
					"TTL cannot be zero in Fixed mode, use Elastic mode for immediate release"))
			}
		}

	default:
		allErrs = append(allErrs, field.Invalid(fldPath.Child("type"), strategy.Type,
			"strategy type must be 'Elastic' or 'Fixed'"))
	}

	return allErrs
}

// validateFixedStrategyNoRangeSubnets blocks no-range subnet config for Fixed mode.
// Product currently does not support fixed-ip allocation on subnet-level random pools.
func validateFixedStrategyNoRangeSubnets(spec *ccev2.PodSubnetTopologySpreadSpec, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if spec == nil || spec.Strategy == nil || spec.Strategy.Type != ccev2.IPAllocTypeFixed {
		return allErrs
	}

	for subnetID, allocList := range spec.Subnets {
		if allocList.HasRangeOrCIDRs() {
			continue
		}
		allErrs = append(allErrs, field.Invalid(
			fldPath.Child("subnets").Key(subnetID),
			allocList,
			"Fixed strategy does not support no-range subnet, please configure range or cidrs",
		))
	}
	return allErrs
}

// Verify the availability of the subnet
// PSTS can now use any subnet with IPReserve mechanism
// No longer requires Exclusive flag
func (h *MutatingPSTSHandler) validateSubnet(
	sas map[string]ccev2.CustomAllocationList,
	fldPath *field.Path) field.ErrorList {

	var (
		allErrs = field.ErrorList{}
	)
	if vpcClient == nil || vpcID == "" {
		return append(allErrs, field.InternalError(fldPath, fmt.Errorf("vpc client or vpc id not initialized")))
	}
	cloudClient := vpcClient
	for sbnID, sbnSpec := range sas {
		subnetInfo, err := describeSubnet(context.Background(), cloudClient, sbnID)
		if err != nil {
			allErrs = append(allErrs, field.InternalError(fldPath.Child(sbnID), fmt.Errorf("describe subnet %s failed: %w", sbnID, err)))
			continue
		}
		if subnetInfo == nil || subnetInfo.VPCId == "" {
			allErrs = append(allErrs, field.Invalid(fldPath.Child(sbnID), sbnID, "describe subnet returned empty vpc id"))
			continue
		}
		if subnetInfo.VPCId != vpcID {
			allErrs = append(allErrs, field.Invalid(
				fldPath.Child(sbnID),
				sbnID,
				fmt.Sprintf("subnet %s vpc %s not match cluster vpc %s", sbnID, subnetInfo.VPCId, vpcID),
			))
			continue
		}
		allErrs = append(allErrs, validateSubnetStackCompatibility(clusterIPv6Enabled, subnetInfo, fldPath, sbnID)...)
		if len(allErrs) > 0 {
			return allErrs
		}
		// No longer pass exclusive flag, PSTS can use any subnet
		sbn, err := operatorWatchers.CCESubnetClient.EnsureSubnet(vpcID, sbnID)
		if err != nil {
			allErrs = append(allErrs, field.Invalid(fldPath.Child(sbnID), sbnID, err.Error()))
			continue
		}
		// Validate custom allocation ranges if specified
		if len(sbnSpec) > 0 {
			allErrs = h.validateReuseSubnet(sbn, sbnSpec, fldPath.Child(sbnID), allErrs, operatorWatchers.CCESubnetClient)
		}

		if len(allErrs) > 0 {
			return allErrs
		}
	}
	return allErrs
}

func validateSubnetStackCompatibility(clusterIPv6Enabled bool, subnetInfo *vpc.Subnet, fldPath *field.Path, subnetID string) field.ErrorList {
	allErrs := field.ErrorList{}
	if !clusterIPv6Enabled || subnetInfo == nil {
		return allErrs
	}
	hasIPv4CIDR := strings.TrimSpace(subnetInfo.Cidr) != ""
	hasIPv6CIDR := strings.TrimSpace(subnetInfo.Ipv6Cidr) != ""
	if hasIPv4CIDR && hasIPv6CIDR {
		return allErrs
	}
	return append(allErrs, field.Invalid(
		fldPath.Child(subnetID),
		subnetID,
		"dual-stack cluster requires dual-stack subnet (both IPv4 CIDR and IPv6 CIDR)",
	))
}

const (
	clusterConfigMapName = "cce-network-v2-config"
	clusterConfigKey     = "cced"
)

func defaultGetClusterConfig() (clusterConfig, error) {
	cfg, err := readConfigFile(defaultClusterConfigFile)
	if err == nil {
		return cfg, nil
	}
	if !os.IsNotExist(err) {
		return clusterConfig{}, err
	}

	// Use kube-system of the target cluster (kubeconfig-apiserver) to avoid ambiguity.
	return readConfigMap(metav1.NamespaceSystem)
}

func getClusterConfigFromFile(path string) (clusterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return clusterConfig{}, err
	}
	cfg, err := parseClusterConfig(data, fmt.Sprintf("config file %s", path))
	if err != nil {
		return clusterConfig{}, err
	}
	return cfg, nil
}

func getClusterConfigFromConfigMap(namespace string) (clusterConfig, error) {
	cm, err := getK8sClient().CoreV1().ConfigMaps(namespace).Get(context.TODO(), clusterConfigMapName, metav1.GetOptions{})
	if err != nil {
		return clusterConfig{}, fmt.Errorf("failed to get configmap %s/%s: %w", namespace, clusterConfigMapName, err)
	}
	rawCfg, ok := cm.Data[clusterConfigKey]
	if !ok || rawCfg == "" {
		return clusterConfig{}, fmt.Errorf("configmap %s/%s missing key %q", namespace, clusterConfigMapName, clusterConfigKey)
	}
	return parseClusterConfig([]byte(rawCfg), fmt.Sprintf("configmap %s/%s key %q", namespace, clusterConfigMapName, clusterConfigKey))
}

func parseClusterConfig(raw []byte, source string) (clusterConfig, error) {
	cfgMap := map[string]interface{}{}
	if err := yaml.Unmarshal(raw, &cfgMap); err != nil {
		return clusterConfig{}, fmt.Errorf("failed to parse %s: %w", source, err)
	}

	cfg := clusterConfig{
		VPCID:           getConfigString(cfgMap, "bce-cloud-vpc-id"),
		Region:          getConfigString(cfgMap, "bce-cloud-region"),
		ClusterID:       getConfigString(cfgMap, "cce-cluster-id"),
		AccessKey:       getConfigString(cfgMap, "bce-cloud-access-key"),
		SecretKey:       getConfigString(cfgMap, "bce-cloud-secure-key"),
		BaseHost:        getConfigString(cfgMap, "bce-cloud-host"),
		ForceViaGateway: getConfigBool(cfgMap, "bce-cloud-force-via-cce-gateway"),
		EnableIPv6:      getConfigBool(cfgMap, "enable-ipv6"),
	}
	applyEnvOverrides(&cfg)
	return cfg, nil
}

func applyEnvOverrides(cfg *clusterConfig) {
	if val := os.Getenv(envKey("bce-cloud-vpc-id")); val != "" {
		cfg.VPCID = val
	}
	if val := os.Getenv(envKey("bce-cloud-region")); val != "" {
		cfg.Region = val
	}
	if val := os.Getenv(envKey("cce-cluster-id")); val != "" {
		cfg.ClusterID = val
	}
	if val := os.Getenv(envKey("bce-cloud-access-key")); val != "" {
		cfg.AccessKey = val
	}
	if val := os.Getenv(envKey("bce-cloud-secure-key")); val != "" {
		cfg.SecretKey = val
	}
	if val := os.Getenv(envKey("bce-cloud-host")); val != "" {
		cfg.BaseHost = val
	}
	if val := os.Getenv(envKey("bce-cloud-force-via-cce-gateway")); val != "" {
		if parsed, err := strconv.ParseBool(val); err == nil {
			cfg.ForceViaGateway = parsed
		}
	}
	if val := os.Getenv(envKey("enable-ipv6")); val != "" {
		if parsed, err := strconv.ParseBool(val); err == nil {
			cfg.EnableIPv6 = parsed
		}
	}
}

func envKey(opt string) string {
	under := strings.ReplaceAll(opt, "-", "_")
	return "CCE_" + strings.ToUpper(under)
}

func getConfigString(cfg map[string]interface{}, key string) string {
	if cfg == nil {
		return ""
	}
	if val, ok := cfg[key]; ok && val != nil {
		switch v := val.(type) {
		case string:
			return v
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func getConfigBool(cfg map[string]interface{}, key string) bool {
	if cfg == nil {
		return false
	}
	if val, ok := cfg[key]; ok && val != nil {
		switch v := val.(type) {
		case bool:
			return v
		case string:
			parsed, err := strconv.ParseBool(v)
			if err == nil {
				return parsed
			}
		default:
			return false
		}
	}
	return false
}

func defaultBuildCloudClient(cfg clusterConfig) (cloud.Interface, error) {
	if cfg.BaseHost != "" {
		os.Setenv(ccegateway.EndpointOverrideEnv, cfg.BaseHost)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("bce-cloud-region is required in configmap")
	}
	if cfg.AccessKey == "" && cfg.SecretKey == "" && cfg.ClusterID == "" {
		return nil, fmt.Errorf("cce-cluster-id is required when ak/sk are not set")
	}
	client, err := newCloudClient(
		cfg.Region,
		cfg.ClusterID,
		cfg.AccessKey,
		cfg.SecretKey,
		cfg.ForceViaGateway,
		k8s.Client(),
		false,
		defaults.ClientConnectTimeout,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to init bce client: %w", err)
	}
	client, err = newFlowControl(client, defaults.CloudAPIQPSLimit, defaults.CloudAPIBurst, defaults.ClientConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to init bce client with flow control: %w", err)
	}
	return client, nil
}

func defaultDescribeSubnet(ctx context.Context, client cloud.Interface, subnetID string) (*vpc.Subnet, error) {
	return client.DescribeSubnet(ctx, subnetID)
}

func (h *MutatingPSTSHandler) validateReuseSubnet(
	sbn *ccev1.Subnet,
	customAllocation ccev2.CustomAllocationList,
	fldPath *field.Path,
	allErrs field.ErrorList,
	subnetClient SubnetClient,
) field.ErrorList {
	// No longer check Exclusive flag - PSTS can use any subnet with IPReserve mechanism

	// Wait for CIDR information to be synced using wait.Poll
	err := poll(200*time.Millisecond, 5*time.Second, func() (bool, error) {
		// Check if CIDR is available
		if sbn.Spec.CIDR != "" {
			return true, nil
		}

		// Re-fetch the subnet to get updated information
		updatedSbn, err := subnetClient.Lister().Get(sbn.Name)
		if err == nil && updatedSbn != nil {
			sbn = updatedSbn
		}

		return false, nil
	})

	if err != nil {
		return append(allErrs, field.Invalid(fldPath, sbn, "subnet CIDR information is not yet available after waiting, please retry later"))
	}

	var (
		cidrV4, cidrV6 *net.IPNet
		parseErr       error

		// indexRanges is used to record the range of the ip index
		// this is used to check whether the custom allocation ip is out of range
		indexRanges []*big.Int
		rangeLabels []string // human-readable label for each range pair (parallel with indexRanges, one per pair)
	)
	_, cidrV4, parseErr = net.ParseCIDR(sbn.Spec.CIDR)
	if parseErr != nil {
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

		// Validate Range field
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
			if first == nil || last == nil {
				return append(allErrs, field.Invalid(fldPath, sbn,
					fmt.Sprintf("ip range %s-%s contains invalid IP address", r.Start, r.End)))
			}
			if first.Cmp(last) > 0 {
				return append(allErrs, field.Invalid(fldPath, sbn,
					fmt.Sprintf("ip range start %s is greater than end %s", r.Start, r.End)))
			}

			// IPv6 range does not need webhook validation here:
			// Range (start-end) is automatically converted to CIDRs via IPRangeToCIDRs at reserve time,
			// so any valid range is acceptable regardless of size.

			indexRanges = append(indexRanges, first, last)
			rangeLabels = append(rangeLabels, fmt.Sprintf("%s-%s", r.Start, r.End))
		}

		// Validate CIDRs field
		for _, cidrStr := range custom.CIDRs {
			_, customCIDR, err := net.ParseCIDR(cidrStr)
			if err != nil {
				return append(allErrs, field.Invalid(fldPath, sbn, fmt.Sprintf("invalid CIDR %s: %v", cidrStr, err)))
			}

			// Check if CIDR is within subnet CIDR
			if !cidrContains(cidrNet, customCIDR) {
				return append(allErrs, field.Invalid(fldPath, sbn, fmt.Sprintf("CIDR %s is not within subnet CIDR", cidrStr)))
			}

			// Validate IPv6 CIDR prefix: VPC IPReserve requires prefix >= /96
			if custom.Family == ccev2.IPv6Family {
				ones, _ := customCIDR.Mask.Size()
				if ones < iputil.IPv6MinPrefixForIPReserve {
					return append(allErrs, field.Invalid(fldPath, cidrStr,
						fmt.Sprintf("IPv6 CIDR %s prefix /%d is too large, VPC IPReserve requires prefix >= /%d", cidrStr, ones, iputil.IPv6MinPrefixForIPReserve)))
				}
			}

			// Add CIDR range to indexRanges for overlap check
			first := k8sutilnet.BigForIP(customCIDR.IP)
			// Calculate last IP in CIDR
			lastIP := lastIPInCIDR(customCIDR)
			last := k8sutilnet.BigForIP(lastIP)
			if first == nil || last == nil {
				return append(allErrs, field.Invalid(fldPath, sbn, fmt.Sprintf("failed to convert CIDR %s to IP range", cidrStr)))
			}
			indexRanges = append(indexRanges, first, last)
			rangeLabels = append(rangeLabels, cidrStr)
		}
	}

	if idxA, idxB := overlappingCheck(indexRanges); idxA >= 0 {
		return append(allErrs, field.Invalid(fldPath, sbn,
			fmt.Sprintf("ip range is overlapping: %s and %s", rangeLabels[idxA], rangeLabels[idxB])))
	}

	return allErrs
}

// cidrContains checks if the child CIDR is completely within the parent CIDR
func cidrContains(parent, child *net.IPNet) bool {
	parentOnes, parentBits := parent.Mask.Size()
	childOnes, childBits := child.Mask.Size()

	// Child must have equal or more specific mask (larger prefix length)
	if childOnes < parentOnes || parentBits != childBits {
		return false
	}

	// Check if child's network is within parent's network
	return parent.Contains(child.IP)
}

// lastIPInCIDR calculates the last IP address in a CIDR block
func lastIPInCIDR(cidr *net.IPNet) net.IP {
	ip := cidr.IP
	mask := cidr.Mask

	// Ensure ip and mask have the same length
	// For IPv4, use 4-byte form; for IPv6, use 16-byte form
	if ipv4 := ip.To4(); ipv4 != nil {
		ip = ipv4
	} else {
		ip = ip.To16()
	}

	lastIP := make(net.IP, len(ip))
	copy(lastIP, ip)

	// Set all host bits to 1
	for i := range lastIP {
		lastIP[i] = ip[i] | ^mask[i]
	}

	return lastIP
}

// overlappingCheck checks if any ranges overlap
// returns the indices (pair index, not element index) of the first overlapping pair, or -1,-1 if no overlap
func overlappingCheck(indexRanges []*big.Int) (int, int) {
	for i := 0; i+1 < len(indexRanges); i = i + 2 {
		for j := i + 2; j+1 < len(indexRanges); j = j + 2 {
			a := indexRanges[i].Cmp(indexRanges[j])
			b := indexRanges[i+1].Cmp(indexRanges[j])
			c := indexRanges[i].Cmp(indexRanges[j+1])
			d := indexRanges[i+1].Cmp(indexRanges[j+1])
			// Two ranges overlap if the comparison results are not all the same,
			// OR if both ranges are identical (all comparisons equal zero)
			allEqual := a == 0 && b == 0 && c == 0 && d == 0
			if a != b || b != c || c != d || allEqual {
				return i / 2, j / 2
			}
		}
	}
	return -1, -1
}

var _ admission.DecoderInjector = &MutatingPSTSHandler{}

// InjectDecoder injects the decoder into the PodCreateHandler
func (h *MutatingPSTSHandler) InjectDecoder(d *admission.Decoder) error {
	h.Decoder = d
	return nil
}

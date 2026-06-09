package v2

import (
	"fmt"
	"strings"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// StatusOK represents the ok status
	StatusOK = "ok"
	// StatusFailed represents the failed status
	StatusFailed = "failed"
)

var (
	effectiveENIErrorDuration = 2 * time.Minute
)

// InstanceID returns the InstanceID of a NetResourceSet.
func (n *NetResourceSet) InstanceID() (instanceID string) {
	if n != nil {
		instanceID = n.Spec.InstanceID
	}
	return
}

// GetEffectiveInstanceID returns the effective instance ID based on the instance type.
// For HPAS instance type and HPASStanderd is true, it returns HPASID if available, otherwise returns an error.
// For other instance types, it returns the regular InstanceID.
func (n *NetResourceSet) GetEffectiveInstanceID(HPASStanderd bool) (string, error) {
	if n == nil {
		return "", fmt.Errorf("NetResourceSet is nil")
	}

	// 非标方案的，直接返回 instance-id
	if !HPASStanderd {
		return n.Spec.InstanceID, nil
	}

	// rdma_roce 类型的全部使用instance-id
	if n.Spec.ENI != nil && strings.EqualFold(n.Spec.ENI.InstanceType, string(ENIForHPC)) {
		return n.Spec.InstanceID, nil
	}

	// Check if this is a HPAS instance type
	if strings.EqualFold(n.Spec.InstanceType, string(metadata.InstanceTypeExHPAS)) {
		// For HPAS type, HPASID must exist
		// But if eni is rdma_roce, we should use the instanceID
		if n.Spec.HPASID == "" {
			return "", fmt.Errorf("HPAS instance type requires HPASID, but HPASID is empty")
		}
		return n.Spec.HPASID, nil
	}

	// For non-HPAS types, return regular instance ID
	return n.Spec.InstanceID, nil
}

// SpeculationNoMoreIPReson speculation on the reason for IP application failure in ENI mode
// when failed to allocate IP, it will return a list of error codes
func (n *NetResourceSet) SpeculationNoMoreIPReson(crossSubnet bool) error {
	if n.Spec.ENI == nil {
		return nil
	}
	var (
		LastAllocatedIPError *StatusChange
		allErrs              = CodeErrorList{}

		shouldWaitIPAvailable bool

		// capacity of eni
		eniIPCapacity = n.Spec.ENI.MaxIPsPerENI

		// Are all enis already full
		allENIFullAllocatedIP = true

		now = time.Now()
	)

	for _, eniStatistics := range n.Status.ENIs {
		if eniStatistics.AvailableIPNum > 0 {
			allENIFullAllocatedIP = false
			if !crossSubnet {
				if eniStatistics.IsMoreAvailableIPInSubnet {
					if !shouldWaitIPAvailable {
						shouldWaitIPAvailable = true
					}
				} else {
					allErrs = append(allErrs, NewCodeError(ErrorCodeENISubnetNoMoreIP, fmt.Sprintf("ENI %s with subnet %s has no more available IP", eniStatistics.ID, eniStatistics.SubnetID)))
				}
			}
		} else {
			allErrs = append(allErrs, NewCodeError(ErrorCodeENIIPCapacityExceed, fmt.Sprintf("ENI %s has no more capacity to allocate new IP, capacity is %d", eniStatistics.ID, eniIPCapacity)))
		}

		if eniStatistics.LastAllocatedIPError != nil {
			if LastAllocatedIPError != nil {
				if LastAllocatedIPError.Time.Before(&eniStatistics.LastAllocatedIPError.Time) {
					LastAllocatedIPError = eniStatistics.LastAllocatedIPError
				}
			} else {
				if now.Sub(eniStatistics.LastAllocatedIPError.Time.Time) > effectiveENIErrorDuration {
					allErrs = append(allErrs, NewCodeError(eniStatistics.LastAllocatedIPError.Code, fmt.Sprintf("eni %s allocate ip error: %s", eniStatistics.ID, eniStatistics.LastAllocatedIPError.Message)))
				}
			}
		}
	}

	if len(n.Status.IPAM.AvailableSubnetIDs) == 0 && n.Spec.ENI.MaxAllocateENI > len(n.Status.ENIs) {
		allErrs = append(allErrs, NewCodeError(ErrorCodeNoAvailableSubnet,
			fmt.Sprintf("subnets [%s] are not available for this node. please add another subnet and create new eni later", strings.Join(n.Spec.ENI.SubnetIDs, ","))))
	}

	if allENIFullAllocatedIP {
		// override allErrs
		allErrs = CodeErrorList{}
		if len(n.Status.ENIs) < n.Spec.ENI.MaxAllocateENI {
			allErrs = append(allErrs, NewCodeError(ErrorCodeWaitNewENIInuse, "all ENI's IP addresses are currently full. will try to apply for a new ENI again later"))
		} else {
			allErrs = append(allErrs, NewCodeError(ErrorCodeENICapacityExceed, "all ENIs and it's IPs have reached capacity, cannot allocate any more"))
		}
	}

	if len(allErrs) == 0 {
		allErrs = append(allErrs, NewCodeError(ErrorCodeIPPoolExhausted, "ip pool have been exhausted will refill  later. please try again"))
	}

	return allErrs.ToAggregate()
}

func NewErrorStatusChange(msg string) *StatusChange {
	return NewCustomerErrorStatusChange(StatusFailed, msg)
}

func NewOKStatusChange(msg string) *StatusChange {
	return NewCustomerErrorStatusChange(StatusOK, msg)
}

func NewCustomerErrorStatusChange(code, msg string) *StatusChange {
	return &StatusChange{
		Code:    code,
		Message: msg,
		Time:    metav1.Now(),
	}
}

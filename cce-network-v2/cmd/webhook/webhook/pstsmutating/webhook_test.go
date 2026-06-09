package pstsmutating

import (
	"errors"
	"math/big"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ccev1 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v1"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	ccev1lister "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/client/listers/cce.baidubce.com/v1"
)

// MockSubnetLister is a mock implementation of SubnetLister
type MockSubnetLister struct {
	subnets map[string]*ccev1.Subnet
	err     error
}

func (m *MockSubnetLister) Get(name string) (*ccev1.Subnet, error) {
	if m.err != nil {
		return nil, m.err
	}
	if subnet, exists := m.subnets[name]; exists {
		return subnet, nil
	}
	return nil, errors.New("not found")
}

func (m *MockSubnetLister) List(selector labels.Selector) (ret []*ccev1.Subnet, err error) {
	// Not implemented for this test
	return nil, nil
}

// MockSubnetUpdater is a mock implementation of subnetUpdaterImpl
type MockSubnetUpdater struct {
	lister *MockSubnetLister
}

func (m *MockSubnetUpdater) Lister() ccev1lister.SubnetLister {
	return m.lister
}

func (m *MockSubnetUpdater) Create(subnet *ccev1.Subnet) (*ccev1.Subnet, error) {
	return nil, nil
}

func (m *MockSubnetUpdater) Update(newResource *ccev1.Subnet) (*ccev1.Subnet, error) {
	return nil, nil
}

func (m *MockSubnetUpdater) Delete(name string) error {
	return nil
}

func (m *MockSubnetUpdater) UpdateStatus(newResource *ccev1.Subnet) (*ccev1.Subnet, error) {
	return nil, nil
}

func TestValidateReuseSubnet_NonExclusiveSubnet(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: false,
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for non-exclusive subnet
	if len(gotErr) == 0 {
		t.Error("expected error for non-exclusive subnet, got none")
		return
	}

	if gotErr[0].Detail != "subnet is not exclusive" {
		t.Errorf("expected 'subnet is not exclusive' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_WithExistingErrors(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: false, // This will trigger an error
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{}
	fldPath := field.NewPath("spec")
	existingErr := field.Invalid(field.NewPath("spec"), "existing-error", "existing error message")
	allErrs := field.ErrorList{existingErr}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should have both existing error and new error
	if len(gotErr) != 2 {
		t.Errorf("expected 2 errors, got %d", len(gotErr))
		return
	}

	// Check that existing error is preserved
	if gotErr[0].Detail != "existing error message" {
		t.Errorf("expected existing error to be preserved, got: %s", gotErr[0].Detail)
	}

	// Check that new error is added
	if gotErr[1].Detail != "subnet is not exclusive" {
		t.Errorf("expected 'subnet is not exclusive' error, got: %s", gotErr[1].Detail)
	}
}

func TestValidateReuseSubnet_ExclusiveSubnetWithValidCIDR(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{} // Empty custom allocation
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return no errors for valid exclusive subnet with empty custom allocation
	if len(gotErr) != 0 {
		t.Errorf("expected no errors, got %d errors: %v", len(gotErr), gotErr)
	}
}

func TestValidateReuseSubnet_ExclusiveSubnetWithInvalidCIDR(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "invalid-cidr", // Invalid CIDR
		},
	}
	customAllocation := ccev2.CustomAllocationList{}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for invalid CIDR
	if len(gotErr) == 0 {
		t.Error("expected error for invalid CIDR, got none")
		return
	}

	if gotErr[0].Detail != "cidr of subnet is invalide" {
		t.Errorf("expected 'cidr of subnet is invalide' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_IPv6CustomAllocationButNoIPv6CIDR(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
			IPv6CIDR:  "", // No IPv6 CIDR
		},
	}
	customAllocation := ccev2.CustomAllocationList{
		{
			Family: ccev2.IPv6Family,
			Range: []ccev2.CustomIPRange{
				{
					Start: "2001:db8::1",
					End:   "2001:db8::10",
				},
			},
		},
	}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for IPv6 allocation without IPv6 CIDR
	if len(gotErr) == 0 {
		t.Error("expected error for IPv6 allocation without IPv6 CIDR, got none")
		return
	}

	if gotErr[0].Detail != "subnet does not support ipv6" {
		t.Errorf("expected 'subnet does not support ipv6' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_EmptyIPRange(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{
		{
			Family: ccev2.IPv4Family,
			Range: []ccev2.CustomIPRange{
				{
					Start: "", // Empty start
					End:   "192.168.1.10",
				},
			},
		},
	}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for empty IP range
	if len(gotErr) == 0 {
		t.Error("expected error for empty IP range, got none")
		return
	}

	if gotErr[0].Detail != "ip range is empty" {
		t.Errorf("expected 'ip range is empty' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_IPRangeOutsideSubnet(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{
		{
			Family: ccev2.IPv4Family,
			Range: []ccev2.CustomIPRange{
				{
					Start: "10.0.0.1", // Outside subnet range
					End:   "10.0.0.10",
				},
			},
		},
	}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for IP range outside subnet
	if len(gotErr) == 0 {
		t.Error("expected error for IP range outside subnet, got none")
		return
	}

	if gotErr[0].Detail != "ip range is not in subnet" {
		t.Errorf("expected 'ip range is not in subnet' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_InvalidIPRange(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{
		{
			Family: ccev2.IPv4Family,
			Range: []ccev2.CustomIPRange{
				{
					Start: "192.168.1.10", // Start > End
					End:   "192.168.1.5",
				},
			},
		},
	}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for invalid IP range
	if len(gotErr) == 0 {
		t.Error("expected error for invalid IP range, got none")
		return
	}

	if gotErr[0].Detail != "ip range is invalid" {
		t.Errorf("expected 'ip range is invalid' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_OverlappingRanges(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{
		{
			Family: ccev2.IPv4Family,
			Range: []ccev2.CustomIPRange{
				{
					Start: "192.168.1.5",
					End:   "192.168.1.15",
				},
				{
					Start: "192.168.1.10", // Overlapping range
					End:   "192.168.1.20",
				},
			},
		},
	}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for overlapping ranges
	if len(gotErr) == 0 {
		t.Error("expected error for overlapping ranges, got none")
		return
	}

	if gotErr[0].Detail != "ip range is overlapping" {
		t.Errorf("expected 'ip range is overlapping' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_ValidNonOverlappingRanges(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
		},
	}
	customAllocation := ccev2.CustomAllocationList{
		{
			Family: ccev2.IPv4Family,
			Range: []ccev2.CustomIPRange{
				{
					Start: "192.168.1.5",
					End:   "192.168.1.10",
				},
				{
					Start: "192.168.1.15", // Non-overlapping range
					End:   "192.168.1.20",
				},
			},
		},
	}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return no errors for valid non-overlapping ranges
	if len(gotErr) != 0 {
		t.Errorf("expected no errors, got %d errors: %v", len(gotErr), gotErr)
	}
}

func TestValidateReuseSubnet_IPv6CIDRAndIPv6CustomAllocation(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
			IPv6CIDR:  "2001:db8::/64",
		},
	}
	customAllocation := ccev2.CustomAllocationList{
		{
			Family: ccev2.IPv6Family,
			Range: []ccev2.CustomIPRange{
				{
					Start: "2001:db8::1",
					End:   "2001:db8::10",
				},
			},
		},
	}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return no errors for valid IPv6 allocation
	if len(gotErr) != 0 {
		t.Errorf("expected no errors, got %d errors: %v", len(gotErr), gotErr)
	}
}

func TestValidateReuseSubnet_EmptyCIDRWithTimeout(t *testing.T) {
	mockLister := &MockSubnetLister{
		subnets: make(map[string]*ccev1.Subnet),
		err:     nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "", // Empty CIDR to trigger wait.Poll timeout
		},
	}
	customAllocation := ccev2.CustomAllocationList{}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return error for CIDR timeout
	if len(gotErr) == 0 {
		t.Error("expected error for CIDR timeout, got none")
		return
	}

	if gotErr[0].Detail != "subnet CIDR information is not yet available after waiting, please retry later" {
		t.Errorf("expected 'subnet CIDR information is not yet available after waiting, please retry later' error, got: %s", gotErr[0].Detail)
	}
}

func TestValidateReuseSubnet_EmptyCIDRWithUpdatedSubnet(t *testing.T) {
	// Create a subnet with CIDR that will be returned by the mock
	updatedSubnet := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "192.168.1.0/24",
		},
	}

	mockLister := &MockSubnetLister{
		subnets: map[string]*ccev1.Subnet{
			"test-subnet": updatedSubnet,
		},
		err: nil,
	}
	mockUpdater := &MockSubnetUpdater{
		lister: mockLister,
	}
	handler := &MutatingPSTSHandler{}

	sbn := &ccev1.Subnet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-subnet",
		},
		Spec: ccev1.SubnetSpec{
			Exclusive: true,
			CIDR:      "", // Empty CIDR initially
		},
	}
	customAllocation := ccev2.CustomAllocationList{}
	fldPath := field.NewPath("spec")
	allErrs := field.ErrorList{}

	gotErr := handler.validateReuseSubnet(sbn, customAllocation, fldPath, allErrs, mockUpdater)

	// Should return no errors because mock returns updated subnet with CIDR
	if len(gotErr) != 0 {
		t.Errorf("expected no errors, got %d errors: %v", len(gotErr), gotErr)
	}
}

func TestOverlappingCheck(t *testing.T) {
	cases := []struct {
		name        string
		indexRanges []*big.Int
		want        bool
	}{
		{
			name:        "Given_EmptyRanges_When_OverlappingCheck_Then_ReturnTrue",
			indexRanges: []*big.Int{},
			want:        true,
		},
		{
			name:        "Given_SingleRange_When_OverlappingCheck_Then_ReturnTrue",
			indexRanges: []*big.Int{big.NewInt(1), big.NewInt(10)},
			want:        true,
		},
		{
			name:        "Given_NonOverlappingRanges_When_OverlappingCheck_Then_ReturnTrue",
			indexRanges: []*big.Int{big.NewInt(1), big.NewInt(10), big.NewInt(20), big.NewInt(30)},
			want:        true,
		},
		{
			name:        "Given_OverlappingRanges_When_OverlappingCheck_Then_ReturnFalse",
			indexRanges: []*big.Int{big.NewInt(1), big.NewInt(15), big.NewInt(10), big.NewInt(20)},
			want:        false,
		},
		{
			name:        "Given_AdjacentRanges_When_OverlappingCheck_Then_ReturnTrue",
			indexRanges: []*big.Int{big.NewInt(1), big.NewInt(10), big.NewInt(11), big.NewInt(20)},
			want:        true,
		},
		{
			name:        "Given_ThreeNonOverlappingRanges_When_OverlappingCheck_Then_ReturnTrue",
			indexRanges: []*big.Int{big.NewInt(1), big.NewInt(10), big.NewInt(20), big.NewInt(30), big.NewInt(40), big.NewInt(50)},
			want:        true,
		},
		{
			name:        "Given_ThreeRangesWithOverlap_When_OverlappingCheck_Then_ReturnFalse",
			indexRanges: []*big.Int{big.NewInt(1), big.NewInt(15), big.NewInt(10), big.NewInt(20), big.NewInt(25), big.NewInt(35)},
			want:        false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := overlappingCheck(tc.indexRanges)
			if got != tc.want {
				t.Errorf("overlappingCheck(%v) = %v, want %v", tc.indexRanges, got, tc.want)
			}
		})
	}
}

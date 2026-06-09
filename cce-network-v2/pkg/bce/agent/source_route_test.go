package agent

import (
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/datapath/link"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/set"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
)

// mockENI returns a minimal ENI for testing
func mockENI(primary bool) *ccev2.ENI {
	return &ccev2.ENI{
		Spec: ccev2.ENISpec{
			ENI: models.ENI{
				MacAddress: "00:11:22:33:44:55",
				PrivateIPSet: []*models.PrivateIP{{
					Primary:          primary,
					PrivateIPAddress: "192.168.1.10",
					PublicIPAddress:  "1.2.3.4",
				}},
				IPV6PrivateIPSet: []*models.PrivateIP{{
					Primary:          primary,
					PrivateIPAddress: "fd00::1",
					PublicIPAddress:  "fd00::2",
				}},
			},
			RouteTableOffset: 1,
		},
		Status: ccev2.ENIStatus{
			ENIIndex: 10,
		},
	}
}

// TestEnsureENIRule_GivenNoIPs_WhenCalled_ThenNoError tests the case when ENI has no IPs
func TestEnsureENIRule_GivenNoIPs_WhenCalled_ThenNoError(t *testing.T) {
	eni := mockENI(false)
	eni.Spec.PrivateIPSet = nil
	eni.Spec.IPV6PrivateIPSet = nil
	log := logrus.NewEntry(logrus.New())
	err := ensureENIRule(log, eni)
	assert.NoError(t, err)
}

// TestEnsureENIRule_GivenRuleListError_WhenCalled_ThenError tests the case when RuleList returns error
func TestEnsureENIRule_GivenRuleListError_WhenCalled_ThenError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(netlink.RuleList, func(family int) ([]netlink.Rule, error) {
		return nil, errors.New("fail")
	})
	eni := mockENI(true)
	log := logrus.NewEntry(logrus.New())
	err := ensureENIRule(log, eni)
	assert.Error(t, err)
}

// TestEnsureERIRule_GivenNoIPs_WhenCalled_ThenNoError tests the case when ERI has no IPs
func TestEnsureERIRule_GivenNoIPs_WhenCalled_ThenNoError(t *testing.T) {
	eni := mockENI(false)
	eni.Spec.PrivateIPSet = nil
	eni.Spec.IPV6PrivateIPSet = nil
	log := logrus.NewEntry(logrus.New())
	err := ensureERIRule(log, eni)
	assert.NoError(t, err)
}

// TestEnsureERIRule_GivenRuleListError_WhenCalled_ThenError tests the case when RuleList returns error
func TestEnsureERIRule_GivenRuleListError_WhenCalled_ThenError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(netlink.RuleList, func(family int) ([]netlink.Rule, error) {
		return nil, fmt.Errorf("fail")
	})
	eni := mockENI(true)
	log := logrus.NewEntry(logrus.New())
	err := ensureERIRule(log, eni)
	assert.Error(t, err)
}

// TestEnsureENIRule_GivenRuleDelError_WhenCalled_ThenErrorReturned tests the case when RuleDel returns error
func TestEnsureENIRule_GivenRuleDelError_WhenCalled_ThenErrorReturned(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	// This rule should trigger the error branch: r.Table == ruleTable && ruleTable != mainRouteTableID
	// We need a rule with table=11 but NOT matching the priority conditions
	// For ENI: tableID = 10 + 1 = 11, fromRulePrority = 1036, toRulePrority = 523
	badRule := netlink.Rule{
		Src:      &net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(32, 32)}, // Different IP, not in SlinceContains
		Table:    11,                                                                  // Same table as ruleTable (11)
		Priority: 999,                                                                 // Different priority so it won't match fromRulePrority (1036) or toRulePrority (523)
	}
	patches.ApplyFunc(netlink.RuleList, func(family int) ([]netlink.Rule, error) {
		return []netlink.Rule{badRule}, nil
	})
	patches.ApplyFunc(netlink.RuleDel, func(rule *netlink.Rule) error {
		// Only return error for the specific rule we want to test
		if rule.Table == 11 && rule.Priority == 999 {
			return fmt.Errorf("del error")
		}
		return nil
	})
	patches.ApplyFunc(set.SlinceContains, func(slice []string, s string) bool {
		// Return false for "10.0.0.1" to ensure the rule goes to the error branch
		return false
	})
	eni := mockENI(true)
	log := logrus.NewEntry(logrus.New())
	err := ensureENIRule(log, eni)
	assert.Error(t, err)
}

// TestEnsureERIRule_GivenFindENILinkByMacError_WhenCalled_ThenError tests the case when FindENILinkByMac returns error
func TestEnsureERIRule_GivenFindENILinkByMacError_WhenCalled_ThenError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(netlink.RuleList, func(family int) ([]netlink.Rule, error) {
		return []netlink.Rule{}, nil
	})
	patches.ApplyFunc(netlink.RuleDel, func(rule *netlink.Rule) error { return nil })
	patches.ApplyFunc(netlink.RuleAdd, func(rule *netlink.Rule) error { return nil })
	patches.ApplyFunc(set.SlinceContains, func(slice []string, s string) bool { return false })
	patches.ApplyFunc(link.FindENILinkByMac, func(mac string) (netlink.Link, error) {
		return nil, fmt.Errorf("not found")
	})
	eni := mockENI(true)
	log := logrus.NewEntry(logrus.New())
	err := ensureERIRule(log, eni)
	assert.Error(t, err)
}

// TestEnsureERIRule_GivenNonPrimaryIPs_WhenCalled_ThenSkip tests the case when ERI has only non-primary IPs
func TestEnsureERIRule_GivenNonPrimaryIPs_WhenCalled_ThenSkip(t *testing.T) {
	// Only primary IPs should be processed
	eni := mockENI(false)
	for _, ip := range eni.Spec.PrivateIPSet {
		ip.Primary = false
	}
	for _, ip := range eni.Spec.IPV6PrivateIPSet {
		ip.Primary = false
	}
	log := logrus.NewEntry(logrus.New())
	err := ensureERIRule(log, eni)
	assert.NoError(t, err)
}

// TestEnsureERIRule_GivenRuleDelError_WhenCalled_ThenErrorReturned tests the case when RuleDel returns error for ERI
func TestEnsureERIRule_GivenRuleDelError_WhenCalled_ThenErrorReturned(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()

	// Rule that should trigger delete error branch: r.Table == ruleTable && ruleTable != mainRouteTableID
	// For ERI: tableID = 10 + 1 = 11, fromRulePrority = 1036, toRulePrority = 523
	badRule := netlink.Rule{
		Src:      &net.IPNet{IP: net.ParseIP("10.0.0.1"), Mask: net.CIDRMask(32, 32)}, // Different IP, not in SlinceContains
		Table:    11,                                                                  // Same table as ruleTable (11)
		Priority: 999,                                                                 // Different priority so it won't match fromRulePrority (1036) or toRulePrority (523)
	}
	patches.ApplyFunc(netlink.RuleList, func(family int) ([]netlink.Rule, error) {
		return []netlink.Rule{badRule}, nil
	})
	patches.ApplyFunc(netlink.RuleDel, func(rule *netlink.Rule) error {
		// Only return error for the specific rule we want to test
		if rule.Table == 11 && rule.Priority == 999 {
			return fmt.Errorf("del error")
		}
		return nil
	})
	patches.ApplyFunc(set.SlinceContains, func(slice []string, s string) bool {
		// Return false for "10.0.0.1" to ensure the rule goes to the error branch
		return false
	})
	patches.ApplyFunc(link.FindENILinkByMac, func(mac string) (netlink.Link, error) {
		return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "eth0", Index: 1}}, nil
	})

	eni := mockENI(true)
	log := logrus.NewEntry(logrus.New())
	err := ensureERIRule(log, eni)
	assert.Error(t, err)
}

// TestIsOldCCEERIRuleToDelete tests the isOldCCEERIRuleToDelete function
func TestIsOldCCEERIRuleToDelete(t *testing.T) {
	tests := []struct {
		name             string
		rule             netlink.Rule
		linkName         string
		tableID          int
		fromRulePriority int
		expected         bool
		description      string
	}{
		{
			name: "GivenCCEERIRuleWithMatchingConditions_WhenCalled_ThenReturnFalse",
			rule: netlink.Rule{
				OifName:  "cce-eri-test",
				Table:    100,
				Priority: 1036,
			},
			linkName:         "cce-eri-test",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         false,
			description:      "CCE-ERI rule with matching conditions should not be deleted",
		},
		{
			name: "GivenCCEERIRuleWithDifferentOifName_WhenCalled_ThenReturnTrue",
			rule: netlink.Rule{
				OifName:  "cce-eri-old",
				Table:    100,
				Priority: 1036,
			},
			linkName:         "cce-eri-new",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         true,
			description:      "CCE-ERI rule with different OifName should be deleted",
		},
		{
			name: "GivenCCEERIRuleWithDifferentTableID_WhenCalled_ThenReturnTrue",
			rule: netlink.Rule{
				OifName:  "cce-eri-test",
				Table:    200,
				Priority: 1036,
			},
			linkName:         "cce-eri-test",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         true,
			description:      "CCE-ERI rule with different TableID should be deleted",
		},
		{
			name: "GivenCCEERIRuleWithDifferentPriority_WhenCalled_ThenReturnTrue",
			rule: netlink.Rule{
				OifName:  "cce-eri-test",
				Table:    100,
				Priority: 2000,
			},
			linkName:         "cce-eri-test",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         true,
			description:      "CCE-ERI rule with different Priority should be deleted",
		},
		{
			name: "GivenNonCCEERIRule_WhenCalled_ThenReturnFalse",
			rule: netlink.Rule{
				OifName:  "eth0",
				Table:    100,
				Priority: 1036,
			},
			linkName:         "cce-eri-test",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         false,
			description:      "Non CCE-ERI rule should not be deleted",
		},
		{
			name: "GivenEmptyOifName_WhenCalled_ThenReturnFalse",
			rule: netlink.Rule{
				OifName:  "",
				Table:    100,
				Priority: 1036,
			},
			linkName:         "cce-eri-test",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         false,
			description:      "Rule with empty OifName should not be deleted",
		},
		{
			name: "GivenCCEERIRuleWithMultipleDifferences_WhenCalled_ThenReturnTrue",
			rule: netlink.Rule{
				OifName:  "cce-eri-old",
				Table:    200,
				Priority: 2000,
			},
			linkName:         "cce-eri-new",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         true,
			description:      "CCE-ERI rule with multiple differences should be deleted",
		},
		{
			name: "GivenCCEERIRuleWithPrefixButDifferentName_WhenCalled_ThenReturnTrue",
			rule: netlink.Rule{
				OifName:  "cce-eri-something-else",
				Table:    100,
				Priority: 1036,
			},
			linkName:         "cce-eri-test",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         true,
			description:      "CCE-ERI rule with different name but same prefix should be deleted",
		},
		{
			name: "GivenCCEERIRuleWithExactMatch_WhenCalled_ThenReturnFalse",
			rule: netlink.Rule{
				OifName:  "cce-eri-exact-match",
				Table:    100,
				Priority: 1036,
			},
			linkName:         "cce-eri-exact-match",
			tableID:          100,
			fromRulePriority: 1036,
			expected:         false,
			description:      "CCE-ERI rule with exact match should not be deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOldCCEERIRuleToDelete(tt.rule, tt.linkName, tt.tableID, tt.fromRulePriority)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

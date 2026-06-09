package main

import (
	"errors"
	"testing"
)

// MockIPTables implements IPTables interface for testing
type MockIPTables struct {
	Rules    map[string][]IPTablesRule
	Chains   map[string]bool
	AddError error
	DelError error
}

// NewMockIPTables creates a new MockIPTables instance
func NewMockIPTables() *MockIPTables {
	return &MockIPTables{
		Rules:  make(map[string][]IPTablesRule),
		Chains: make(map[string]bool),
	}
}

func ruleEqual(r1, r2 IPTablesRule) bool {
	if r1.Chain != r2.Chain {
		return false
	}
	if len(r1.Params) != len(r2.Params) {
		return false
	}
	for i := range r1.Params {
		if r1.Params[i] != r2.Params[i] {
			return false
		}
	}
	return true
}

// paramsEqual compares if two parameter lists are equal
func paramsEqual(p1, p2 []string) bool {
	if len(p1) != len(p2) {
		return false
	}
	for i := range p1 {
		if p1[i] != p2[i] {
			return false
		}
	}
	return true
}

func (m *MockIPTables) AddRules(rules []IPTablesRule) error {
	if m.AddError != nil {
		return m.AddError
	}

	for _, rule := range rules {
		// Check if rule already exists
		existingRules := m.Rules[rule.Chain]
		exists := false
		for _, r := range existingRules {
			if ruleEqual(r, rule) {
				exists = true
				break
			}
		}

		// If rule doesn't exist, add it
		if !exists {
			m.Rules[rule.Chain] = append(existingRules, rule)
		}
	}
	return nil
}

func (m *MockIPTables) DeleteRules(rules []IPTablesRule) error {
	if m.DelError != nil {
		return m.DelError
	}

	for _, rule := range rules {
		if existingRules, ok := m.Rules[rule.Chain]; ok {
			for i, r := range existingRules {
				if ruleEqual(r, rule) {
					m.Rules[rule.Chain] = append(existingRules[:i], existingRules[i+1:]...)
					break
				}
			}
		}
	}
	return nil
}

func (m *MockIPTables) ChainExists(chain string) bool {
	return m.Chains[chain]
}

func TestMockIPTables_AddRules(t *testing.T) {
	tests := []struct {
		name    string
		rules   []IPTablesRule
		wantErr bool
	}{
		{
			name: "add single rule",
			rules: []IPTablesRule{
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 1,
					Params:   []string{"-i", "eth0", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
			},
			wantErr: false,
		},
		{
			name: "add multiple rules",
			rules: []IPTablesRule{
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 1,
					Params:   []string{"-i", "eth0", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 2,
					Params:   []string{"-i", "eth1", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
			},
			wantErr: false,
		},
		{
			name: "add rules with error",
			rules: []IPTablesRule{
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 1,
					Params:   []string{"-i", "eth0", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockIPTables()
			if tt.wantErr {
				mock.AddError = errors.New("mock error")
			}

			err := mock.AddRules(tt.rules)
			if (err != nil) != tt.wantErr {
				t.Errorf("MockIPTables.AddRules() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify rule count
				rules := mock.Rules[CalicoForwardChain]
				if len(rules) != len(tt.rules) {
					t.Errorf("expected %d rules, got %d", len(tt.rules), len(rules))
				}

				// Verify table name
				for i, rule := range rules {
					if rule.Table != FilterTable {
						t.Errorf("rule %d: expected table %s, got %s", i, FilterTable, rule.Table)
					}
				}
			}
		})
	}
}

func TestMockIPTables_DeleteRules(t *testing.T) {
	tests := []struct {
		name    string
		rules   []IPTablesRule
		wantErr bool
	}{
		{
			name: "delete single rule",
			rules: []IPTablesRule{
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 1,
					Params:   []string{"-i", "eth0", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
			},
			wantErr: false,
		},
		{
			name: "delete multiple rules",
			rules: []IPTablesRule{
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 1,
					Params:   []string{"-i", "eth0", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 2,
					Params:   []string{"-i", "eth1", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
			},
			wantErr: false,
		},
		{
			name: "delete non-existent rules",
			rules: []IPTablesRule{
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 1,
					Params:   []string{"-i", "eth2", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
			},
			wantErr: false,
		},
		{
			name: "delete rules with error",
			rules: []IPTablesRule{
				{
					Table:    FilterTable,
					Chain:    CalicoForwardChain,
					Position: 1,
					Params:   []string{"-i", "eth0", "-p", ICMPProtocol, "-j", AcceptTarget},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockIPTables()
			if tt.wantErr {
				mock.DelError = errors.New("mock error")
			}

			// Add rules first
			for _, rule := range tt.rules {
				mock.Rules[rule.Chain] = append(mock.Rules[rule.Chain], rule)
			}

			err := mock.DeleteRules(tt.rules)
			if (err != nil) != tt.wantErr {
				t.Errorf("MockIPTables.DeleteRules() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Verify rules are deleted
				rules := mock.Rules[CalicoForwardChain]
				if len(rules) != 0 {
					t.Errorf("expected 0 rules after deletion, got %d", len(rules))
				}
			}
		})
	}
}

func TestMockIPTables_ChainExists(t *testing.T) {
	tests := []struct {
		name      string
		chain     string
		exists    bool
		setExists bool
	}{
		{
			name:      "chain exists",
			chain:     CalicoForwardChain,
			exists:    true,
			setExists: true,
		},
		{
			name:      "chain does not exist",
			chain:     CalicoForwardChain,
			exists:    false,
			setExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockIPTables()
			mock.Chains[tt.chain] = tt.setExists

			exists := mock.ChainExists(tt.chain)
			if exists != tt.exists {
				t.Errorf("MockIPTables.ChainExists() = %v, want %v", exists, tt.exists)
			}
		})
	}
}

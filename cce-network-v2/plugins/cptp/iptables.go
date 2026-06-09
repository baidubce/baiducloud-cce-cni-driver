package main

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	// Table names
	FilterTable = "filter"

	// Chain names
	CalicoForwardChain = "cali-FORWARD"

	// Rule parameters
	ICMPProtocol = "icmp"
	AcceptTarget = "ACCEPT"
)

// IPTablesRule represents a single iptables rule or multiple rules
type IPTablesRule struct {
	Table       string // Table name (e.g., "filter", "nat", "mangle")
	Chain       string
	Position    int
	Params      []string
	MultiParams [][]string
}

// IPTables interface for managing iptables rules
type IPTables interface {
	// AddRules adds rules to the specified chain
	AddRules(rules []IPTablesRule) error
	// DeleteRules deletes rules from the specified chain
	DeleteRules(rules []IPTablesRule) error
	// ChainExists checks if a chain exists
	ChainExists(chain string) bool
}

// RealIPTables implements IPTables interface using actual iptables command
type RealIPTables struct{}

// NewRealIPTables creates a new RealIPTables instance
func NewRealIPTables() *RealIPTables {
	return &RealIPTables{}
}

func (i *RealIPTables) AddRules(rules []IPTablesRule) error {
	for _, rule := range rules {
		// Check if rule already exists
		checkArgs := append([]string{"-t", rule.Table, "-C", rule.Chain}, rule.Params...)
		checkCmd := exec.Command("iptables", checkArgs...)
		if err := checkCmd.Run(); err == nil {
			// Rule exists, continue to next
			continue
		}

		// Rule doesn't exist, add new rule
		args := append([]string{"-t", rule.Table, "-I", rule.Chain, fmt.Sprintf("%d", rule.Position)}, rule.Params...)
		cmd := exec.Command("iptables", args...)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to add rule %v: %w", rule, err)
		}
	}
	return nil
}

func (i *RealIPTables) DeleteRules(rules []IPTablesRule) error {
	for _, rule := range rules {
		args := append([]string{"-t", rule.Table, "-D", rule.Chain}, rule.Params...)
		cmd := exec.Command("iptables", args...)
		if err := cmd.Run(); err != nil && !strings.Contains(err.Error(), "No chain/target/match") {
			return fmt.Errorf("failed to delete rule %v: %w", rule, err)
		}
	}
	return nil
}

func (i *RealIPTables) ChainExists(chain string) bool {
	cmd := exec.Command("iptables", "-t", FilterTable, "-L", chain)
	return cmd.Run() == nil
}

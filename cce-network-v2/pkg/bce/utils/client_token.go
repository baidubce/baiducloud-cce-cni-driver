/*
 * Copyright 2021 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/lock"
	"github.com/google/uuid"
)

const (
	// clientTokenTTL is the time-to-live for client tokens
	// After this duration, tokens will be automatically removed to prevent memory leaks
	clientTokenTTL = 10 * time.Minute
)

// clientTokenEntry stores a client token with its creation time
type clientTokenEntry struct {
	token     string
	createdAt time.Time
}

// ClientTokenSet manages client tokens for API idempotency (ENI/ERI creation, IP allocation)
type ClientTokenSet struct {
	mutex  *lock.Mutex
	tokens map[string]*clientTokenEntry // key: hash of parameters, value: token entry
}

// NewClientTokenSet creates a new ClientTokenSet
func NewClientTokenSet() *ClientTokenSet {
	return &ClientTokenSet{
		mutex:  &lock.Mutex{},
		tokens: make(map[string]*clientTokenEntry),
	}
}

// GetOrCreateClientToken returns existing client token for the given key,
// or creates a new one if not exists or expired
func (c *ClientTokenSet) GetOrCreateClientToken(key string) string {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	// Clean up expired tokens
	c.cleanupExpiredTokensLocked()

	if entry, ok := c.tokens[key]; ok {
		// Check if token is still valid
		if time.Since(entry.createdAt) < clientTokenTTL {
			return entry.token
		}
		// Token expired, remove it
		delete(c.tokens, key)
	}

	// Generate new UUID as client token
	token := uuid.New().String()
	c.tokens[key] = &clientTokenEntry{
		token:     token,
		createdAt: time.Now(),
	}
	return token
}

// RemoveClientToken removes the client token for the given key
func (c *ClientTokenSet) RemoveClientToken(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	delete(c.tokens, key)
}

// cleanupExpiredTokensLocked removes expired tokens (must be called with lock held)
func (c *ClientTokenSet) cleanupExpiredTokensLocked() {
	now := time.Now()
	for key, entry := range c.tokens {
		if now.Sub(entry.createdAt) >= clientTokenTTL {
			delete(c.tokens, key)
		}
	}
}

// GenerateENIClientTokenKey generates a deterministic key for ENI/ERI creation based on fixed parameters
// This function is used by both ENI and ERI creation processes
func GenerateENIClientTokenKey(instanceID, subnetID string, securityGroupIDs, enterpriseSecurityGroupIDs []string) string {
	// Sort security group IDs to ensure consistent ordering
	sgIDs := make([]string, len(securityGroupIDs))
	copy(sgIDs, securityGroupIDs)
	sort.Strings(sgIDs)

	esgIDs := make([]string, len(enterpriseSecurityGroupIDs))
	copy(esgIDs, enterpriseSecurityGroupIDs)
	sort.Strings(esgIDs)

	// Format: instanceID-subnetID-sgIDs-esgIDs
	data := fmt.Sprintf("%s-%s-%v-%v", instanceID, subnetID, sgIDs, esgIDs)

	// Generate SHA256 hash
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GenerateBatchAddIPClientTokenKey generates a deterministic key for BatchAddPrivateIP based on fixed parameters
func GenerateBatchAddIPClientTokenKey(eniID string, count int, isIPv6 bool) string {
	// Format: eniID-count-ipv6flag
	data := fmt.Sprintf("%s-%d-%t", eniID, count, isIPv6)

	// Generate SHA256 hash
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

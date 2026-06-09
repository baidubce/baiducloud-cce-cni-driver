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
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNewClientTokenSet tests the creation of ClientTokenSet
func TestNewClientTokenSet(t *testing.T) {
	tokenSet := NewClientTokenSet()

	assert.NotNil(t, tokenSet, "Token set should not be nil")
	assert.NotNil(t, tokenSet.mutex, "Mutex should not be nil")
	assert.NotNil(t, tokenSet.tokens, "Tokens map should not be nil")
	assert.Empty(t, tokenSet.tokens, "Tokens map should be empty initially")
}

// TestClientTokenSetGetOrCreate tests the GetOrCreateClientToken method
func TestClientTokenSetGetOrCreate(t *testing.T) {
	t.Run("creates new token for new key", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key := "new-key"

		token := tokenSet.GetOrCreateClientToken(key)

		assert.NotEmpty(t, token, "Token should not be empty")
		assert.Contains(t, tokenSet.tokens, key, "Key should exist in map")
		assert.Equal(t, token, tokenSet.tokens[key].token, "Token should match stored value")

		// Verify it's a valid UUID format
		assert.Regexp(t, regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"), token, "Token should be valid UUID")
	})

	t.Run("returns existing token for existing key", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key := "existing-key"

		token1 := tokenSet.GetOrCreateClientToken(key)
		token2 := tokenSet.GetOrCreateClientToken(key)

		assert.Equal(t, token1, token2, "Should return same token for same key")
		assert.Len(t, tokenSet.tokens, 1, "Should only have one entry in map")
	})

	t.Run("creates different tokens for different keys", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key1 := "key-1"
		key2 := "key-2"

		token1 := tokenSet.GetOrCreateClientToken(key1)
		token2 := tokenSet.GetOrCreateClientToken(key2)

		assert.NotEqual(t, token1, token2, "Different keys should have different tokens")
		assert.Len(t, tokenSet.tokens, 2, "Should have two entries in map")
	})
}

// TestClientTokenSetRemove tests the RemoveClientToken method
func TestClientTokenSetRemove(t *testing.T) {
	t.Run("removes existing token", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key := "test-key"

		// Create token
		token1 := tokenSet.GetOrCreateClientToken(key)
		assert.Contains(t, tokenSet.tokens, key, "Key should exist before removal")

		// Remove token
		tokenSet.RemoveClientToken(key)
		assert.NotContains(t, tokenSet.tokens, key, "Key should not exist after removal")

		// Get token again should create new one
		token2 := tokenSet.GetOrCreateClientToken(key)
		assert.NotEqual(t, token1, token2, "New token should be different after removal")
	})

	t.Run("removing non-existent key should not panic", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key := "non-existent-key"

		assert.NotPanics(t, func() {
			tokenSet.RemoveClientToken(key)
		}, "Removing non-existent key should not panic")
	})

	t.Run("removes only specified key", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key1 := "key-1"
		key2 := "key-2"

		_ = tokenSet.GetOrCreateClientToken(key1)
		token2 := tokenSet.GetOrCreateClientToken(key2)

		// Remove only key1
		tokenSet.RemoveClientToken(key1)

		assert.NotContains(t, tokenSet.tokens, key1, "key1 should be removed")
		assert.Contains(t, tokenSet.tokens, key2, "key2 should still exist")
		assert.Equal(t, token2, tokenSet.tokens[key2].token, "key2 token should be unchanged")
	})
}

// TestClientTokenSetConcurrency tests concurrent access to ClientTokenSet
func TestClientTokenSetConcurrency(t *testing.T) {
	t.Run("concurrent GetOrCreate on same key", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key := "concurrent-key"
		numGoroutines := 100
		results := make(chan string, numGoroutines)

		// Launch multiple goroutines
		for i := 0; i < numGoroutines; i++ {
			go func() {
				token := tokenSet.GetOrCreateClientToken(key)
				results <- token
			}()
		}

		// Collect results
		tokens := make([]string, numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			tokens[i] = <-results
		}

		// All tokens should be the same
		firstToken := tokens[0]
		for i := 1; i < numGoroutines; i++ {
			assert.Equal(t, firstToken, tokens[i], "All concurrent calls should return same token")
		}
	})
}

// TestClientTokenSetExpiration tests token expiration functionality
func TestClientTokenSetExpiration(t *testing.T) {
	t.Run("expired token should be recreated", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key := "test-key"

		// Create token
		token1 := tokenSet.GetOrCreateClientToken(key)
		assert.NotEmpty(t, token1, "Token should not be empty")

		// Manually expire the token by setting createdAt to past
		tokenSet.mutex.Lock()
		if entry, ok := tokenSet.tokens[key]; ok {
			entry.createdAt = time.Now().Add(-11 * time.Minute) // Older than TTL
		}
		tokenSet.mutex.Unlock()

		// Get token again should create new one
		token2 := tokenSet.GetOrCreateClientToken(key)
		assert.NotEqual(t, token1, token2, "Expired token should be replaced with new one")
	})

	t.Run("cleanup removes expired tokens", func(t *testing.T) {
		tokenSet := NewClientTokenSet()
		key1 := "key-1"
		key2 := "key-2"

		// Create two tokens
		_ = tokenSet.GetOrCreateClientToken(key1)
		_ = tokenSet.GetOrCreateClientToken(key2)

		// Manually expire key1
		tokenSet.mutex.Lock()
		if entry, ok := tokenSet.tokens[key1]; ok {
			entry.createdAt = time.Now().Add(-11 * time.Minute)
		}
		tokenSet.mutex.Unlock()

		// Trigger cleanup by getting a new token
		_ = tokenSet.GetOrCreateClientToken("key-3")

		// key1 should be removed, key2 should still exist
		tokenSet.mutex.Lock()
		_, key1Exists := tokenSet.tokens[key1]
		_, key2Exists := tokenSet.tokens[key2]
		tokenSet.mutex.Unlock()

		assert.False(t, key1Exists, "Expired key1 should be removed")
		assert.True(t, key2Exists, "Non-expired key2 should still exist")
	})
}

// TestGenerateENIClientTokenKey tests the GenerateENIClientTokenKey function
func TestGenerateENIClientTokenKey(t *testing.T) {
	t.Run("generates valid hash", func(t *testing.T) {
		key := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1"}, []string{"esg-1"})
		assert.Len(t, key, 64)
		assert.Regexp(t, regexp.MustCompile("^[0-9a-f]{64}$"), key)
	})

	t.Run("same parameters produce same key", func(t *testing.T) {
		key1 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1", "sg-2"}, []string{"esg-1"})
		key2 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1", "sg-2"}, []string{"esg-1"})
		assert.Equal(t, key1, key2)
	})

	t.Run("different order of security groups produce same key", func(t *testing.T) {
		key1 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-2", "sg-1"}, []string{"esg-2", "esg-1"})
		key2 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1", "sg-2"}, []string{"esg-1", "esg-2"})
		assert.Equal(t, key1, key2)
	})

	t.Run("different instanceID produces different key", func(t *testing.T) {
		key1 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1"}, []string{"esg-1"})
		key2 := GenerateENIClientTokenKey("i-456", "sbn-456", []string{"sg-1"}, []string{"esg-1"})
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different subnetID produces different key", func(t *testing.T) {
		key1 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1"}, []string{"esg-1"})
		key2 := GenerateENIClientTokenKey("i-123", "sbn-789", []string{"sg-1"}, []string{"esg-1"})
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different security groups produce different key", func(t *testing.T) {
		key1 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1"}, []string{"esg-1"})
		key2 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-2"}, []string{"esg-1"})
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different enterprise security groups produce different key", func(t *testing.T) {
		key1 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1"}, []string{"esg-1"})
		key2 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{"sg-1"}, []string{"esg-2"})
		assert.NotEqual(t, key1, key2)
	})

	t.Run("empty security groups", func(t *testing.T) {
		key1 := GenerateENIClientTokenKey("i-123", "sbn-456", []string{}, []string{})
		key2 := GenerateENIClientTokenKey("i-123", "sbn-456", nil, nil)
		assert.NotEmpty(t, key1)
		assert.NotEmpty(t, key2)
	})
}

// TestGenerateBatchAddIPClientTokenKey tests the GenerateBatchAddIPClientTokenKey function
func TestGenerateBatchAddIPClientTokenKey(t *testing.T) {
	t.Run("generates valid hash", func(t *testing.T) {
		key := GenerateBatchAddIPClientTokenKey("eni-123", 5, false)
		assert.Len(t, key, 64)
		assert.Regexp(t, regexp.MustCompile("^[0-9a-f]{64}$"), key)
	})

	t.Run("same parameters produce same key", func(t *testing.T) {
		key1 := GenerateBatchAddIPClientTokenKey("eni-123", 5, false)
		key2 := GenerateBatchAddIPClientTokenKey("eni-123", 5, false)
		assert.Equal(t, key1, key2)
	})

	t.Run("different eniID produces different key", func(t *testing.T) {
		key1 := GenerateBatchAddIPClientTokenKey("eni-123", 5, false)
		key2 := GenerateBatchAddIPClientTokenKey("eni-456", 5, false)
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different count produces different key", func(t *testing.T) {
		key1 := GenerateBatchAddIPClientTokenKey("eni-123", 5, false)
		key2 := GenerateBatchAddIPClientTokenKey("eni-123", 10, false)
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different isIPv6 produces different key", func(t *testing.T) {
		key1 := GenerateBatchAddIPClientTokenKey("eni-123", 5, false)
		key2 := GenerateBatchAddIPClientTokenKey("eni-123", 5, true)
		assert.NotEqual(t, key1, key2)
	})

	t.Run("IPv4 and IPv6 keys are different", func(t *testing.T) {
		ipv4Key := GenerateBatchAddIPClientTokenKey("eni-123", 5, false)
		ipv6Key := GenerateBatchAddIPClientTokenKey("eni-123", 5, true)
		assert.NotEqual(t, ipv4Key, ipv6Key)
	})
}

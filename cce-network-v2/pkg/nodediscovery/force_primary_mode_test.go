package nodediscovery

import (
	"errors"
	"testing"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestDetermineForcePrimaryENIMode(t *testing.T) {
	tests := []struct {
		name         string
		currentMode  string
		patterns     []string
		spec         string
		fetchErr     error
		expectedMode string
		forced       bool
		expectErr    bool
	}{
		{
			name:         "no patterns keeps current mode",
			currentMode:  string(ccev2.ENIUseModeSecondaryIP),
			patterns:     nil,
			spec:         "bcc.g1",
			expectedMode: string(ccev2.ENIUseModeSecondaryIP),
		},
		{
			name:         "already primary mode keeps value",
			currentMode:  string(ccev2.ENIUseModePrimaryIP),
			patterns:     []string{"bcc.*"},
			spec:         "bcc.mock",
			expectedMode: string(ccev2.ENIUseModePrimaryIP),
		},
		{
			name:         "pattern match forces mode",
			currentMode:  string(ccev2.ENIUseModeSecondaryIP),
			patterns:     []string{"hpc.*", "bcc.g1*"},
			spec:         "bcc.g1.c2m4",
			expectedMode: string(ccev2.ENIUseModePrimaryWithSecondaryIP),
			forced:       true,
		},
		{
			name:         "fetch error bubbles up",
			currentMode:  string(ccev2.ENIUseModeSecondaryIP),
			patterns:     []string{"*"},
			fetchErr:     errors.New("boom"),
			expectedMode: string(ccev2.ENIUseModeSecondaryIP),
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetchCalled := false
			mode, spec, err := determineForcePrimaryENIMode(tt.currentMode, tt.patterns, func() (string, error) {
				fetchCalled = true
				return tt.spec, tt.fetchErr
			})
			forced := spec != ""
			if len(tt.patterns) == 0 || tt.currentMode == string(ccev2.ENIUseModePrimaryIP) {
				assert.False(t, forced)
			}
			assert.Equal(t, tt.expectedMode, mode)
			assert.Equal(t, tt.forced, forced)
			if tt.forced {
				assert.Equal(t, tt.spec, spec)
			} else {
				assert.Empty(t, spec)
			}
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			if len(tt.patterns) == 0 || (tt.currentMode != "" && tt.currentMode != string(ccev2.ENIUseModeSecondaryIP)) {
				assert.False(t, fetchCalled)
			}
		})
	}
}

func TestMatchAnyPattern(t *testing.T) {
	assert.True(t, utils.MatchAnyPattern([]string{"bcc.*"}, "bcc.g2.c4m16"))
	assert.True(t, utils.MatchAnyPattern([]string{"*", "other"}, "whatever"))
	assert.False(t, utils.MatchAnyPattern([]string{"ebc.*"}, "bcc.g2"))
	assert.False(t, utils.MatchAnyPattern([]string{}, "bcc.g2"))
	assert.False(t, utils.MatchAnyPattern([]string{"   "}, "bcc.g2"))
}

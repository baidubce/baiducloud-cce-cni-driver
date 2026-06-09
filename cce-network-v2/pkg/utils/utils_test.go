package utils

import "testing"

func TestMatchAnyPattern(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		value    string
		expected bool
	}{
		{
			name:     "empty value returns false",
			patterns: []string{"*"},
			value:    "   ",
			expected: false,
		},
		{
			name:     "nil patterns returns false",
			patterns: nil,
			value:    "foo",
			expected: false,
		},
		{
			name:     "matches wildcard",
			patterns: []string{"*"},
			value:    "bar",
			expected: true,
		},
		{
			name:     "trims and lowers value and pattern",
			patterns: []string{"  FoO*  "},
			value:    "  fOoBar  ",
			expected: true,
		},
		{
			name:     "skips empty patterns",
			patterns: []string{"", "   ", "foo"},
			value:    "foo",
			expected: true,
		},
		{
			name:     "invalid pattern is ignored",
			patterns: []string{"[abc"},
			value:    "abc",
			expected: false,
		},
		{
			name:     "invalid pattern does not block later match",
			patterns: []string{"[abc", "bar*"},
			value:    "bar1",
			expected: true,
		},
		{
			name:     "matches question mark pattern",
			patterns: []string{"file?.txt"},
			value:    "file1.txt",
			expected: true,
		},
		{
			name:     "no match returns false",
			patterns: []string{"foo*", "bar"},
			value:    "baz",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchAnyPattern(tt.patterns, tt.value)
			if got != tt.expected {
				t.Fatalf("MatchAnyPattern(%v, %q) = %v, want %v",
					tt.patterns, tt.value, got, tt.expected)
			}
		})
	}
}

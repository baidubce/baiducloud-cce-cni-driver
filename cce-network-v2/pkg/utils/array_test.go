package utils

import (
	"testing"
)

func TestStrArrContains(t *testing.T) {
	tests := []struct {
		name     string
		arr      []string
		str      string
		expected bool
	}{
		{
			name:     "array contains string",
			arr:      []string{"apple", "banana", "orange"},
			str:      "banana",
			expected: true,
		},
		{
			name:     "array does not contain string",
			arr:      []string{"apple", "banana", "orange"},
			str:      "grape",
			expected: false,
		},
		{
			name:     "empty array",
			arr:      []string{},
			str:      "apple",
			expected: false,
		},
		{
			name:     "nil array",
			arr:      nil,
			str:      "apple",
			expected: false,
		},
		{
			name:     "empty string in array",
			arr:      []string{"apple", "", "orange"},
			str:      "",
			expected: true,
		},
		{
			name:     "empty string not in array",
			arr:      []string{"apple", "banana", "orange"},
			str:      "",
			expected: false,
		},
		{
			name:     "case sensitive - exact match",
			arr:      []string{"Apple", "Banana", "Orange"},
			str:      "Banana",
			expected: true,
		},
		{
			name:     "case sensitive - different case",
			arr:      []string{"Apple", "Banana", "Orange"},
			str:      "banana",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StrArrContains(tt.arr, tt.str)
			if result != tt.expected {
				t.Errorf("StrArrContains(%v, %q) = %v, want %v", 
					tt.arr, tt.str, result, tt.expected)
			}
		})
	}
}
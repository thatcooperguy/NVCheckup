package common

import (
	"testing"
)

func TestFormatPCIeGen(t *testing.T) {
	tests := []struct {
		gen      int
		expected string
	}{
		{1, "Gen1"},
		{3, "Gen3"},
		{4, "Gen4"},
		{5, "Gen5"},
	}

	for _, tt := range tests {
		result := formatPCIeGen(tt.gen)
		if result != tt.expected {
			t.Errorf("formatPCIeGen(%d) = %s, want %s", tt.gen, result, tt.expected)
		}
	}
}

func TestFormatPCIeWidth(t *testing.T) {
	tests := []struct {
		width    int
		expected string
	}{
		{1, "x1"},
		{4, "x4"},
		{8, "x8"},
		{16, "x16"},
	}

	for _, tt := range tests {
		result := formatPCIeWidth(tt.width)
		if result != tt.expected {
			t.Errorf("formatPCIeWidth(%d) = %s, want %s", tt.width, result, tt.expected)
		}
	}
}

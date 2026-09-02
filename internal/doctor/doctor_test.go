package doctor

import (
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestChooseMode(t *testing.T) {
	tests := []struct {
		useCase, issue, goos string
		want                 types.RunMode
	}{
		{"a", "b", "windows", types.ModeGaming},
		{"b", "b", "windows", types.ModeAI},
		{"c", "e", "windows", types.ModeCreator},
		{"d", "d", "windows", types.ModeStreaming},
		{"e", "e", "windows", types.ModeFull},
		{"", "", "linux", types.ModeFull},
		// Crashes override AI so the Windows event logs are collected.
		{"b", "a", "windows", types.ModeGaming},
		{"b", "a", "linux", types.ModeGaming},
		// Crashes do not downgrade gaming/creator/streaming/full.
		{"c", "a", "windows", types.ModeCreator},
		{"e", "a", "windows", types.ModeFull},
		// GPU not detected: ai on Linux, full on Windows.
		{"a", "c", "linux", types.ModeAI},
		{"a", "c", "windows", types.ModeFull},
		{"b", "c", "windows", types.ModeFull},
	}
	for _, tt := range tests {
		if got := chooseMode(tt.useCase, tt.issue, tt.goos); got != tt.want {
			t.Errorf("chooseMode(%q, %q, %q) = %s, want %s", tt.useCase, tt.issue, tt.goos, got, tt.want)
		}
	}
}

func TestIsYes(t *testing.T) {
	for _, s := range []string{"a", "A", "y", "yes", " YES "} {
		if !isYes(s, "a") {
			t.Errorf("isYes(%q) should be true", s)
		}
	}
	for _, s := range []string{"", "b", "no", "n"} {
		if isYes(s, "a") {
			t.Errorf("isYes(%q) should be false", s)
		}
	}
}

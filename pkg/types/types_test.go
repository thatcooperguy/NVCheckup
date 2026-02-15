package types

import (
	"testing"
)

func TestDefaultRunConfig(t *testing.T) {
	cfg := DefaultRunConfig()

	if cfg.Mode != ModeFull {
		t.Errorf("expected ModeFull, got %s", cfg.Mode)
	}
	if !cfg.Redact {
		t.Error("redaction should be enabled by default")
	}
	if cfg.IncludeLogs {
		t.Error("include-logs should be disabled by default")
	}
	if cfg.Zip {
		t.Error("zip should be disabled by default")
	}
	if cfg.JSON {
		t.Error("json should be disabled by default")
	}
	if cfg.Timeout != 30 {
		t.Errorf("expected timeout 30, got %d", cfg.Timeout)
	}
}

func TestRunModeConstants(t *testing.T) {
	modes := []RunMode{ModeGaming, ModeAI, ModeCreator, ModeStreaming, ModeFull}
	expected := []string{"gaming", "ai", "creator", "streaming", "full"}

	for i, mode := range modes {
		if string(mode) != expected[i] {
			t.Errorf("mode %d: expected %q, got %q", i, expected[i], mode)
		}
	}
}

func TestSeverityConstants(t *testing.T) {
	if string(SeverityInfo) != "INFO" {
		t.Error("SeverityInfo should be INFO")
	}
	if string(SeverityWarn) != "WARN" {
		t.Error("SeverityWarn should be WARN")
	}
	if string(SeverityCrit) != "CRIT" {
		t.Error("SeverityCrit should be CRIT")
	}
}

func TestExitCodes(t *testing.T) {
	if ExitOK != 0 {
		t.Error("ExitOK should be 0")
	}
	if ExitWarnings != 1 {
		t.Error("ExitWarnings should be 1")
	}
	if ExitCritical != 2 {
		t.Error("ExitCritical should be 2")
	}
	if ExitError != 3 {
		t.Error("ExitError should be 3")
	}
}

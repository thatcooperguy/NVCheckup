package util

import (
	"runtime"
	"testing"
)

func TestRunCommandSuccess(t *testing.T) {
	var r CommandResult
	if runtime.GOOS == "windows" {
		r = RunCommand(5, "cmd", "/c", "echo hello")
	} else {
		r = RunCommand(5, "echo", "hello")
	}
	if r.Err != nil {
		t.Fatalf("expected no error, got: %v", r.Err)
	}
	if r.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", r.ExitCode)
	}
	if r.Stdout != "hello" {
		t.Errorf("expected 'hello', got '%s'", r.Stdout)
	}
	if r.TimedOut {
		t.Error("should not have timed out")
	}
}

func TestRunCommandTimeout(t *testing.T) {
	var r CommandResult
	if runtime.GOOS == "windows" {
		r = RunCommand(1, "cmd", "/c", "ping -n 10 127.0.0.1")
	} else {
		r = RunCommand(1, "sleep", "10")
	}
	if !r.TimedOut {
		t.Error("expected timeout")
	}
	if r.ExitCode != -1 {
		t.Errorf("expected exit code -1 on timeout, got %d", r.ExitCode)
	}
}

func TestRunCommandNotFound(t *testing.T) {
	r := RunCommand(5, "this-command-does-not-exist-12345")
	if r.Err == nil {
		t.Error("expected error for missing command")
	}
}

func TestCommandExists(t *testing.T) {
	// echo should exist on all platforms
	var cmd string
	if runtime.GOOS == "windows" {
		cmd = "cmd"
	} else {
		cmd = "echo"
	}
	if !CommandExists(cmd) {
		t.Errorf("expected %s to exist", cmd)
	}

	if CommandExists("this-command-does-not-exist-12345") {
		t.Error("expected non-existent command to return false")
	}
}

func TestIsWindows(t *testing.T) {
	result := IsWindows()
	if runtime.GOOS == "windows" && !result {
		t.Error("expected true on Windows")
	}
	if runtime.GOOS != "windows" && result {
		t.Error("expected false on non-Windows")
	}
}

func TestIsLinux(t *testing.T) {
	result := IsLinux()
	if runtime.GOOS == "linux" && !result {
		t.Error("expected true on Linux")
	}
	if runtime.GOOS != "linux" && result {
		t.Error("expected false on non-Linux")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		input []string
		want  string
	}{
		{[]string{"", "", "hello"}, "hello"},
		{[]string{"first", "second"}, "first"},
		{[]string{"", ""}, ""},
		{[]string{" ", "x"}, "x"},
		{[]string{"  spaces  ", "other"}, "spaces"},
	}
	for _, tt := range tests {
		got := FirstNonEmpty(tt.input...)
		if got != tt.want {
			t.Errorf("FirstNonEmpty(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		s      string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 8, "hello..."},
		{"hi", 2, "hi"},
		{"hello", 5, "hello"},
		{"hello", 4, "h..."},
	}
	for _, tt := range tests {
		got := TruncateString(tt.s, tt.maxLen)
		if got != tt.want {
			t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
		}
	}
}

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		line  string
		sep   string
		wantK string
		wantV string
	}{
		{"NAME=Ubuntu", "=", "NAME", "Ubuntu"},
		{"key: value", ":", "key", "value"},
		{"no-separator", "=", "", ""},
		{"multi=part=value", "=", "multi", "part=value"},
	}
	for _, tt := range tests {
		k, v := ParseKeyValue(tt.line, tt.sep)
		if k != tt.wantK || v != tt.wantV {
			t.Errorf("ParseKeyValue(%q, %q) = (%q, %q), want (%q, %q)",
				tt.line, tt.sep, k, v, tt.wantK, tt.wantV)
		}
	}
}

// Package util provides shared utilities for NVCheckup.
package util

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// CommandResult holds the output of a command execution
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	TimedOut bool
	Duration time.Duration
}

// defaultTimeoutSec is used when a caller passes a zero or negative timeout,
// which would otherwise make every command fail instantly.
const defaultTimeoutSec = 30

// waitDelay bounds how long Run blocks after the context expires while a
// grandchild (e.g. ping.exe spawned by cmd.exe) still holds the stdout pipe.
// Without it a 1 s timeout was observed to take ~9 s to return.
const waitDelay = 2 * time.Second

// RunCommand executes a command with a timeout. Never panics; always returns a result.
func RunCommand(timeoutSec int, name string, args ...string) CommandResult {
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeoutSec
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	if runtime.GOOS == "windows" {
		// Killing only the direct child leaves its children (a shell's ping,
		// PowerShell's native helpers) running and holding our pipes. taskkill
		// /T walks the whole tree; fall back to Kill if taskkill itself fails.
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return nil
			}
			err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
			if err != nil {
				return cmd.Process.Kill()
			}
			return nil
		}
	}

	err := cmd.Run()
	duration := time.Since(start)

	result := CommandResult{
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
		Duration: duration,
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Err = fmt.Errorf("command timed out after %ds: %s", timeoutSec, name)
		result.ExitCode = -1
		return result
	}

	if err != nil {
		result.Err = err
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	return result
}

// CommandExists checks if a command is available in PATH
func CommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// IsWindows returns true if running on Windows
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

// IsLinux returns true if running on Linux
func IsLinux() bool {
	return runtime.GOOS == "linux"
}

// GetArch returns the architecture string
func GetArch() string {
	return runtime.GOARCH
}

// FirstNonEmpty returns the first non-empty string
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// TruncateString truncates a string to maxLen chars, appending "..." if truncated
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// ParseKeyValue parses a "key=value" or "key: value" line
func ParseKeyValue(line, sep string) (string, string) {
	parts := strings.SplitN(line, sep, 2)
	if len(parts) != 2 {
		return "", ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

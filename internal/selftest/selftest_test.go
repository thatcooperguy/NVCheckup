package selftest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/util"
)

func TestFailureDetailPrefersStderrThenStdoutThenErr(t *testing.T) {
	exit2 := errors.New("exit status 2")

	// nvidia-smi rejects an unknown field on STDOUT with exit 2 and an empty
	// stderr; the stdout line must be surfaced, not the bare Go error.
	r := util.CommandResult{Stdout: "Field \"power.state\" is not a valid field to query.\n\n", ExitCode: 2, Err: exit2}
	if got := failureDetail(r); got != `Field "power.state" is not a valid field to query.` {
		t.Errorf("stdout reason not surfaced: %q", got)
	}

	r = util.CommandResult{Stderr: "NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver.\nMake sure...\n", Stdout: "ignored", ExitCode: 9, Err: exit2}
	if got := failureDetail(r); got != "NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver." {
		t.Errorf("stderr should win over stdout and be cut to one line: %q", got)
	}

	r = util.CommandResult{ExitCode: 1, Err: exit2}
	if got := failureDetail(r); got != "exit status 2" {
		t.Errorf("Go error should be the last resort: %q", got)
	}

	r = util.CommandResult{ExitCode: 3}
	if got := failureDetail(r); got != "exit 3" {
		t.Errorf("no information at all should still produce a reason: %q", got)
	}
}

func TestConcernsTool(t *testing.T) {
	if !concernsTool(CheckResult{Name: "nvidia-smi", Status: "WARN"}) {
		t.Error("nvidia-smi is a tool check")
	}
	if !concernsTool(CheckResult{Name: "Python", Status: "WARN"}) {
		t.Error("Python is a tool check")
	}
	for _, name := range []string{"Elevation", "Architecture", "nvidia-smi thermal query", "Write Permissions"} {
		if concernsTool(CheckResult{Name: name, Status: "WARN"}) {
			t.Errorf("%q must not be reported as a missing tool", name)
		}
	}
}

func TestCheckElevationIsInfoEitherWay(t *testing.T) {
	// Whether or not the test process is elevated, the row is informational:
	// it must never flip the self-test exit code on a healthy machine.
	r := checkElevation()
	if r.Status != "INFO" {
		t.Errorf("Elevation status = %q, want INFO (detail %q)", r.Status, r.Detail)
	}
	if r.Name != "Elevation" || r.Detail == "" {
		t.Errorf("unexpected elevation row: %+v", r)
	}
}

func TestCheckWritePermissionsUsesUniqueProbe(t *testing.T) {
	dir := t.TempDir()
	// A user file with the legacy fixed probe name must never be touched.
	legacy := filepath.Join(dir, ".nvcheckup-selftest-write")
	if err := os.WriteFile(legacy, []byte("user data"), 0644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	r := checkWritePermissions()
	if r.Status != "OK" {
		t.Fatalf("expected OK in a writable temp dir, got %+v", r)
	}
	data, err := os.ReadFile(legacy)
	if err != nil || string(data) != "user data" {
		t.Errorf("legacy-named user file was modified or removed: %q, %v", data, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != ".nvcheckup-selftest-write" {
			t.Errorf("probe file %q was left behind", e.Name())
		}
	}
}

func TestCountGPUListLines(t *testing.T) {
	three := "GPU 0: NVIDIA GeForce RTX 3090 (UUID: GPU-a)\nGPU 1: NVIDIA GeForce RTX 4090 (UUID: GPU-b)\nGPU 2: NVIDIA GeForce RTX 4090 (UUID: GPU-c)\n"
	if got := countGPUListLines(three); got != 3 {
		t.Errorf("three GPUs counted as %d", got)
	}
	// MIG instances are listed under the GPU and must not be counted.
	mig := "GPU 0: NVIDIA H100 80GB HBM3 (UUID: GPU-d)\n  MIG 1g.10gb     Device  0: (UUID: MIG-e)\n  MIG 1g.10gb     Device  1: (UUID: MIG-f)\n"
	if got := countGPUListLines(mig); got != 1 {
		t.Errorf("MIG-enabled H100 counted as %d GPUs, want 1", got)
	}
	if got := countGPUListLines("No devices were found\n"); got != 0 {
		t.Errorf("failure text counted as %d GPUs", got)
	}
}

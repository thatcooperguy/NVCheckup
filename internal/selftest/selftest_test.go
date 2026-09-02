package selftest

import (
	"errors"
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

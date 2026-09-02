//go:build windows

package windows

import (
	"runtime"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// TestIsWow64Process2OnHost exercises the kernel32 call on the machine
// running the tests: every supported Windows (10 1709+) has the API, and the
// native machine must be one of the two labels the report knows.
func TestIsWow64Process2OnHost(t *testing.T) {
	pm, nm, err := isWow64Process2()
	if err != nil {
		t.Skipf("IsWow64Process2 unavailable on this host: %v", err)
	}
	native := machineName(nm)
	if native != "AMD64" && native != "ARM64" {
		t.Errorf("native machine = %q (%#x), want AMD64 or ARM64", native, nm)
	}
	// A process built for the host architecture reports IMAGE_FILE_MACHINE_UNKNOWN.
	if runtime.GOARCH == "amd64" && native == "AMD64" && pm != machineUnknown {
		t.Errorf("amd64 build on an x64 host must not be emulated, process machine %#x", pm)
	}
}

func TestCollectWoAOnHost(t *testing.T) {
	var p types.PlatformInfo
	errs := CollectWoA(10, &p)
	if p.NativeMachine == "" {
		t.Errorf("NativeMachine must be filled; errs=%+v", errs)
	}
	if runtime.GOARCH == "arm64" && !p.IsWindowsOnArm {
		t.Error("an arm64 build is by definition Windows on Arm")
	}
	if p.NativeMachine == "AMD64" && p.IsWindowsOnArm {
		t.Error("an x64 host must not be classed Windows on Arm")
	}
	// Never panics on a nil receiver.
	if got := CollectWoA(10, nil); len(got) != 0 {
		t.Errorf("nil PlatformInfo must be a no-op, got %+v", got)
	}
}

//go:build windows

package windows

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Windows on Arm detection (spec docs/roadmap/spark-support.md 3.1 row 1 and
// section 8): IsWow64Process2 through kernel32 first, the PROCESSOR_ARCHITECTURE
// / PROCESSOR_ARCHITEW6432 environment as fallback, then WMI
// (Win32_Processor.Architecture 12, Win32_ComputerSystemProduct). On an Arm
// host the RTX Spark adapter facts (gpu_woa.go) are collected too.

var (
	modkernel32           = syscall.NewLazyDLL("kernel32.dll")
	procIsWow64Process2   = modkernel32.NewProc("IsWow64Process2")
	procGetCurrentProcess = modkernel32.NewProc("GetCurrentProcess")
)

// isWow64Process2 calls kernel32!IsWow64Process2 for the current process. It
// returns an error when the API is missing (Windows before 10 1709) or the
// call fails.
func isWow64Process2() (processMachine, nativeMachine uint16, err error) {
	if err := procIsWow64Process2.Find(); err != nil {
		return 0, 0, err
	}
	if err := procGetCurrentProcess.Find(); err != nil {
		return 0, 0, err
	}
	handle, _, _ := procGetCurrentProcess.Call()
	var pm, nm uint16
	ret, _, callErr := procIsWow64Process2.Call(handle, uintptr(unsafe.Pointer(&pm)), uintptr(unsafe.Pointer(&nm)))
	if ret == 0 {
		if callErr == nil {
			callErr = syscall.EINVAL
		}
		return 0, 0, callErr
	}
	return pm, nm, nil
}

// CollectWoA fills IsWindowsOnArm, ProcessEmulated and NativeMachine, and on
// an Arm host the RTX Spark adapter (Class rtx-spark, GPUSoC N1X, WoA facts)
// and the nvcc.exe machine type. It is cheap on x64 hosts: one syscall and
// no WMI unless the syscall is unavailable.
func CollectWoA(timeout int, p *types.PlatformInfo) []types.CollectorError {
	var errs []types.CollectorError
	if p == nil {
		return errs
	}

	pm, nm, err := isWow64Process2()
	if err == nil {
		applyWow64(p, pm, nm)
	} else {
		applyArchEnv(p, os.Getenv("PROCESSOR_ARCHITECTURE"), os.Getenv("PROCESSOR_ARCHITEW6432"))
	}
	if runtime.GOARCH == "arm64" {
		// A native arm64 build is proof enough (spec 3.1 row 1).
		p.IsWindowsOnArm = true
		if p.NativeMachine == "" {
			p.NativeMachine = "ARM64"
		}
	}

	// WMI confirms the arm test when the syscall was unavailable and supplies
	// vendor/model on Arm hosts (Win32_ComputerSystemProduct.Name, spec 3.2).
	if err != nil || p.IsWindowsOnArm {
		r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", systemProductScript)
		if r.Err == nil {
			applySystemProduct(p, parseSystemProduct(r.Stdout))
		} else {
			errs = append(errs, types.CollectorError{Collector: "windows.woa", Error: "Win32_Processor/Win32_ComputerSystemProduct query failed: " + r.Err.Error()})
		}
	}

	if p.IsWindowsOnArm {
		collectRTXSparkAdapter(timeout, p, &errs)
		collectNvccMachine(p)
	}
	return errs
}

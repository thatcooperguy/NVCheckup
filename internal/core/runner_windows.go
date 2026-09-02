//go:build windows

package core

import (
	winCollector "github.com/thatcooperguy/nvcheckup/internal/collector/windows"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func collectPlatformSpecific(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	var allErrs []types.CollectorError

	if cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeStreaming ||
		cfg.Mode == types.ModeCreator || cfg.Mode == types.ModeFull {
		winInfo, winErrs := winCollector.CollectWindowsInfo(cfg.Timeout, cfg.IncludeLogs)
		r.Windows = &winInfo
		allErrs = append(allErrs, winErrs...)
	}

	// Collect display info for gaming, streaming, and full modes
	if cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeStreaming || cfg.Mode == types.ModeFull {
		displays, displayErrs := winCollector.CollectDisplayInfo(cfg.Timeout)
		r.Displays = displays
		allErrs = append(allErrs, displayErrs...)
	}

	return allErrs
}

// refinePlatformPhase1 runs windows.CollectWoA right after
// common.DetectPlatform (spec 3.1 rows 1-2, section 8): IsWow64Process2 is
// the authoritative source of IsWindowsOnArm / ProcessEmulated /
// NativeMachine, and on Arm hosts the Win32_VideoController rows supply the
// RTX Spark adapter facts (Class rtx-spark, GPUSoC N1X, Platform.WoA). The
// collector works on a copy so mergeWoAPlatform can apply the precedence
// rules explicitly. Cheap on x64 hosts: one syscall, no WMI.
func refinePlatformPhase1(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	if r == nil {
		return nil
	}
	woa := r.Platform
	errs := winCollector.CollectWoA(cfg.Timeout, &woa)
	r.Platform = mergeWoAPlatform(r.Platform, woa)
	return errs
}

// collectPlatformOSExtras: the DGX OS / ConnectX-7 / NVRM collectors are
// Linux-only (DGX Spark runs DGX OS); nothing to add on Windows.
func collectPlatformOSExtras(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	return nil
}

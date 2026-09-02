//go:build linux

package core

import (
	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	linuxCollector "github.com/thatcooperguy/nvcheckup/internal/collector/linux"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func collectPlatformSpecific(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	linInfo, linErrs := linuxCollector.CollectLinuxInfo(cfg.Timeout, cfg.IncludeLogs)
	r.Linux = &linInfo
	allErrs := linErrs

	// Collect display info
	if cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeFull {
		displays, displayErrs := linuxCollector.CollectDisplayInfo(cfg.Timeout)
		r.Displays = displays
		allErrs = append(allErrs, displayErrs...)
	}

	// Collect Xid errors from kernel logs
	if cfg.Mode == types.ModeAI || cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeFull {
		xidErrors, xidErrs := linuxCollector.CollectXidErrors(cfg.Timeout)
		if r.Linux != nil {
			r.Linux.XidErrors = xidErrors
		}
		allErrs = append(allErrs, xidErrs...)
	}

	// Detect llvmpipe software rendering fallback
	if cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeAI || cfg.Mode == types.ModeFull {
		fallback, glRenderer, rendererErrs := linuxCollector.DetectLlvmpipe(cfg.Timeout)
		if r.Linux != nil {
			r.Linux.LlvmpipeFallback = fallback
			r.Linux.GLRenderer = glRenderer
		}
		allErrs = append(allErrs, rendererErrs...)
	}

	return allErrs
}

// refinePlatformPhase1 has nothing to add on Linux: rows 1-2 of spec 3.1 are
// Windows-only and the Linux rows are evaluated by common.DetectPlatform.
func refinePlatformPhase1(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	return nil
}

// collectPlatformOSExtras runs the DGX Spark collectors of work package 1b
// on Class == dgx-spark in every mode (spec section 9 restricts none of them
// to a mode; all are read-only and bounded by cfg.Timeout):
//
//   - linux.CollectDGXOS merged over the release-file DGXOSInfo that
//     collectPlatformExtras stored (UnitsQueried, OTA, package and unit facts
//     come only from here; Report.DGXOS is set because a collector ran).
//   - linux.CollectDGXHostState fills PlatformInfo.Firmware, ClockCapUnit,
//     PrevBootClean/PrevBootLastLine/UncleanBoots, PstoreEmpty, ACPIThermalMC,
//     GDMSleepPolicy, SuspendAttempts/SuspendFailed.
//   - linux.CollectCluster -> Report.Cluster when it saw ConnectX-7
//     functions or the hotplug marker.
//   - linux.CollectNVRMMessages -> Linux.GSPFailureLines, UnifiedMemory
//     counts, Cluster.PeermemAttempted (applyNVRMMessages).
func collectPlatformOSExtras(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	var errs []types.CollectorError
	if r == nil || r.Platform.Class != common.ClassDGXSpark {
		return errs
	}

	dgx, dgxErrs := linuxCollector.CollectDGXOS(cfg.Timeout)
	errs = append(errs, dgxErrs...)
	r.DGXOS = mergeDGXOS(r.DGXOS, dgx)

	errs = append(errs, linuxCollector.CollectDGXHostState(cfg.Timeout, &r.Platform)...)

	cl, clErrs := linuxCollector.CollectCluster(cfg.Timeout)
	errs = append(errs, clErrs...)
	if clusterCollected(cl) {
		r.Cluster = &cl
	}

	nv, nvErrs := linuxCollector.CollectNVRMMessages(cfg.Timeout)
	errs = append(errs, nvErrs...)
	applyNVRMMessages(r, nv)

	return errs
}

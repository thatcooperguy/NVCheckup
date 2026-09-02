package core

import (
	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// collectPlatformExtras runs at the end of phase 4, after the OS-specific
// collectors and after common.ApplyPlatformFlags has settled r.Platform. It
// gathers the Spark / unified-memory data of docs/roadmap/spark-support.md
// section 4 that lives in the common package, and is the single hook where
// the work-package-1b collectors are to be wired by the integrator:
//
//   - linux.CollectDGXOS(timeout) on Class == dgx-spark: dpkg pairing,
//     nvidia-spark-ota-check, systemd units, fwupdmgr get-devices, journal
//     boot classification, pstore, acpitz, GDM sleep policy. It should merge
//     into the *types.DGXOSInfo that CollectDGXRelease already stored in
//     r.DGXOS (release-file fields) and fill r.Platform.Firmware,
//     ACPIThermalMC, PrevBootClean, PrevBootLastLine, PstoreEmpty,
//     ClockCapUnit, GDMSleepPolicy, SuspendAttempts, SuspendFailed.
//   - linux.CollectCluster(timeout) on dgx-spark (modes ai/full): ConnectX-7
//     fabric, r.Cluster.
//   - linux.CollectEcosystem / ai equivalents on dgx-spark and rtx-spark
//     (modes ai/creator/full): r.Ecosystem.
//   - windows.CollectWoA(timeout) on Windows: IsWow64Process2, dxdiag
//     dedicated/shared memory, nvcc.exe machine type; refines r.Platform.
//
// Those calls are platform-specific (build tags) and belong in
// runner_linux.go / runner_windows.go or a tagged sibling of this file.
// Everything here is read-only.
func collectPlatformExtras(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	var errs []types.CollectorError
	if r == nil {
		return errs
	}

	// Unified-memory picture (/proc/meminfo is the only truthful "VRAM" source
	// on GB10/N1X; spec 3.3). Gated on the flag rules, so it also runs for
	// Jetson and for the row-9 fallback, never on discrete-VRAM machines.
	if r.Platform.UnifiedMemory {
		um, umErrs := common.CollectUnifiedMemory(cfg.Timeout)
		r.UnifiedMemory = &um
		errs = append(errs, umErrs...)
	}

	// DGX OS release files (spec 3.1 row 4). linux.CollectDGXOS extends this.
	if r.Platform.Class == common.ClassDGXSpark {
		dgx, dgxErrs := common.CollectDGXRelease()
		if dgx != nil {
			r.DGXOS = dgx
		}
		errs = append(errs, dgxErrs...)
	}

	return errs
}

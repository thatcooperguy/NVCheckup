package core

import (
	"github.com/thatcooperguy/nvcheckup/internal/collector/ai"
	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	linuxCollector "github.com/thatcooperguy/nvcheckup/internal/collector/linux"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// collectPlatformExtras runs at the end of phase 4, after the OS-specific
// collectors and after common.ApplyPlatformFlags has settled r.Platform. It
// gathers the Spark / unified-memory data of docs/roadmap/spark-support.md
// section 4 that lives in the common package, then hands over to the
// build-tagged collectPlatformOSExtras (runner_linux.go: DGX OS, host state,
// ConnectX-7 fabric, NVRM kernel-log scan; runner_windows.go and
// runner_other.go: no-op). Everything here is read-only.
//
// gb10-pd-power-wedge (spec 5) grades one thermal sample as WARN and needs
// >= 2 matching samples per GPU for CRIT. The runner deliberately keeps one
// ThermalInfo per GPU: a second nvidia-smi read appended to GPUThermal would
// make analyzeThermal's multiGPU labelling treat the GB10 as a two-GPU rig
// and duplicate every thermal finding. The single-sample WARN is therefore
// the shipped behaviour and CRIT is left for hardware confirmation (spec
// section 12 open questions).
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

	// DGX OS release files (spec 3.1 row 4). linux.CollectDGXOS extends this
	// in collectPlatformOSExtras and is merged with mergeDGXOS.
	if r.Platform.Class == common.ClassDGXSpark {
		dgx, dgxErrs := common.CollectDGXRelease()
		if dgx != nil {
			r.DGXOS = dgx
		}
		errs = append(errs, dgxErrs...)
	}

	errs = append(errs, collectPlatformOSExtras(r, cfg)...)
	return errs
}

// collectEcosystemExtras runs in phase 5 (modes ai/creator/full) on dgx-spark
// and rtx-spark: the AI software-ecosystem facts of spec section 4
// (EcosystemInfo). ai.CollectEcosystem is portable; on Windows the /proc and
// /etc reads simply yield nothing. Torch facts are shared with the PyTorch
// probe of CollectAIInfo in both directions so neither consumer misses them.
func collectEcosystemExtras(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	var errs []types.CollectorError
	if r == nil {
		return errs
	}
	if r.Platform.Class != common.ClassDGXSpark && r.Platform.Class != common.ClassRTXSpark {
		return errs
	}
	eco, ecoErrs := ai.CollectEcosystem(cfg.Timeout)
	r.Ecosystem = &eco
	errs = append(errs, ecoErrs...)
	syncTorchFacts(r)
	return errs
}

// syncTorchFacts copies TorchArchList / TorchWarnings between
// Report.Ecosystem and Report.AI.PyTorchInfo, filling whichever side is
// empty. Nothing is overwritten and a nil PyTorchInfo is never allocated
// (a missing torch probe must stay visible as such).
func syncTorchFacts(r *types.Report) {
	if r == nil || r.Ecosystem == nil || r.AI == nil || r.AI.PyTorchInfo == nil {
		return
	}
	eco, pt := r.Ecosystem, r.AI.PyTorchInfo
	if len(eco.TorchArchList) == 0 && len(pt.ArchList) > 0 {
		eco.TorchArchList = append([]string(nil), pt.ArchList...)
	} else if len(pt.ArchList) == 0 && len(eco.TorchArchList) > 0 {
		pt.ArchList = append([]string(nil), eco.TorchArchList...)
	}
	if len(eco.TorchWarnings) == 0 && len(pt.Warnings) > 0 {
		eco.TorchWarnings = append([]string(nil), pt.Warnings...)
	} else if len(pt.Warnings) == 0 && len(eco.TorchWarnings) > 0 {
		pt.Warnings = append([]string(nil), eco.TorchWarnings...)
	}
}

// mergeDGXOS combines the release-file half of DGXOSInfo that
// common.CollectDGXRelease produced (base, may be nil) with the full
// linux.CollectDGXOS result (extra). Field by field the non-empty value wins,
// extra first because it is the more complete collector; the OTA, package,
// unit and UnitsQueried facts exist only on the extra side and are therefore
// never lost. The result is never nil: the caller only invokes this when a
// DGX OS collector actually ran, which is the contract that keeps
// Report.DGXOS nil (and rule dgx-spark-dashboard-unhealthy silent) when no
// collector ran (pkg/types DGXOSInfo.UnitsQueried comment).
func mergeDGXOS(base *types.DGXOSInfo, extra types.DGXOSInfo) *types.DGXOSInfo {
	out := extra
	if base == nil {
		return &out
	}
	pick := func(dst *string, alt string) {
		if *dst == "" {
			*dst = alt
		}
	}
	pick(&out.Name, base.Name)
	pick(&out.PrettyName, base.PrettyName)
	pick(&out.SWBuildVersion, base.SWBuildVersion)
	pick(&out.SWBuildDate, base.SWBuildDate)
	pick(&out.OTAVersion, base.OTAVersion)
	pick(&out.OTADate, base.OTADate)
	pick(&out.Platform, base.Platform)
	pick(&out.CommitID, base.CommitID)
	pick(&out.SerialNumber, base.SerialNumber)
	pick(&out.FastOSVersion, base.FastOSVersion)
	pick(&out.OTAName, base.OTAName)
	pick(&out.DriverPkgVersion, base.DriverPkgVersion)
	pick(&out.FirmwarePkgVersion, base.FirmwarePkgVersion)
	pick(&out.FwupdError, base.FwupdError)
	pick(&out.AptSourceCorrupt, base.AptSourceCorrupt)
	if out.OTATorn == nil && base.OTATorn != nil {
		v := *base.OTATorn
		out.OTATorn = &v
	}
	if len(out.OTAFailed) == 0 && len(base.OTAFailed) > 0 {
		out.OTAFailed = append([]string(nil), base.OTAFailed...)
	}
	out.ModulesForKernel = out.ModulesForKernel || base.ModulesForKernel
	out.DashboardActive = out.DashboardActive || base.DashboardActive
	out.DashboardAdminActive = out.DashboardAdminActive || base.DashboardAdminActive
	out.FwupdActive = out.FwupdActive || base.FwupdActive
	out.PersistencedActive = out.PersistencedActive || base.PersistencedActive
	out.DashboardPortOpen = out.DashboardPortOpen || base.DashboardPortOpen
	out.UnitsQueried = out.UnitsQueried || base.UnitsQueried
	return &out
}

// mergeWoAPlatform folds the result of windows.CollectWoA (woa, run on a copy
// of the phase-1 PlatformInfo) back into that PlatformInfo (base). Rules:
// the IsWow64Process2-derived IsWindowsOnArm / ProcessEmulated /
// NativeMachine win over the WMI / environment answers of DetectPlatform; a
// non-empty Class (and GPUSoC) is never overwritten by an empty one; Vendor
// and Model keep the first non-empty value; Platform.WoA is taken when
// CollectWoA filled it. Every other field is base's, because CollectWoA
// does not touch it.
func mergeWoAPlatform(base, woa types.PlatformInfo) types.PlatformInfo {
	out := base
	out.IsWindowsOnArm = woa.IsWindowsOnArm
	out.ProcessEmulated = woa.ProcessEmulated
	if woa.NativeMachine != "" {
		out.NativeMachine = woa.NativeMachine
	}
	if woa.Class != "" {
		out.Class = woa.Class
	}
	if woa.GPUSoC != "" {
		out.GPUSoC = woa.GPUSoC
	}
	if out.Vendor == "" {
		out.Vendor = woa.Vendor
	}
	if out.Model == "" {
		out.Model = woa.Model
	}
	if woa.WoA != nil {
		out.WoA = woa.WoA
	}
	return out
}

// applyNVRMMessages distributes one kernel-log scan (linux.CollectNVRMMessages)
// over the report: GSP/SEC2 failure lines into Linux.GSPFailureLines
// (allocating LinuxInfo when the Linux collector did not run), the NVRM
// out-of-memory and OOM-killer counts into UnifiedMemory (the higher of the
// two independent scans of the same log is kept, never the sum), and the
// nvidia_peermem load attempt into Cluster. Nil pointers other than Linux are
// left nil: a count without its section would be meaningless.
func applyNVRMMessages(r *types.Report, nv linuxCollector.NVRMMessages) {
	if r == nil {
		return
	}
	if len(nv.GSPFailureLines) > 0 {
		if r.Linux == nil {
			r.Linux = &types.LinuxInfo{}
		}
		r.Linux.GSPFailureLines = append([]string(nil), nv.GSPFailureLines...)
	}
	if r.UnifiedMemory != nil {
		if nv.NoMemoryCount > r.UnifiedMemory.NVRMNoMemory {
			r.UnifiedMemory.NVRMNoMemory = nv.NoMemoryCount
		}
		if nv.OOMKillCount > r.UnifiedMemory.OOMKills {
			r.UnifiedMemory.OOMKills = nv.OOMKillCount
		}
	}
	if r.Cluster != nil && nv.PeermemAttempted {
		r.Cluster.PeermemAttempted = true
	}
}

// clusterCollected reports whether a ClusterInfo carries anything worth
// publishing: enumerated ConnectX-7 functions or the hotplug marker file.
// Report.Cluster stays nil otherwise (types.Report comment: "only when
// ConnectX-7 functions are enumerated").
func clusterCollected(cl types.ClusterInfo) bool {
	return len(cl.Ports) > 0 || cl.HotplugFileEnabled
}

// Package types defines all shared data structures for NVCheckup.
package types

import "time"

// Version of NVCheckup. Overridden at build time via
// -ldflags "-X github.com/thatcooperguy/nvcheckup/pkg/types.Version=x.y.z".
var Version = "0.2.2"

// SchemaVersion identifies the report.json layout. Bump on breaking changes.
const SchemaVersion = "1"

// Disclaimer shown in all reports
const Disclaimer = "NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation."

// RunMode selects which collectors and analyzers to activate
type RunMode string

const (
	ModeGaming    RunMode = "gaming"
	ModeAI        RunMode = "ai"
	ModeCreator   RunMode = "creator"
	ModeStreaming RunMode = "streaming"
	ModeFull      RunMode = "full"
)

// Severity levels for findings
type Severity string

const (
	SeverityInfo Severity = "INFO"
	SeverityWarn Severity = "WARN"
	SeverityCrit Severity = "CRIT"
)

// ExitCode for CLI
const (
	ExitOK       = 0
	ExitWarnings = 1
	ExitCritical = 2
	ExitError    = 3
)

// RiskLevel for remediation actions
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// RunConfig holds all CLI flags and options for a run
type RunConfig struct {
	Mode        RunMode
	OutDir      string
	Zip         bool
	JSON        bool
	Markdown    bool
	Verbose     bool
	Timeout     int // seconds
	Redact      bool
	IncludeLogs bool // Linux only: include journalctl/dmesg snippets
	NetworkTest bool // run network diagnostics
}

// DefaultRunConfig returns a RunConfig with safe defaults
func DefaultRunConfig() RunConfig {
	return RunConfig{
		Mode:        ModeFull,
		OutDir:      ".",
		Zip:         false,
		JSON:        false,
		Markdown:    false,
		Verbose:     false,
		Timeout:     30,
		Redact:      true,
		IncludeLogs: false,
	}
}

// SystemInfo holds universal system snapshot data
type SystemInfo struct {
	OSName        string `json:"os_name"`
	OSVersion     string `json:"os_version"`
	OSBuild       string `json:"os_build,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"`
	Architecture  string `json:"architecture"`
	BootMode      string `json:"boot_mode,omitempty"`
	SecureBoot    string `json:"secure_boot,omitempty"`
	CPUModel      string `json:"cpu_model"`
	RAMTotalMB    int64  `json:"ram_total_mb"`
	StorageFreeMB int64  `json:"storage_free_mb,omitempty"`
	Uptime        string `json:"uptime"`
	Timezone      string `json:"timezone,omitempty"`
	Hostname      string `json:"hostname,omitempty"`       // will be redacted
	IsJetson      bool   `json:"is_jetson,omitempty"`      // NVIDIA Jetson / Tegra board (no nvidia-smi; GPU is not on PCIe)
	JetsonRelease string `json:"jetson_release,omitempty"` // first line of /etc/nv_tegra_release, e.g. "# R36 (release), REVISION: 4.3, ..."
}

// GPUInfo holds information about a single GPU
type GPUInfo struct {
	Index         int    `json:"index"`
	Name          string `json:"name"`
	Vendor        string `json:"vendor"` // "NVIDIA", "Intel", "AMD"
	PCIVendorID   string `json:"pci_vendor_id,omitempty"`
	PCIDeviceID   string `json:"pci_device_id,omitempty"`
	PCIBusID      string `json:"pci_bus_id,omitempty"`
	DriverVersion string `json:"driver_version,omitempty"`
	WDDMVersion   string `json:"wddm_version,omitempty"`
	VRAMTotalMB   int64  `json:"vram_total_mb,omitempty"`
	VRAMFreeMB    int64  `json:"vram_free_mb,omitempty"`
	VRAMUsedMB    int64  `json:"vram_used_mb,omitempty"`
	Temperature   int    `json:"temperature_c,omitempty"`
	PowerDraw     string `json:"power_draw,omitempty"`
	IsNVIDIA      bool   `json:"is_nvidia"`
	PCIeLinkSpeed string `json:"pcie_link_speed,omitempty"` // "Gen4"
	PCIeLinkWidth string `json:"pcie_link_width,omitempty"` // "x16"

	// Spark / unified-memory additions (docs/roadmap/spark-support.md section 4).
	ComputeCap      string `json:"compute_cap,omitempty"`      // CUDA compute capability from nvidia-smi --query-gpu=compute_cap, e.g. "12.1"
	OnPackage       bool   `json:"on_package,omitempty"`       // GPU is on the SoC package (GB10/N1X): no user-serviceable PCIe slot, PCIe rules are suppressed
	MemoryReporting string `json:"memory_reporting,omitempty"` // "dedicated" | "not-supported" (nvidia-smi memory.total is [N/A] on unified-memory parts)
}

// DriverInfo holds NVIDIA driver details
type DriverInfo struct {
	Version         string `json:"version"`
	CUDAVersion     string `json:"cuda_version,omitempty"` // CUDA runtime from driver
	NvidiaSmiPath   string `json:"nvidia_smi_path,omitempty"`
	NvidiaSmiOutput string `json:"nvidia_smi_output,omitempty"`
	Source          string `json:"source,omitempty"` // "package", "runfile", "wmi", etc.
}

// WindowsInfo holds Windows-specific collected data
type WindowsInfo struct {
	HAGSEnabled       string          `json:"hags_enabled,omitempty"`
	GameMode          string          `json:"game_mode,omitempty"`
	PowerPlan         string          `json:"power_plan,omitempty"`
	Monitors          []MonitorInfo   `json:"monitors,omitempty"`
	DriverResetEvents []EventLogEntry `json:"driver_reset_events,omitempty"`
	NvlddmkmErrors    []EventLogEntry `json:"nvlddmkm_errors,omitempty"`
	WHEAErrors        []EventLogEntry `json:"whea_errors,omitempty"`
	RecentKBs         []WindowsUpdate `json:"recent_kbs,omitempty"`
	NVIDIAAppVersion  string          `json:"nvidia_app_version,omitempty"`
	GFEVersion        string          `json:"gfe_version,omitempty"`
	OverlaySoftware   []string        `json:"overlay_software,omitempty"`
	DxDiagSummary     string          `json:"dxdiag_summary,omitempty"`
}

// MonitorInfo holds display/monitor data
type MonitorInfo struct {
	Name        string `json:"name"`
	Resolution  string `json:"resolution"`
	RefreshRate string `json:"refresh_rate"`
	Primary     bool   `json:"primary"`
}

// EventLogEntry holds a Windows event log entry
type EventLogEntry struct {
	EventID int       `json:"event_id"`
	Source  string    `json:"source"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

// WindowsUpdate holds info about a KB
type WindowsUpdate struct {
	KBID        string    `json:"kb_id"`
	Title       string    `json:"title"`
	InstalledOn time.Time `json:"installed_on"`
}

// LinuxInfo holds Linux-specific collected data
type LinuxInfo struct {
	Distro             string          `json:"distro"`
	DistroVersion      string          `json:"distro_version"`
	PackageManager     string          `json:"package_manager,omitempty"`
	NVIDIAPackages     []string        `json:"nvidia_packages,omitempty"`
	LoadedModules      map[string]bool `json:"loaded_modules,omitempty"` // nvidia, nvidia_drm, nouveau
	DKMSStatus         string          `json:"dkms_status,omitempty"`
	DKMSErrors         string          `json:"dkms_errors,omitempty"` // opt-in only
	SecureBootState    string          `json:"secure_boot_state,omitempty"`
	MOKStatus          string          `json:"mok_status,omitempty"`
	SessionType        string          `json:"session_type,omitempty"` // x11, wayland
	PRIMEStatus        string          `json:"prime_status,omitempty"`
	DevNvidiaNodes     []string        `json:"dev_nvidia_nodes,omitempty"`
	LibCudaPath        string          `json:"libcuda_path,omitempty"`
	ContainerRuntime   string          `json:"container_runtime,omitempty"`
	NVContainerToolkit string          `json:"nv_container_toolkit,omitempty"`
	JournalSnippets    string          `json:"journal_snippets,omitempty"` // opt-in
	DmesgSnippets      string          `json:"dmesg_snippets,omitempty"`   // opt-in
	XidErrors          []XidError      `json:"xid_errors,omitempty"`
	LlvmpipeFallback   bool            `json:"llvmpipe_fallback"`
	GLRenderer         string          `json:"gl_renderer,omitempty"`

	// GSP/SEC2 boot-failure lines from the kernel log (spec 3.2 "GSP failure"),
	// collected on GB10 by linux.CollectNVRMMessages independently of
	// --include-logs so dgx-spark-gsp-init-failure can fire without it.
	GSPFailureLines []string `json:"gsp_failure_lines,omitempty"`
}

// AIInfo holds AI/CUDA framework info
type AIInfo struct {
	CUDADriverVersion  string        `json:"cuda_driver_version,omitempty"`
	CUDAToolkitVersion string        `json:"cuda_toolkit_version,omitempty"`
	NvccPath           string        `json:"nvcc_path,omitempty"`
	CuDNNVersion       string        `json:"cudnn_version,omitempty"`
	PythonVersions     []PythonEnv   `json:"python_versions,omitempty"`
	CondaPresent       bool          `json:"conda_present"`
	PyTorchInfo        *PyTorchInfo  `json:"pytorch_info,omitempty"`
	TensorFlowInfo     *TFInfo       `json:"tensorflow_info,omitempty"`
	KeyPackages        []PackageInfo `json:"key_packages,omitempty"`
}

// PythonEnv holds python environment info
type PythonEnv struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// PyTorchInfo holds PyTorch probe results
type PyTorchInfo struct {
	Version       string `json:"version"`
	CUDAVersion   string `json:"cuda_version,omitempty"`
	CUDAAvailable bool   `json:"cuda_available"`
	DeviceName    string `json:"device_name,omitempty"`
	Error         string `json:"error,omitempty"`

	// Spark / unified-memory additions (docs/roadmap/spark-support.md section 4).
	Warnings []string `json:"warnings,omitempty"`  // stderr warnings emitted by the torch probe (e.g. unsupported capability 12.1)
	ArchList []string `json:"arch_list,omitempty"` // torch.cuda.get_arch_list(), e.g. ["sm_80", ..., "sm_120"]
}

// TFInfo holds TensorFlow probe results
type TFInfo struct {
	Version string   `json:"version"`
	GPUs    []string `json:"gpus,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// PackageInfo holds pip package info
type PackageInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// WSLInfo holds WSL-specific info
type WSLInfo struct {
	IsWSL         bool   `json:"is_wsl"`
	WSLVersion    string `json:"wsl_version,omitempty"`
	Distro        string `json:"distro,omitempty"`
	KernelVersion string `json:"kernel_version,omitempty"`
	DevDxgExists  bool   `json:"dev_dxg_exists,omitempty"`
	NvidiaSmiOK   bool   `json:"nvidia_smi_ok,omitempty"`
}

// Finding represents an actionable diagnostic finding
type Finding struct {
	ID           string             `json:"id,omitempty"` // stable rule id, e.g. "pcie-downshift"
	Severity     Severity           `json:"severity"`
	Title        string             `json:"title"`
	Evidence     string             `json:"evidence"`
	WhyItMatters string             `json:"why_it_matters"`
	NextSteps    []string           `json:"next_steps"`
	References   []string           `json:"references,omitempty"`
	Category     string             `json:"category,omitempty"` // "driver", "cuda", "overlay", etc.
	Confidence   int                `json:"confidence"`         // 0-100 confidence score
	Remediation  *RemediationAction `json:"remediation,omitempty"`
	GPUIndexes   []int              `json:"gpu_indexes,omitempty"` // nvidia-smi indices of the GPU(s) this finding is about (thermal/PCIe findings)

	// Spark / unified-memory additions (docs/roadmap/spark-support.md section 4).
	Impact string `json:"impact,omitempty"` // most invasive next step of the rule: none | reversible | persistent | irreversible | data-loss
}

// RemediationAction describes a safe, reversible fix for a finding
type RemediationAction struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Risk        RiskLevel `json:"risk"`
	Description string    `json:"description"`
	DryRunDesc  string    `json:"dry_run_desc"`
	UndoDesc    string    `json:"undo_desc"`
	Platform    string    `json:"platform"` // "windows", "linux", "all"
	NeedsReboot bool      `json:"needs_reboot"`
	NeedsAdmin  bool      `json:"needs_admin"`
	Category    string    `json:"category,omitempty"`     // "power", "registry", "driver"
	RelatedFind string    `json:"related_find,omitempty"` // human description of related finding
}

// RemediationResult holds the outcome of applying a remediation
type RemediationResult struct {
	ActionID  string    `json:"action_id"`
	Success   bool      `json:"success"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	UndoInfo  string    `json:"undo_info,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	DryRun    bool      `json:"dry_run"`
}

// ChangeJournalEntry records an applied change for undo tracking
type ChangeJournalEntry struct {
	ActionID    string    `json:"action_id"`
	Title       string    `json:"title"`
	AppliedAt   time.Time `json:"applied_at"`
	Success     bool      `json:"success"`
	Output      string    `json:"output,omitempty"`
	UndoInfo    string    `json:"undo_info,omitempty"`
	UndoneAt    time.Time `json:"undone_at,omitempty"`
	UndoSuccess bool      `json:"undo_success,omitempty"`
	UndoOutput  string    `json:"undo_output,omitempty"`
}

// ThermalInfo holds GPU thermal and power state data
type ThermalInfo struct {
	TemperatureC    int      `json:"temperature_c"`
	ThermalThrottle bool     `json:"thermal_throttle"`
	PowerState      string   `json:"power_state"` // P0-P15
	CurrentClockMHz int      `json:"current_clock_mhz"`
	MaxClockMHz     int      `json:"max_clock_mhz"`
	PowerLimitW     string   `json:"power_limit_w"`
	PowerDrawW      string   `json:"power_draw_w"`
	FanSpeedPct     int      `json:"fan_speed_pct"`
	FanSupported    bool     `json:"fan_supported"`              // false when nvidia-smi reports [N/A] (passive/water-cooled)
	SlowdownActive  bool     `json:"slowdown_active"`            // true only for thermal/power/HW slowdown bits, never idle
	SlowdownReason  string   `json:"slowdown_reason,omitempty"`  // raw clocks_event_reasons.active bitmask
	ThrottleReasons []string `json:"throttle_reasons,omitempty"` // decoded active reasons, e.g. "sw_thermal_slowdown"
	UtilizationPct  int      `json:"utilization_pct"`            // utilization.gpu at sample time
	GPUIndex        int      `json:"gpu_index"`                  // nvidia-smi index of the GPU this row describes

	// Spark / unified-memory additions (docs/roadmap/spark-support.md section 4).
	PowerLimitSupported bool             `json:"power_limit_supported"`       // false when nvidia-smi reports power.limit as [N/A] (GB10)
	EventCounters       map[string]int64 `json:"event_counters_us,omitempty"` // nvidia-smi -q -d PERFORMANCE "Clocks Event Reasons Counters", microseconds per reason
}

// PCIeInfo holds PCIe link state data
type PCIeInfo struct {
	CurrentSpeed   string `json:"current_speed"`         // "Gen4"
	MaxSpeed       string `json:"max_speed"`             // "Gen4"
	CurrentWidth   string `json:"current_width"`         // "x16"
	MaxWidth       string `json:"max_width"`             // "x16"
	Downshifted    bool   `json:"downshifted"`           // gen or width below max while the GPU is NOT idle
	PowerState     string `json:"power_state,omitempty"` // pstate at sample time, e.g. "P8"
	UtilizationPct int    `json:"utilization_pct"`       // utilization.gpu at sample time
	IdleLikely     bool   `json:"idle_likely"`           // P5+ or low utilization: link power-saving is expected
	GPUIndex       int    `json:"gpu_index"`             // nvidia-smi index of the GPU this row describes

	// Spark / unified-memory additions (docs/roadmap/spark-support.md section 4).
	OnPackage bool `json:"on_package,omitempty"` // GPU is on the SoC package: the reported link is not a user-serviceable slot, all PCIe rules are suppressed
}

// DisplayInfo holds display/monitor pipeline data
type DisplayInfo struct {
	Name       string `json:"name"`
	Resolution string `json:"resolution"`
	RefreshHz  int    `json:"refresh_hz"`
	HDREnabled bool   `json:"hdr_enabled"`
	HDRCapable bool   `json:"hdr_capable"`
	VRREnabled bool   `json:"vrr_enabled"` // G-Sync / FreeSync
	ColorDepth string `json:"color_depth"` // "8-bit", "10-bit"
	OutputType string `json:"output_type"` // "HDMI", "DP", "USB-C"
	GPUIndex   int    `json:"gpu_index"`   // adapter ordinal (Windows: Win32_VideoController order) driving this display; may differ from gpus[] index on iGPU+dGPU systems
	Primary    bool   `json:"primary"`
	ScalingPct int    `json:"scaling_pct"`
}

// NetworkInfo holds network diagnostic results
type NetworkInfo struct {
	InterfaceName string    `json:"interface_name"`
	InterfaceType string    `json:"interface_type"` // "ethernet", "wifi"
	WifiBand      string    `json:"wifi_band,omitempty"`
	WifiSignalDBM int       `json:"wifi_signal_dbm,omitempty"`
	LatencyMs     float64   `json:"latency_ms"`
	JitterMs      float64   `json:"jitter_ms"`
	PacketLossPct float64   `json:"packet_loss_pct"`
	DNSTimeMs     float64   `json:"dns_time_ms"`
	Hops          []HopInfo `json:"hops,omitempty"`
}

// HopInfo holds a single traceroute hop
type HopInfo struct {
	Number    int     `json:"number"`
	Address   string  `json:"address"` // redacted
	LatencyMs float64 `json:"latency_ms"`
	Loss      bool    `json:"loss"`
}

// XidError holds a parsed NVIDIA Xid error from kernel logs
type XidError struct {
	Code      int       `json:"code"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Count     int       `json:"count"`
}

// CollectorError records a non-fatal error from a collector
type CollectorError struct {
	Collector string `json:"collector"`
	Error     string `json:"error"`
	Fatal     bool   `json:"fatal"`
}

// Report is the complete collected + analyzed result
type Report struct {
	Metadata        ReportMetadata   `json:"metadata"`
	System          SystemInfo       `json:"system"`
	GPUs            []GPUInfo        `json:"gpus"`
	Driver          DriverInfo       `json:"driver"`
	Windows         *WindowsInfo     `json:"windows,omitempty"`
	Linux           *LinuxInfo       `json:"linux,omitempty"`
	WSL             *WSLInfo         `json:"wsl,omitempty"`
	AI              *AIInfo          `json:"ai,omitempty"`
	Thermal         *ThermalInfo     `json:"thermal,omitempty"`
	PCIe            *PCIeInfo        `json:"pcie,omitempty"`
	GPUThermal      []ThermalInfo    `json:"gpu_thermal,omitempty"` // one entry per NVIDIA GPU; Thermal points at entry 0
	GPUPCIe         []PCIeInfo       `json:"gpu_pcie,omitempty"`    // one entry per NVIDIA GPU; PCIe points at entry 0
	Displays        []DisplayInfo    `json:"displays,omitempty"`
	Network         *NetworkInfo     `json:"network,omitempty"`
	Findings        []Finding        `json:"findings"`
	CollectorErrors []CollectorError `json:"collector_errors,omitempty"`
	TopIssues       []string         `json:"top_issues"`
	NextSteps       []string         `json:"next_steps"`
	SummaryBlock    string           `json:"summary_block"`

	// Spark / unified-memory additions (docs/roadmap/spark-support.md section 4).
	Platform      PlatformInfo       `json:"platform"`                 // always present; Class is "" when no detection row matched
	UnifiedMemory *UnifiedMemoryInfo `json:"unified_memory,omitempty"` // only when Platform.UnifiedMemory
	DGXOS         *DGXOSInfo         `json:"dgx_os,omitempty"`         // only on dgx-spark
	Cluster       *ClusterInfo       `json:"cluster,omitempty"`        // only when ConnectX-7 functions are enumerated
	Ecosystem     *EcosystemInfo     `json:"ecosystem,omitempty"`      // only on dgx-spark / rtx-spark
}

// ReportMetadata holds info about the report itself
type ReportMetadata struct {
	ToolVersion      string    `json:"tool_version"`
	Timestamp        time.Time `json:"timestamp"`
	Mode             RunMode   `json:"mode"`
	RuntimeSeconds   float64   `json:"runtime_seconds"`
	RedactionEnabled bool      `json:"redaction_enabled"`
	Platform         string    `json:"platform"` // "windows", "linux", "wsl"
	SchemaVersion    string    `json:"schema_version"`
	NetworkProbes    bool      `json:"network_probes"` // true when ping/DNS/traceroute probes were run (--network)
}

// Snapshot is a timestamped JSON snapshot for comparison
type Snapshot struct {
	Metadata ReportMetadata `json:"metadata"`
	System   SystemInfo     `json:"system"`
	GPUs     []GPUInfo      `json:"gpus"`
	Driver   DriverInfo     `json:"driver"`
	Windows  *WindowsInfo   `json:"windows,omitempty"`
	Linux    *LinuxInfo     `json:"linux,omitempty"`
	AI       *AIInfo        `json:"ai,omitempty"`
}

// ComparisonResult holds diffs between two snapshots
type ComparisonResult struct {
	SnapshotA   string       `json:"snapshot_a"`
	SnapshotB   string       `json:"snapshot_b"`
	TimestampA  time.Time    `json:"timestamp_a"`
	TimestampB  time.Time    `json:"timestamp_b"`
	Differences []Difference `json:"differences"`
}

// Difference represents a single difference between snapshots
type Difference struct {
	Field    string `json:"field"`
	ValueA   string `json:"value_a"`
	ValueB   string `json:"value_b"`
	Severity string `json:"severity,omitempty"` // how important is this change
}

// ---------------------------------------------------------------------------
// DGX Spark / RTX Spark / unified-memory platform types
// (docs/roadmap/spark-support.md section 4; additive, SchemaVersion stays "1").
// ---------------------------------------------------------------------------

// PlatformInfo classifies the machine (DGX Spark, RTX Spark, Jetson, Grace Hopper,
// arm64 with a discrete GPU) and carries the platform-level facts the Spark rules
// need. It is always present in a Report; Class is "" when no row of the detection
// table (spec section 3.1) matched.
type PlatformInfo struct {
	Class          string `json:"class"`                     // dgx-spark | rtx-spark | jetson | grace-hopper | arm64-dgpu | ""
	Vendor         string `json:"vendor,omitempty"`          // DMI sys_vendor / Win32_ComputerSystem.Manufacturer
	Model          string `json:"model,omitempty"`           // DMI product_name / device-tree model / Win32_ComputerSystemProduct.Name
	ProductVersion string `json:"product_version,omitempty"` // "A.7" on GB10
	BIOSVersion    string `json:"bios_version,omitempty"`
	BIOSDate       string `json:"bios_date,omitempty"`
	GPUSoC         string `json:"gpu_soc,omitempty"`     // GB10 | N1X | GH200
	ComputeCap     string `json:"compute_cap,omitempty"` // "12.1"
	UnifiedMemory  bool   `json:"unified_memory"`        // CPU and GPU share one physical memory pool (nvidia-smi memory.total is [N/A])

	// Sub-structures collected in phase 4 on dgx-spark / rtx-spark only.
	// The same data is also exposed at the top level of Report (Report.DGXOS,
	// Report.UnifiedMemory, Report.Cluster, Report.Ecosystem), which is where
	// spec section 10 asserts on it (e.g. unified_memory.mem_total_kb).
	DGXOS      *DGXOSInfo          `json:"dgx_os,omitempty"`
	UnifiedMem *UnifiedMemoryInfo  `json:"unified_memory_info,omitempty"`
	Firmware   []FirmwareComponent `json:"firmware,omitempty"` // fwupdmgr get-devices
	Cluster    *ClusterInfo        `json:"cluster,omitempty"`
	Ecosystem  *EcosystemInfo      `json:"ecosystem,omitempty"`

	// Windows on Arm (spec section 8).
	IsWindowsOnArm  bool   `json:"is_windows_on_arm,omitempty"`
	ProcessEmulated bool   `json:"process_emulated,omitempty"` // NVCheckup itself is running under Prism (x64 emulation)
	NativeMachine   string `json:"native_machine,omitempty"`   // ARM64 | AMD64

	// Kernel, thermal, boot and power facts (Linux).
	NvidiaKernelFlavour bool           `json:"nvidia_kernel_flavour,omitempty"` // Canonical linux-nvidia kernel (also on GH200/x86 DGX); consumed only by dgx-spark-non-nvidia-kernel
	ACPIThermalMC       map[string]int `json:"acpi_thermal_mc,omitempty"`       // thermal_zoneN -> millidegrees C
	PrevBootClean       *bool          `json:"prev_boot_clean,omitempty"`       // nil when the previous boot's journal is unreadable
	PrevBootLastLine    string         `json:"prev_boot_last_line,omitempty"`
	PstoreEmpty         *bool          `json:"pstore_empty,omitempty"`
	ClockCapUnit        string         `json:"clock_cap_unit,omitempty"` // "gb10-clock-cap.service"
	GDMSleepPolicy      string         `json:"gdm_sleep_policy,omitempty"`
	SuspendAttempts     int            `json:"suspend_attempts,omitempty"`
	SuspendFailed       bool           `json:"suspend_failed,omitempty"`
	UncleanBoots        int            `json:"unclean_boots,omitempty"` // boots in the journal window that ended without a clean-shutdown marker (gb10-logless-hard-poweroff)

	// RTX Spark adapter facts on Windows on Arm (spec 3.1 row 2, 3.2, section 8).
	WoA *WoAInfo `json:"woa,omitempty"`
}

// WoAInfo holds the Windows-on-Arm adapter and toolkit facts that spec
// section 8 assigns to the Windows collectors (windows.CollectWoA).
type WoAInfo struct {
	AdapterName      string `json:"adapter_name,omitempty"`   // Win32_VideoController.Name, e.g. "NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)"
	PNPDeviceID      string `json:"pnp_device_id,omitempty"`  // PCI\VEN_10DE&DEV_2E03&SUBSYS_...
	DriverVersion    string `json:"driver_version,omitempty"` // WDDM DriverVersion, expected to end in 16.1600 for 616.00
	InfFilename      string `json:"inf_filename,omitempty"`   // e.g. nv_surface_woa.inf
	DeveloperPreview bool   `json:"developer_preview"`        // DriverVersion ends 16.1600 or INF nv_surface_woa.inf
	NvccMachine      string `json:"nvcc_machine,omitempty"`   // PE machine type of nvcc.exe: ARM64 | AMD64 | I386 | 0x....
	NvccPath         string `json:"nvcc_path,omitempty"`
}

// DGXOSInfo holds DGX OS release, OTA, package and service state read on
// dgx-spark (/etc/dgx-release, /etc/fastos-release, dpkg, nvidia-spark-ota-check,
// systemd). Read-only; SerialNumber is redacted to <serial>.
type DGXOSInfo struct {
	Name           string `json:"name,omitempty"`
	PrettyName     string `json:"pretty_name,omitempty"`
	SWBuildVersion string `json:"sw_build_version,omitempty"`
	SWBuildDate    string `json:"sw_build_date,omitempty"`
	OTAVersion     string `json:"ota_version,omitempty"`
	OTADate        string `json:"ota_date,omitempty"`
	Platform       string `json:"platform,omitempty"`
	CommitID       string `json:"commit_id,omitempty"`
	SerialNumber   string `json:"serial_number,omitempty"`   // redacted to <serial>
	FastOSVersion  string `json:"fast_os_version,omitempty"` // /etc/fastos-release
	OTAName        string `json:"ota_name,omitempty"`        // e.g. "OTA2607"

	OTATorn   *int     `json:"ota_torn,omitempty"`   // nvidia-spark-ota-check torn score; nil when the tool is absent or timed out
	OTAFailed []string `json:"ota_failed,omitempty"` // components the OTA check reports as failed

	DriverPkgVersion   string `json:"driver_pkg_version,omitempty"`   // nvidia driver dpkg version
	FirmwarePkgVersion string `json:"firmware_pkg_version,omitempty"` // nvidia firmware dpkg version
	ModulesForKernel   bool   `json:"modules_for_kernel"`             // an nvidia modules package matches the running kernel

	DashboardActive      bool `json:"dashboard_active"`       // dgx-dashboard.service
	DashboardAdminActive bool `json:"dashboard_admin_active"` // dgx-dashboard-admin.service
	FwupdActive          bool `json:"fwupd_active"`           // fwupd.service
	PersistencedActive   bool `json:"persistenced_active"`    // nvidia-persistenced.service
	DashboardPortOpen    bool `json:"dashboard_port_open"`    // 127.0.0.1:11000 accepts connections

	FwupdError       string `json:"fwupd_error,omitempty"`        // last fwupd error line, if any
	AptSourceCorrupt string `json:"apt_source_corrupt,omitempty"` // apt source that fails to parse (e.g. nvidia-container-toolkit.list)
}

// UnifiedMemoryInfo is the system-memory picture on unified-memory platforms
// (GB10/N1X), where /proc/meminfo is the only truthful "VRAM" source.
// Counts only; no process names are recorded.
type UnifiedMemoryInfo struct {
	MemTotalKB     int64 `json:"mem_total_kb"`
	MemFreeKB      int64 `json:"mem_free_kb"`
	MemAvailableKB int64 `json:"mem_available_kb"`
	BuffersKB      int64 `json:"buffers_kb"`
	CachedKB       int64 `json:"cached_kb"`
	SwapTotalKB    int64 `json:"swap_total_kb"`
	SwapFreeKB     int64 `json:"swap_free_kb"`
	HugePagesTotal int64 `json:"huge_pages_total"`
	HugePagesFree  int64 `json:"huge_pages_free"`
	HugepagesizeKB int64 `json:"hugepagesize_kb"`
	AllocatableKB  int64 `json:"allocatable_kb"` // memory a single CUDA allocation can realistically obtain (spec section 3.3)

	SwapDevices  []string `json:"swap_devices,omitempty"` // /proc/swaps device names (zram, files, partitions)
	Swappiness   int      `json:"swappiness"`             // vm.swappiness
	PSISomeAvg10 float64  `json:"psi_some_avg10"`         // /proc/pressure/memory "some avg10"
	PSIFullAvg10 float64  `json:"psi_full_avg10"`         // /proc/pressure/memory "full avg10"

	GPUProcesses int `json:"gpu_processes"`  // processes holding a GPU context (count only)
	OOMKills     int `json:"oom_kills"`      // kernel OOM-killer events seen in logs
	NVRMNoMemory int `json:"nvrm_no_memory"` // NVRM out-of-memory class kernel messages
}

// FirmwareComponent is one device row from fwupdmgr get-devices.
type FirmwareComponent struct {
	Name    string `json:"name"`
	GUID    string `json:"guid,omitempty"`
	Version string `json:"version,omitempty"` // current version, dotted or hex form as printed
	Pending string `json:"pending,omitempty"` // update version staged but not yet applied
}

// FabricPort describes one ConnectX-7 port (RDMA device plus netdev) on DGX Spark.
type FabricPort struct {
	RDMADev    string   `json:"rdma_dev,omitempty"`   // /sys/class/infiniband device, e.g. "rocep1s0f0"
	Netdev     string   `json:"netdev,omitempty"`     // e.g. "enp1s0f0np0"
	PCIAddr    string   `json:"pci_addr,omitempty"`   // full BDF including domain
	Cage       int      `json:"cage"`                 // physical QSFP cage grouping the functions
	State      string   `json:"state,omitempty"`      // ports/1/state, e.g. "4: ACTIVE"
	PhysState  string   `json:"phys_state,omitempty"` // ports/1/phys_state, e.g. "5: LinkUp"
	SpeedMbps  int      `json:"speed_mbps"`           // /sys/class/net/<if>/speed
	MTU        int      `json:"mtu"`
	IPv4       []string `json:"ipv4,omitempty"` // redacted
	Bond       string   `json:"bond,omitempty"` // bond master, if enslaved
	Persistent bool     `json:"persistent"`     // configuration is persisted (netplan), not ad hoc
}

// ClusterInfo holds ConnectX-7 fabric and NCCL state for multi-Spark clustering
// (spec section 9). Read-only: no active network probes are made.
type ClusterInfo struct {
	Ports              []FabricPort      `json:"ports,omitempty"`
	HotplugFileEnabled bool              `json:"hotplug_file_enabled"` // /etc/nvidia/cx7-hotplug-enabled present
	NetplanMTU         int               `json:"netplan_mtu"`          // MTU configured in netplan for the fabric ports (0 = unset)
	NCCLEnv            map[string]string `json:"nccl_env,omitempty"`   // NCCL_* / UCX_* variables of this process
	NCCLPluginLib      string            `json:"nccl_plugin_lib,omitempty"`
	NCCLVersion        string            `json:"nccl_version,omitempty"`
	PeermemAttempted   bool              `json:"peermem_attempted"` // nvidia-peermem load attempted
	AvahiActive        bool              `json:"avahi_active"`
	AvahiConflicts     int               `json:"avahi_conflicts"`      // hostname conflicts (spark-xxxx renames) seen in the journal
	UfwEnabled         bool              `json:"ufw_enabled"`          // /etc/ufw/ufw.conf ENABLED=yes
	RDMATools          []string          `json:"rdma_tools,omitempty"` // rdma-core tools found on PATH (ibstat, ibdev2netdev, ...)
}

// EcosystemInfo captures the AI software-ecosystem facts that matter on
// sm_121 / arm64 (spec section 5 ecosystem rules).
type EcosystemInfo struct {
	TorchArchList      []string         `json:"torch_arch_list,omitempty"` // torch.cuda.get_arch_list()
	TorchWarnings      []string         `json:"torch_warnings,omitempty"`  // stderr of the torch probe
	TritonPtxasVersion string           `json:"triton_ptxas_version,omitempty"`
	TritonPtxasPath    string           `json:"triton_ptxas_path,omitempty"`  // TRITON_PTXAS_PATH
	LibcudartVersions  []string         `json:"libcudart_versions,omitempty"` // libcudart.so.12 / .13 found
	FlashAttnVersion   string           `json:"flash_attn_version,omitempty"`
	ORTVersion         string           `json:"ort_version,omitempty"` // onnxruntime distribution version
	ORTProviders       []string         `json:"ort_providers,omitempty"`
	ORTGPUShadowed     bool             `json:"ort_gpu_shadowed"` // CPU-only onnxruntime installed alongside onnxruntime-gpu
	Images             []ContainerImage `json:"images,omitempty"`
	DockerRuntimes     []string         `json:"docker_runtimes,omitempty"` // daemon.json runtimes
	DockerCDI          bool             `json:"docker_cdi"`                // daemon.json features.cdi
	CDISpecPresent     bool             `json:"cdi_spec_present"`          // /etc/cdi/nvidia.yaml
	SnapDocker         bool             `json:"snap_docker"`               // docker installed from snap
	ListeningPorts     []int            `json:"listening_ports,omitempty"` // inference ports open (8000, 30000, 11434, ...); no process names
}

// ContainerImage is a local container image reference and its architecture.
type ContainerImage struct {
	Ref  string `json:"ref"`
	Arch string `json:"arch,omitempty"` // e.g. "arm64", "amd64"
}

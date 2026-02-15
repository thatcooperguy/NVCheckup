// Package types defines all shared data structures for NVCheckup.
package types

import "time"

// Version of NVCheckup
const Version = "0.1.0"

// Disclaimer shown in all reports
const Disclaimer = "NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation."

// RunMode selects which collectors and analyzers to activate
type RunMode string

const (
	ModeGaming   RunMode = "gaming"
	ModeAI       RunMode = "ai"
	ModeCreator  RunMode = "creator"
	ModeStreaming RunMode = "streaming"
	ModeFull     RunMode = "full"
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

// RunConfig holds all CLI flags and options for a run
type RunConfig struct {
	Mode        RunMode
	OutDir      string
	Zip         bool
	JSON        bool
	Markdown    bool
	Verbose     bool
	NoAdmin     bool
	Timeout     int // seconds
	Redact      bool
	IncludeLogs bool
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
		NoAdmin:     false,
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
	Hostname      string `json:"hostname,omitempty"` // will be redacted
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
	NvlddmkmErrors   []EventLogEntry `json:"nvlddmkm_errors,omitempty"`
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
	DmesgSnippets      string          `json:"dmesg_snippets,omitempty"`  // opt-in
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
	Severity     Severity `json:"severity"`
	Title        string   `json:"title"`
	Evidence     string   `json:"evidence"`
	WhyItMatters string   `json:"why_it_matters"`
	NextSteps    []string `json:"next_steps"`
	References   []string `json:"references,omitempty"`
	Category     string   `json:"category,omitempty"` // "driver", "cuda", "overlay", etc.
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
	Findings        []Finding        `json:"findings"`
	CollectorErrors []CollectorError `json:"collector_errors,omitempty"`
	TopIssues       []string         `json:"top_issues"`
	NextSteps       []string         `json:"next_steps"`
	SummaryBlock    string           `json:"summary_block"`
}

// ReportMetadata holds info about the report itself
type ReportMetadata struct {
	ToolVersion      string    `json:"tool_version"`
	Timestamp        time.Time `json:"timestamp"`
	Mode             RunMode   `json:"mode"`
	RuntimeSeconds   float64   `json:"runtime_seconds"`
	RedactionEnabled bool      `json:"redaction_enabled"`
	Platform         string    `json:"platform"` // "windows", "linux", "wsl"
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

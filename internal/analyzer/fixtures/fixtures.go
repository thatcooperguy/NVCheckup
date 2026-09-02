// Package fixtures builds synthetic reports for the unified-memory platforms
// described in docs/roadmap/spark-support.md so analyzer and renderer tests
// (and the report golden files) share one source of truth.
//
// Every value that the spec states is copied from it and cites the section;
// values the spec leaves open (dates, addresses, hostnames) are plainly
// synthetic test data and are marked as such. Nothing here is a claim about
// real hardware beyond what the spec records.
package fixtures

import (
	"time"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// FixtureTime is the fixed timestamp of every fixture so renderers produce
// byte-identical golden output.
var FixtureTime = time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)

// GB10 mirrors the simulated healthy DGX Spark of spec section 10:
// MemTotal 125513944 kB (119.7 GiB), nvidia-smi memory [N/A], compute
// capability 12.1, the misreported GEN 1@ 1x PCIe link on an on-package GPU,
// DGX OS 7.5.0 / OTA2607 with driver 580.159.03, the nvidia-dkms-580-open
// placeholder package, and two ConnectX-7 twins of one cabled cage.
//
// Expected findings in full mode (asserted by the analyzer tests):
// dgx-spark-detected, unified-memory-nvsmi-expected, secureboot-ok. None of
// the section 10 forbidden ids may fire.
func GB10() *types.Report {
	torn := 0
	prevBootClean := true
	pstoreEmpty := true
	return &types.Report{
		Metadata: types.ReportMetadata{
			ToolVersion:      types.Version,
			Timestamp:        FixtureTime,
			Mode:             types.ModeFull,
			RuntimeSeconds:   4.2,
			RedactionEnabled: true,
			Platform:         "linux",
			SchemaVersion:    types.SchemaVersion,
		},
		System: types.SystemInfo{
			OSName:        "Ubuntu",
			OSVersion:     "24.04",
			KernelVersion: "6.17.0-1031-nvidia", // spec 2.1: 6.17.0-1004..1031-nvidia; the fixed kernel of the cx7 regression row
			Architecture:  "arm64",
			BootMode:      "UEFI",
			SecureBoot:    "Enabled",
			CPUModel:      "Cortex-X925 / Cortex-A725 (20 cores)", // spec 3.1 lscpu Model name lines
			RAMTotalMB:    125513944 / 1024,
			Uptime:        "1d 2h 3m",
			Hostname:      "spark-test", // synthetic
		},
		GPUs: []types.GPUInfo{{
			Index:           0,
			Name:            "NVIDIA GB10", // spec 2.1 nvidia-smi name
			Vendor:          "NVIDIA",
			PCIVendorID:     "10de",
			PCIDeviceID:     "2e12",             // spec 3.1 row 5
			PCIBusID:        "0000000F:01:00.0", // spec 2.1 Bus-Id
			DriverVersion:   "580.159.03",
			IsNVIDIA:        true,
			Temperature:     42,
			ComputeCap:      "12.1",
			OnPackage:       true,
			MemoryReporting: "not-supported",
		}},
		Driver: types.DriverInfo{
			Version:       "580.159.03", // spec 2.1 FE table (Aug 2026)
			CUDAVersion:   "13.0",
			NvidiaSmiPath: "nvidia-smi",
		},
		Linux: &types.LinuxInfo{
			Distro:         "Ubuntu",
			DistroVersion:  "24.04",
			PackageManager: "apt",
			// spec 3.2 package names; nvidia-dkms-580-open is the section 10
			// placeholder that proves the metapackage exclusion.
			NVIDIAPackages: []string{
				"nvidia-driver-580-open 580.159.03-0ubuntu0.24.04.1",
				"nvidia-dkms-580-open 580.159.03-0ubuntu0.24.04.1",
				"nvidia-firmware-580-580.159.03 580.159.03-0ubuntu0.24.04.1",
				"linux-modules-nvidia-580-open-6.17.0-1031-nvidia 6.17.0-1031.31",
				"nvidia-kernel-common-580 580.159.03-0ubuntu0.24.04.1",
				"dgx-release 7.5.0",
				"dgx-dashboard 1.0",
				"nvidia-spark-ota-check 1.0",
			},
			LoadedModules:      map[string]bool{"nvidia": true, "nvidia_drm": true, "nvidia_modeset": true, "nvidia_uvm": true},
			SecureBootState:    "Enabled",
			DevNvidiaNodes:     []string{"/dev/nvidia0", "/dev/nvidiactl", "/dev/nvidia-uvm"},
			LibCudaPath:        "/usr/lib/aarch64-linux-gnu/libcuda.so.580.159.03",
			ContainerRuntime:   "docker",
			NVContainerToolkit: "1.19.0", // spec 5 docker-cdi-spec-missing: factory toolkit
		},
		Thermal: &types.ThermalInfo{
			TemperatureC:        42,
			PowerState:          "P8",
			CurrentClockMHz:     210,
			MaxClockMHz:         3003, // spec 2.1 Max Clocks Graphics
			PowerDrawW:          "9.87",
			PowerLimitW:         "", // spec 2.1: all power limits N/A
			FanSupported:        false,
			PowerLimitSupported: false,
			UtilizationPct:      0,
			GPUIndex:            0,
			EventCounters:       map[string]int64{"sw_power_cap": 0, "sw_thermal_slowdown": 0, "hw_thermal_slowdown": 0},
		},
		PCIe: &types.PCIeInfo{
			// spec 2.1 / 5.1: the link is misreported as GEN 1@ 1x. The
			// maximums are set so pcie-width-reduced WOULD fire were the GPU
			// not on-package; that is what the suppression test relies on.
			CurrentSpeed:   "Gen1",
			MaxSpeed:       "Gen5",
			CurrentWidth:   "x1",
			MaxWidth:       "x16",
			PowerState:     "P0",
			UtilizationPct: 95,
			GPUIndex:       0,
			OnPackage:      true,
		},
		Platform: types.PlatformInfo{
			Class:               "dgx-spark",
			Vendor:              "NVIDIA",           // spec 3.1 row 10 (Founders Edition)
			Model:               "NVIDIA_DGX_Spark", // spec 3.1 row 10
			ProductVersion:      "A.7",
			BIOSVersion:         "5.36_0ACUM023", // spec 3.1 row 10
			GPUSoC:              "GB10",
			ComputeCap:          "12.1",
			UnifiedMemory:       true,
			NvidiaKernelFlavour: true,
			// fwupdmgr rows: versions from the spec 2.1 FE table (Aug 2026); the
			// device names are synthetic (verbatim names are an open question).
			Firmware: []types.FirmwareComponent{
				{Name: "Embedded Controller", Version: "0x03000508"},
				{Name: "System Firmware (UEFI/SoC)", Version: "2.155.11"},
				{Name: "USB-PD Controller", Version: "0x00000516"},
			},
			ACPIThermalMC:    map[string]int{"thermal_zone0": 41000, "thermal_zone1": 43500},
			PrevBootClean:    &prevBootClean,
			PrevBootLastLine: "systemd-journald[512]: Journal stopped",
			PstoreEmpty:      &pstoreEmpty,
		},
		UnifiedMemory: &types.UnifiedMemoryInfo{
			MemTotalKB:     125513944, // spec 2.1 / 10: 119.7 GiB
			MemFreeKB:      118111232,
			MemAvailableKB: 121530368, // 115.9 GiB (spec 5.1 summary example)
			BuffersKB:      262144,
			CachedKB:       3145728,
			SwapTotalKB:    16777216, // ~16 GiB DGX OS default swapfile (spec 5, S124)
			SwapFreeKB:     16777216,
			AllocatableKB:  121530368 + 16777216, // spec 3.3: MemAvailable + SwapFree
			SwapDevices:    []string{"/swapfile"},
			Swappiness:     60,
		},
		DGXOS: &types.DGXOSInfo{
			Name:                 "DGX Spark",        // spec 3.1 row 4
			PrettyName:           "NVIDIA DGX Spark", // spec 3.1 row 4
			SWBuildVersion:       "7.5.0",            // spec 2.1 FE table
			OTAVersion:           "7.5.0",
			Platform:             "DGX Server for KVM", // spec 3.1 row 4 quirk
			SerialNumber:         "<serial>",
			OTAName:              "OTA2607", // spec 2.1: OTA2607 = July 2026 = 580.159.03
			OTATorn:              &torn,
			DriverPkgVersion:     "580.159.03-0ubuntu0.24.04.1",
			FirmwarePkgVersion:   "580.159.03-0ubuntu0.24.04.1",
			ModulesForKernel:     true,
			DashboardActive:      true,
			DashboardAdminActive: true,
			FwupdActive:          true,
			PersistencedActive:   true,
			DashboardPortOpen:    true,
		},
		Cluster: &types.ClusterInfo{
			// spec 2.1 / 9: port 0 twins rocep1s0f0 / roceP2p1s0f0 on PCI
			// 0000:01:00.0 and 0002:01:00.0; addresses are synthetic /24s per
			// the playbook layout (192.168.100.x / 192.168.101.x).
			Ports: []types.FabricPort{
				{RDMADev: "rocep1s0f0", Netdev: "enp1s0f0np0", PCIAddr: "0000:01:00.0", Cage: 0, State: "4: ACTIVE", PhysState: "5: LinkUp", SpeedMbps: 200000, MTU: 9000, IPv4: []string{"192.168.100.1/24"}, Persistent: true},
				{RDMADev: "roceP2p1s0f0", Netdev: "enP2p1s0f0np0", PCIAddr: "0002:01:00.0", Cage: 0, State: "4: ACTIVE", PhysState: "5: LinkUp", SpeedMbps: 200000, MTU: 9000, IPv4: []string{"192.168.101.1/24"}, Persistent: true},
			},
			HotplugFileEnabled: true,
			NetplanMTU:         9000,
			AvahiActive:        true,
			RDMATools:          []string{"ibstat", "ibdev2netdev", "avahi-browse"},
		},
	}
}

// GB10GSPFail is the gb10-gsp-fail variant of spec section 10: GB10 silicon
// classified from /etc/dgx-release and lspci, nvidia-smi answering "No
// devices were found", and the SEC2 / GSP kernel lines of spec 3.2. It must
// yield dgx-spark-gsp-init-failure CRIT and no no-nvidia-gpu.
func GB10GSPFail() *types.Report {
	r := GB10()
	r.GPUs = nil
	r.Thermal = nil
	r.PCIe = nil
	r.Driver = types.DriverInfo{NvidiaSmiPath: "nvidia-smi", NvidiaSmiOutput: "No devices were found\n"}
	r.CollectorErrors = []types.CollectorError{{
		Collector: "gpu.nvidia-smi",
		Error:     `nvidia-smi query failed: "No devices were found" (the driver is loaded but no NVIDIA GPU is visible to it)`,
		Fatal:     true,
	}}
	// spec 3.2 GSP failure strings, verbatim.
	r.Linux.DmesgSnippets = "NVRM: Xid (PCI:000f:01:00): 119, Timeout after 6s of waiting for RPC response from GPU0 GSP!\n" +
		"NVRM: ksec2PrepareBootCommands_GB20B: SEC2 secure boot partition timed out.\n" +
		"NVRM: RmInitAdapter: Cannot initialize GSP firmware RM\n" +
		"NVRM: RmInitAdapter failed! (0x62:0x65:2028)\n"
	r.Linux.XidErrors = []types.XidError{{Code: 119, Message: "GSP firmware error", Count: 1}}
	// A plain apt upgrade pulled a newer driver than the OTA-paired firmware
	// (spec 5 dgx-spark-gsp-init-failure: 580.173.02 on OTA2607 = 580.159.03).
	r.DGXOS.DriverPkgVersion = "580.173.02-0ubuntu0.24.04.1"
	r.DGXOS.FirmwarePkgVersion = "580.159.03-0ubuntu0.24.04.1"
	r.Platform.ComputeCap = ""
	r.Platform.GPUSoC = "GB10"
	return r
}

// RTXSpark is a Windows-on-Arm RTX Spark (N1X) device on the 616.00
// Developer Preview driver without nvidia-smi.exe (spec 2.2 / 8). Names and
// ids come from spec 2.2 and 3.2; the OEM model is the Surface RTX Spark Dev
// Box example of spec 3.2; the build number is 24H2 (26100, S74).
func RTXSpark() *types.Report {
	return &types.Report{
		Metadata: types.ReportMetadata{
			ToolVersion:      types.Version,
			Timestamp:        FixtureTime,
			Mode:             types.ModeFull,
			RuntimeSeconds:   3.1,
			RedactionEnabled: true,
			Platform:         "windows",
			SchemaVersion:    types.SchemaVersion,
		},
		System: types.SystemInfo{
			OSName:       "Microsoft Windows 11 Pro",
			OSVersion:    "24H2",
			OSBuild:      "26100", // S74: 24H2
			Architecture: "arm64",
			BootMode:     "UEFI",
			SecureBoot:   "Enabled",
			CPUModel:     "NVIDIA N1X (20 cores)", // spec 2.2: 20-core Grace CPU; exact Win32_Processor.Name unconfirmed
			RAMTotalMB:   131072,                  // spec 2.2: 128 GB Surface RTX Spark Dev Box (S122)
			Uptime:       "0d 4h 12m",
		},
		GPUs: []types.GPUInfo{{
			Index:           0,
			Name:            "NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)", // spec 2.2 driver name (S24 S25)
			Vendor:          "NVIDIA",
			PCIVendorID:     "10DE",
			PCIDeviceID:     "2E03", // spec 2.2 / 3.1 row 2
			DriverVersion:   "616.00",
			IsNVIDIA:        true,
			ComputeCap:      "12.1", // spec 2.2: inferred, not published
			OnPackage:       true,
			MemoryReporting: "not-supported",
		}},
		Driver: types.DriverInfo{
			Version: "616.00", // spec 2.2: first Arm64 driver, Developer Preview
			Source:  "nv_surface_woa.inf",
		},
		Windows: &types.WindowsInfo{
			HAGSEnabled: "Default (not configured)",
			GameMode:    "Enabled",
			PowerPlan:   "High performance",
		},
		Platform: types.PlatformInfo{
			Class:          "rtx-spark",
			Vendor:         "Microsoft",
			Model:          "Surface RTX Spark Dev Box", // spec 3.2 example
			GPUSoC:         "N1X",
			ComputeCap:     "12.1",
			UnifiedMemory:  true,
			IsWindowsOnArm: true,
			NativeMachine:  "ARM64",
		},
		UnifiedMemory: &types.UnifiedMemoryInfo{
			// Win32_OperatingSystem.TotalVisibleMemorySize / FreePhysicalMemory
			// as the pool (spec 8); synthetic values for a 128 GB unit.
			MemTotalKB:     131072 * 1024,
			MemFreeKB:      100 * 1024 * 1024,
			MemAvailableKB: 100 * 1024 * 1024,
			AllocatableKB:  100 * 1024 * 1024,
		},
	}
}

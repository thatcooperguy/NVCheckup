package windows

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// rtxSparkAdapterRows mirrors the spec 3.2 RTX Spark WMI strings: PNPDeviceID
// PCI\VEN_10DE&DEV_2E03&SUBSYS_...1414 (Microsoft subsystem), the 616.00
// Developer Preview INF nv_surface_woa.inf and a WDDM DriverVersion ending
// 16.1600 (the 32.0 prefix is unconfirmed), next to an ordinary desktop GPU.
const rtxSparkAdapterRows = `NVIDIA GeForce RTX 4090|PCI\VEN_10DE&DEV_2684&SUBSYS_889D1043&REV_A1\4&2F3A1B9C&0&0019|32.0.15.6081|oem12.inf
NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)|PCI\VEN_10DE&DEV_2E03&SUBSYS_00011414&REV_A1\4&1A2B3C4D&0&0008|32.0.16.1600|nv_surface_woa.inf
`

func TestApplyRTXSparkAdapter(t *testing.T) {
	rows := parseVideoAdapterRows(rtxSparkAdapterRows)
	if len(rows) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(rows))
	}
	var p types.PlatformInfo
	if !applyRTXSparkAdapter(&p, rows) {
		t.Fatal("RTX Spark N1X row must match")
	}
	if p.Class != "rtx-spark" || p.GPUSoC != "N1X" {
		t.Errorf("Class/GPUSoC = %q/%q", p.Class, p.GPUSoC)
	}
	if p.WoA == nil {
		t.Fatal("WoA facts missing")
	}
	if p.WoA.AdapterName != "NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)" || p.WoA.InfFilename != "nv_surface_woa.inf" || p.WoA.DriverVersion != "32.0.16.1600" {
		t.Errorf("WoA = %+v", p.WoA)
	}
	if !p.WoA.DeveloperPreview {
		t.Error("616.00 Developer Preview must be flagged")
	}

	var other types.PlatformInfo
	if applyRTXSparkAdapter(&other, parseVideoAdapterRows("NVIDIA GeForce RTX 4090|PCI\\VEN_10DE&DEV_2684&SUBSYS_889D1043|32.0.15.6081|oem12.inf\n")) {
		t.Error("a desktop GPU must not be classed rtx-spark")
	}
	if other.Class != "" || other.WoA != nil {
		t.Errorf("no match must leave PlatformInfo untouched: %+v", other)
	}
}

func TestRTXSparkDeviceID(t *testing.T) {
	cases := map[string]string{
		`PCI\VEN_10DE&DEV_2E03&SUBSYS_00011414&REV_A1`: "2E03",
		`pci\ven_10de&dev_2e06&subsys_00011414`:        "2E06",
		`PCI\VEN_10DE&DEV_2684&SUBSYS_889D1043`:        "",
		`PCI\VEN_8086&DEV_2E03`:                        "", // wrong vendor
		"":                                             "",
	}
	for in, want := range cases {
		if got := rtxSparkDeviceID(in); got != want {
			t.Errorf("rtxSparkDeviceID(%q) = %q, want %q", in, got, want)
		}
	}
	// Name-based match (spec 3.1 row 2) without a PNP id.
	if !isRTXSparkAdapter(wmiVideoAdapter{Name: "NVIDIA RTX Spark N1X (5120-core Blackwell RTX GPU)"}) {
		t.Error("adapter name containing RTX Spark N1X must match")
	}
}

func TestIsDeveloperPreviewDriver(t *testing.T) {
	cases := []struct {
		version, inf string
		want         bool
	}{
		{"32.0.16.1600", "nv_surface_woa.inf", true},
		{"32.0.16.1600", "nvlti.inf", true},          // trailing 16.1600 alone is enough
		{"31.0.16.1600", "", true},                   // prefix is unconfirmed, only the tail is matched
		{"32.0.16.1700", "nv_surface_woa.inf", true}, // INF alone is enough
		{"32.0.16.1700", "nvlti.inf", false},
		{"32.0.15.6081", "oem12.inf", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := isDeveloperPreviewDriver(c.version, c.inf); got != c.want {
			t.Errorf("isDeveloperPreviewDriver(%q,%q) = %v, want %v", c.version, c.inf, got, c.want)
		}
	}
}

func TestApplyWow64(t *testing.T) {
	cases := []struct {
		process, native uint16
		wantWoA         bool
		wantEmulated    bool
		wantNative      string
	}{
		{machineUnknown, machineARM64, true, false, "ARM64"}, // native arm64 build
		{machineAMD64, machineARM64, true, true, "ARM64"},    // x64 NVCheckup under Prism (rule woa-nvcheckup-emulated)
		{machineUnknown, machineAMD64, false, false, "AMD64"},
		{machineI386, machineAMD64, false, true, "AMD64"}, // 32-bit under WOW64 on x64
	}
	for _, c := range cases {
		var p types.PlatformInfo
		applyWow64(&p, c.process, c.native)
		if p.IsWindowsOnArm != c.wantWoA || p.ProcessEmulated != c.wantEmulated || p.NativeMachine != c.wantNative {
			t.Errorf("applyWow64(%#x,%#x) = woa %v emulated %v native %q", c.process, c.native, p.IsWindowsOnArm, p.ProcessEmulated, p.NativeMachine)
		}
	}
	if machineName(0x5064) != "0x5064" {
		t.Errorf("unknown machine label = %q", machineName(0x5064))
	}
}

func TestApplyArchEnv(t *testing.T) {
	cases := []struct {
		arch, w6432  string
		wantWoA      bool
		wantEmulated bool
		wantNative   string
	}{
		{"ARM64", "", true, false, "ARM64"},
		{"AMD64", "ARM64", true, true, "ARM64"},
		{"x86", "ARM64", true, true, "ARM64"},
		{"AMD64", "", false, false, "AMD64"},
		{"x86", "AMD64", false, true, "AMD64"},
		{"", "", false, false, ""},
	}
	for _, c := range cases {
		var p types.PlatformInfo
		applyArchEnv(&p, c.arch, c.w6432)
		if p.IsWindowsOnArm != c.wantWoA || p.ProcessEmulated != c.wantEmulated || p.NativeMachine != c.wantNative {
			t.Errorf("applyArchEnv(%q,%q) = woa %v emulated %v native %q", c.arch, c.w6432, p.IsWindowsOnArm, p.ProcessEmulated, p.NativeMachine)
		}
	}
}

func TestApplySystemProduct(t *testing.T) {
	// spec 3.2: Win32_Processor.Architecture 12, Win32_ComputerSystemProduct.Name "Surface RTX Spark Dev Box"
	sp := parseSystemProduct("12|Surface RTX Spark Dev Box|Microsoft Corporation|Microsoft Corporation|ARM64-based PC\n")
	var p types.PlatformInfo
	applySystemProduct(&p, sp)
	if !p.IsWindowsOnArm || p.NativeMachine != "ARM64" || p.Model != "Surface RTX Spark Dev Box" || p.Vendor != "Microsoft Corporation" {
		t.Errorf("applySystemProduct = %+v", p)
	}
	var x types.PlatformInfo
	x.Vendor = "existing"
	applySystemProduct(&x, parseSystemProduct("9|Precision 5570|Dell Inc.|Dell Inc.|x64-based PC"))
	if x.IsWindowsOnArm || x.Vendor != "existing" || x.Model != "Precision 5570" {
		t.Errorf("x64 product = %+v", x)
	}
}

// writeSyntheticPE writes the smallest PE image debug/pe accepts: an MZ stub
// pointing at "PE\0\0" plus a file header with the given machine, no
// sections and no optional header, padded to the 96 bytes NewFile reads.
func writeSyntheticPE(t *testing.T, machine uint16) string {
	t.Helper()
	buf := make([]byte, 128)
	buf[0], buf[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(buf[0x3c:], 0x40)
	copy(buf[0x40:], []byte{'P', 'E', 0, 0})
	binary.LittleEndian.PutUint16(buf[0x44:], machine)
	path := filepath.Join(t.TempDir(), "nvcc.exe")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPeMachine(t *testing.T) {
	for _, c := range []struct {
		machine uint16
		want    string
	}{{machineAMD64, "AMD64"}, {machineARM64, "ARM64"}} {
		path := writeSyntheticPE(t, c.machine)
		m, err := peMachine(path)
		if err != nil {
			t.Fatalf("peMachine(%s): %v", path, err)
		}
		if machineName(m) != c.want {
			t.Errorf("machine = %q, want %q", machineName(m), c.want)
		}
	}
	if _, err := peMachine(filepath.Join(t.TempDir(), "missing.exe")); err == nil {
		t.Error("missing file must error")
	}
}

func TestCollectNvccMachineFromCUDAPath(t *testing.T) {
	cudaPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cudaPath, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := writeSyntheticPE(t, machineAMD64)
	data, _ := os.ReadFile(src)
	if err := os.WriteFile(filepath.Join(cudaPath, "bin", "nvcc.exe"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CUDA_PATH", cudaPath)
	var p types.PlatformInfo
	collectNvccMachine(&p)
	if p.WoA == nil || p.WoA.NvccMachine != "AMD64" {
		t.Errorf("nvcc machine = %+v, want AMD64 (rule woa-cuda-toolkit-not-native)", p.WoA)
	}
}

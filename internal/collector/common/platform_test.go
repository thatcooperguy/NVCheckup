package common

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Verbatim fixtures from docs/roadmap/spark-support.md sections 2-3 and the
// gb10 field-test scenario.
const (
	dgxReleaseSample = "DGX_NAME=\"DGX Spark\"\nDGX_PRETTY_NAME=\"NVIDIA DGX Spark\"\nDGX_SWBUILD_DATE=\"2025-09-10-13-50-03\"\n" +
		"DGX_SWBUILD_VERSION=\"7.2.3\"\nDGX_COMMIT_ID=\"833b4a7\"\nDGX_PLATFORM=\"DGX Server for KVM\"\nDGX_SERIAL_NUMBER=\"1234567890123\"\n" +
		"DGX_OTA_VERSION=\"7.5.0\"\nDGX_OTA_DATE=\"Wed Jul 15 09:06:56 AM PDT 2026\"\n"
	fastosReleaseSample = "NAME=\"DGX SPARK FASTOS\"\nVERSION=\"1.91.51\"\nBUILD_TYPE=\"customer\"\n"

	lspciGB10 = "0000:01:00.0 Ethernet controller [0200]: Mellanox Technologies MT2910 Family [ConnectX-7] [15b3:1021]\n" +
		"0002:01:00.1 Ethernet controller [0200]: Mellanox Technologies MT2910 Family [ConnectX-7] [15b3:1021]\n" +
		"000f:01:00.0 VGA compatible controller [0300]: NVIDIA Corporation Device [10de:2e12] (rev a1)\n"
	lspciN1X6k   = "0001:01:00.0 VGA compatible controller [0300]: NVIDIA Corporation GB20B [RTX Spark N1X 6144] [10de:2e03] (rev a1)\n"
	lspciN1X5k   = "0001:01:00.0 VGA compatible controller [0300]: NVIDIA Corporation GB20B [RTX Spark N1X 5120] [10de:2e06] (rev a1)\n"
	lspciRTX4090 = "01:00.0 VGA compatible controller [0300]: NVIDIA Corporation AD102 [GeForce RTX 4090] [10de:2684] (rev a1)\n"
	lspciGH200   = "0009:01:00.0 3D controller [0302]: NVIDIA Corporation GH100 [GH200 480GB] [10de:2342] (rev a1)\n"
	kernelGB10   = "6.17.0-1026-nvidia"
)

var dmiFE = map[string]string{
	"sys_vendor": "NVIDIA", "product_name": "NVIDIA_DGX_Spark", "product_version": "A.7",
	"bios_version": "5.36_0ACUM023", "bios_date": "12/25/2025",
}

var dmiASUS = map[string]string{
	"sys_vendor": "ASUS", "product_name": "Ascent GX10", "product_version": "A.7",
	"bios_version": "GX10DGX.0102.2025.1111.1531", "bios_date": "11/11/2025",
}

func linuxInputs() platformInputs {
	return platformInputs{GOOS: "linux", GOARCH: "arm64", ProcessorArchitecture: -1}
}

// TestClassifyPhase1_DecisionTable runs the file/lspci/DMI/kernel rows of spec
// table 3.1 (rows 3, 4, 5-lspci, 6, 10, 11) including the confusables of
// section 2.3 and the OEM GB10 boxes of the exact-string reference.
func TestClassifyPhase1_DecisionTable(t *testing.T) {
	cases := []struct {
		name   string
		in     func() platformInputs
		class  string
		soc    string
		vendor string
		model  string
		kernel bool
		fe     bool
	}{
		{
			name: "row 4: stock DGX OS Founders Edition",
			in: func() platformInputs {
				in := linuxInputs()
				in.DGXRelease, in.FastOSRelease, in.Lspci, in.DMI, in.Kernel = dgxReleaseSample, fastosReleaseSample, lspciGB10, dmiFE, kernelGB10
				return in
			},
			class: ClassDGXSpark, soc: socGB10, vendor: "NVIDIA", model: "NVIDIA_DGX_Spark", kernel: true, fe: true,
		},
		{
			name: "row 4 by DGX_PRETTY_NAME only",
			in: func() platformInputs {
				in := linuxInputs()
				in.DGXRelease = "DGX_PRETTY_NAME=\"NVIDIA DGX Spark\"\n"
				return in
			},
			class: ClassDGXSpark, soc: socGB10,
		},
		{
			name: "row 4 by /etc/fastos-release only",
			in: func() platformInputs {
				in := linuxInputs()
				in.FastOSRelease = fastosReleaseSample
				return in
			},
			class: ClassDGXSpark, soc: socGB10,
		},
		{
			name: "row 5: OEM GB10 (ASUS Ascent GX10) without dgx-release, lspci 10de:2e12",
			in: func() platformInputs {
				in := linuxInputs()
				in.Lspci, in.DMI, in.Kernel = lspciGB10, dmiASUS, "6.14.0-1015-nvidia"
				return in
			},
			class: ClassDGXSpark, soc: socGB10, vendor: "ASUS", model: "Ascent GX10", kernel: true, fe: false,
		},
		{
			name: "row 4/10: HP ZGX Nano G1n with dgx-release is dgx-spark, vendor HP",
			in: func() platformInputs {
				in := linuxInputs()
				in.DGXRelease, in.Lspci = dgxReleaseSample, lspciGB10
				in.DMI = map[string]string{"sys_vendor": "HP", "product_name": "HP ZGX Nano G1n AI Station"}
				return in
			},
			class: ClassDGXSpark, soc: socGB10, vendor: "HP", model: "HP ZGX Nano G1n AI Station", fe: false,
		},
		{
			name: "row 10: Lenovo ThinkStation PGX (30KL0004FC)",
			in: func() platformInputs {
				in := linuxInputs()
				in.Lspci = lspciGB10
				in.DMI = map[string]string{"sys_vendor": "LENOVO", "product_name": "30KL0004FC", "product_version": "ThinkStation PGX"}
				return in
			},
			class: ClassDGXSpark, soc: socGB10, vendor: "LENOVO", model: "30KL0004FC", fe: false,
		},
		{
			name: "row 6: N1X 6144 on Linux is rtx-spark, never dgx-spark",
			in: func() platformInputs {
				in := linuxInputs()
				in.Lspci, in.Kernel = lspciN1X6k, "6.17.0-1004-nvidia"
				return in
			},
			class: ClassRTXSpark, soc: socN1X, kernel: true,
		},
		{
			name: "row 6: N1X 5120 on Linux",
			in: func() platformInputs {
				in := linuxInputs()
				in.Lspci = lspciN1X5k
				return in
			},
			class: ClassRTXSpark, soc: socN1X,
		},
		{
			name: "row 3: Jetson Thor (has nvidia-smi) by /etc/nv_tegra_release wins over PCI ids",
			in: func() platformInputs {
				in := linuxInputs()
				in.TegraReleasePresent = true
				in.DeviceTreeModel = "NVIDIA Jetson AGX Thor Developer Kit"
				in.Lspci = lspciRTX4090
				return in
			},
			class: ClassJetson, model: "NVIDIA Jetson AGX Thor Developer Kit",
		},
		{
			name: "row 3: Jetson by device-tree model only",
			in: func() platformInputs {
				in := linuxInputs()
				in.DeviceTreeModel = "NVIDIA Jetson AGX Orin Developer Kit"
				return in
			},
			class: ClassJetson, model: "NVIDIA Jetson AGX Orin Developer Kit",
		},
		{
			name: "GH200 (confusable): no class in phase 1, nvidia kernel flavour noted",
			in: func() platformInputs {
				in := linuxInputs()
				in.Lspci, in.Kernel = lspciGH200, "6.8.0-1010-nvidia-64k"
				in.DMI = map[string]string{"sys_vendor": "Supermicro", "product_name": "ARS-111GL-NHR"}
				return in
			},
			class: "", vendor: "Supermicro", model: "ARS-111GL-NHR", kernel: true,
		},
		{
			name: "x86 desktop with RTX 4090 and an OEM vendor string is not dgx-spark",
			in: func() platformInputs {
				in := linuxInputs()
				in.GOARCH = "amd64"
				in.Lspci, in.Kernel = lspciRTX4090, "6.8.0-45-generic"
				in.DMI = map[string]string{"sys_vendor": "ASUS", "product_name": "ROG STRIX"}
				return in
			},
			class: "", vendor: "ASUS", model: "ROG STRIX", kernel: false,
		},
		{
			name: "no files, no lspci, no DMI: empty class and no panic",
			in:   linuxInputs,
		},
	}
	for _, c := range cases {
		p := classifyPhase1(c.in())
		if p.Class != c.class {
			t.Errorf("%s: Class = %q, want %q", c.name, p.Class, c.class)
		}
		if p.GPUSoC != c.soc {
			t.Errorf("%s: GPUSoC = %q, want %q", c.name, p.GPUSoC, c.soc)
		}
		if p.Vendor != c.vendor || p.Model != c.model {
			t.Errorf("%s: Vendor/Model = %q/%q, want %q/%q", c.name, p.Vendor, p.Model, c.vendor, c.model)
		}
		if p.NvidiaKernelFlavour != c.kernel {
			t.Errorf("%s: NvidiaKernelFlavour = %v, want %v", c.name, p.NvidiaKernelFlavour, c.kernel)
		}
		if got := IsFoundersEdition(p); got != c.fe {
			t.Errorf("%s: IsFoundersEdition = %v, want %v", c.name, got, c.fe)
		}
		// Phase 1 never sets the GPU-derived flags; ApplyPlatformFlags does.
		if p.UnifiedMemory {
			t.Errorf("%s: phase 1 must not set UnifiedMemory", c.name)
		}
	}
}

func TestClassifyPhase1_DMIRecordedForFE(t *testing.T) {
	in := linuxInputs()
	in.DGXRelease, in.DMI = dgxReleaseSample, dmiFE
	p := classifyPhase1(in)
	if p.ProductVersion != "A.7" || p.BIOSVersion != "5.36_0ACUM023" || p.BIOSDate != "12/25/2025" {
		t.Errorf("DMI fields not recorded: %+v", p)
	}
	for _, v := range []string{"ASUS", "HP", "LENOVO", "Dell", "MSI", "Acer", "GIGABYTE", "Dell Inc.", "gigabyte"} {
		if !IsOEMGB10Vendor(v) {
			t.Errorf("IsOEMGB10Vendor(%q) = false", v)
		}
	}
	for _, v := range []string{"NVIDIA", "Supermicro", ""} {
		if IsOEMGB10Vendor(v) {
			t.Errorf("IsOEMGB10Vendor(%q) = true", v)
		}
	}
}

func TestIsNvidiaKernelFlavour(t *testing.T) {
	yes := []string{"6.17.0-1026-nvidia", "6.11.0-1004-nvidia", "6.14.0-1015-nvidia-64k", "6.17.0-1031-nvidia-lowlatency", " 6.8.0-1010-nvidia\n"}
	no := []string{"6.8.0-45-generic", "6.17.0-1026-nvidia-extra", "6.17.0-nvidia", "5.15.0-1043-nvidia-tegra", "", "nvidia"}
	for _, k := range yes {
		if !IsNvidiaKernelFlavour(k) {
			t.Errorf("IsNvidiaKernelFlavour(%q) = false", k)
		}
	}
	for _, k := range no {
		if IsNvidiaKernelFlavour(k) {
			t.Errorf("IsNvidiaKernelFlavour(%q) = true", k)
		}
	}
}

func TestClassifyPhase1_Windows(t *testing.T) {
	n1x := windowsAdapter{
		Name:          "NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)",
		PNPDeviceID:   `PCI\VEN_10DE&DEV_2E03&SUBSYS_00011414&REV_A1\4&1A2B3C4D&0&0008`,
		InfFilename:   "nv_surface_woa.inf",
		DriverVersion: "32.0.16.1600",
	}
	rtx3090 := windowsAdapter{Name: "NVIDIA GeForce RTX 3090", PNPDeviceID: `PCI\VEN_10DE&DEV_2204&SUBSYS_38811462&REV_A1\4&5E6F7A8B&0&0019`, InfFilename: "oem12.inf", DriverVersion: "32.0.15.9186"}

	// Native arm64 build on an RTX Spark device (row 1 + row 2).
	in := platformInputs{GOOS: "windows", GOARCH: "arm64", ProcessorArchitecture: 12, Adapters: []windowsAdapter{n1x},
		ComputerManufacturer: "Microsoft Corporation", ComputerProductName: "Surface RTX Spark Dev Box"}
	p := classifyPhase1(in)
	if p.Class != ClassRTXSpark || p.GPUSoC != socN1X || !p.IsWindowsOnArm || p.ProcessEmulated || p.NativeMachine != "ARM64" {
		t.Errorf("native arm64 RTX Spark = %+v", p)
	}
	if p.Vendor != "Microsoft Corporation" || p.Model != "Surface RTX Spark Dev Box" {
		t.Errorf("WMI system strings not recorded: %+v", p)
	}

	// x64 build running under Prism on the same device: emulated.
	in = platformInputs{GOOS: "windows", GOARCH: "amd64", ProcessorArchitecture: 12, Adapters: []windowsAdapter{n1x}}
	p = classifyPhase1(in)
	if p.Class != ClassRTXSpark || !p.IsWindowsOnArm || !p.ProcessEmulated || p.NativeMachine != "ARM64" {
		t.Errorf("emulated x64 on Arm = %+v", p)
	}
	// PROCESSOR_ARCHITEW6432 alone also marks emulation when WMI is silent.
	in = platformInputs{GOOS: "windows", GOARCH: "amd64", ProcessorArchitecture: -1, ArchitEW6432: "ARM64"}
	if p = classifyPhase1(in); !p.IsWindowsOnArm || !p.ProcessEmulated {
		t.Errorf("ARCHITEW6432=ARM64 = %+v", p)
	}

	// Ordinary x64 desktop.
	in = platformInputs{GOOS: "windows", GOARCH: "amd64", ProcessorArchitecture: 9, Adapters: []windowsAdapter{rtx3090}}
	p = classifyPhase1(in)
	if p.Class != "" || p.IsWindowsOnArm || p.ProcessEmulated || p.NativeMachine != "AMD64" || p.UnifiedMemory {
		t.Errorf("x64 desktop = %+v", p)
	}
}

func TestIsRTXSparkAdapter(t *testing.T) {
	cases := []struct {
		name, pnp, inf, drv string
		want                bool
	}{
		{"", `PCI\VEN_10DE&DEV_2E03&SUBSYS_00011414`, "", "", true},
		{"", `pci\ven_10de&dev_2e06&subsys_0001103c`, "", "", true},
		{"NVIDIA RTX Spark N1X (5120-core Blackwell RTX GPU)", "", "", "", true},
		{"NVIDIA Display", `PCI\VEN_10DE&DEV_0000`, "nv_surface_woa.inf", "", true},
		{"NVIDIA Display", `PCI\VEN_10DE&DEV_0000`, "", "32.0.16.1600", true},
		{"Intel Display", `PCI\VEN_8086&DEV_A7A0`, "nv_surface_woa.inf", "32.0.16.1600", false},
		{"NVIDIA GeForce RTX 4090", `PCI\VEN_10DE&DEV_2684`, "oem12.inf", "32.0.15.6094", false},
		{"NVIDIA GeForce RTX 4060 Laptop GPU", `PCI\VEN_10DE&DEV_28A0`, "nvltwi.inf", "32.0.15.6094", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		if got := IsRTXSparkAdapter(c.name, c.pnp, c.inf, c.drv); got != c.want {
			t.Errorf("IsRTXSparkAdapter(%q,%q,%q,%q) = %v, want %v", c.name, c.pnp, c.inf, c.drv, got, c.want)
		}
	}
}

func TestParseWindowsPlatformOutput(t *testing.T) {
	out := "arch=12\r\nmanufacturer=ASUSTeK COMPUTER INC.\r\nproduct=ProArt P16\r\nversion=1.0\r\nbios=P16.303\r\nbios_date=20260801000000.000000+000\r\n" +
		"adapter=NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)|PCI\\VEN_10DE&DEV_2E03&SUBSYS_00011043|nv_surface_woa.inf|32.0.16.1600\r\n" +
		"adapter=|||\r\n"
	in := platformInputs{ProcessorArchitecture: -1}
	parseWindowsPlatformOutput(&in, out)
	if in.ProcessorArchitecture != 12 || in.ComputerManufacturer != "ASUSTeK COMPUTER INC." || in.ComputerProductName != "ProArt P16" || in.BIOSVersion != "P16.303" {
		t.Errorf("inputs = %+v", in)
	}
	if len(in.Adapters) != 1 || in.Adapters[0].InfFilename != "nv_surface_woa.inf" || in.Adapters[0].DriverVersion != "32.0.16.1600" {
		t.Errorf("adapters = %+v", in.Adapters)
	}
}

func TestParseDGXRelease(t *testing.T) {
	d := ParseDGXRelease(dgxReleaseSample)
	want := types.DGXOSInfo{Name: "DGX Spark", PrettyName: "NVIDIA DGX Spark", SWBuildDate: "2025-09-10-13-50-03", SWBuildVersion: "7.2.3",
		CommitID: "833b4a7", Platform: "DGX Server for KVM", SerialNumber: "1234567890123", OTAVersion: "7.5.0", OTADate: "Wed Jul 15 09:06:56 AM PDT 2026"}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("ParseDGXRelease\n got %+v\nwant %+v", d, want)
	}
	if name, ver := parseFastOSRelease(fastosReleaseSample); name != "DGX SPARK FASTOS" || ver != "1.91.51" {
		t.Errorf("parseFastOSRelease = %q %q", name, ver)
	}
	if d := ParseDGXRelease(""); !reflect.DeepEqual(d, types.DGXOSInfo{}) {
		t.Errorf("empty file parsed as %+v", d)
	}
}

func TestLspciNVIDIADeviceIDs(t *testing.T) {
	ids := lspciNVIDIADeviceIDs(lspciGB10)
	if !ids["2e12"] || ids["1021"] || len(ids) != 1 {
		t.Errorf("ids = %v", ids)
	}
	if !LspciHasGB10(lspciGB10) || LspciHasGB10(lspciRTX4090) || LspciHasGB10("") {
		t.Error("LspciHasGB10 wrong")
	}
	if ids := lspciNVIDIADeviceIDs("000f:01:00.0 VGA compatible controller [0300]: NVIDIA Corporation Device [10DE:2E12] (rev a1)"); !ids["2e12"] {
		t.Error("upper-case ids must be normalised")
	}
}

// Fixtures under NVC_SIM_ROOT drive the file readers exactly as the CI
// scenario does (spec section 10).
func TestSimRootReaders(t *testing.T) {
	root := t.TempDir()
	t.Setenv(SimRootEnv, root)
	mk := func(rel, content string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mk("/etc/dgx-release", dgxReleaseSample)
	mk("/etc/fastos-release", fastosReleaseSample)
	mk("/proc/sys/kernel/osrelease", kernelGB10+"\n")
	for k, v := range dmiFE {
		mk("/sys/class/dmi/id/"+k, v+"\n")
	}

	if got := SimPath("/etc/dgx-release"); got != filepath.Join(root, "etc", "dgx-release") {
		t.Errorf("SimPath = %q", got)
	}
	if got := SimPath("relative/file"); got != "relative/file" {
		t.Errorf("relative paths must pass through, got %q", got)
	}
	if !SimFileExists("/etc/dgx-release") || SimFileExists("/etc/nv_tegra_release") {
		t.Error("SimFileExists wrong")
	}

	dgx, errs := CollectDGXRelease()
	if len(errs) != 0 || dgx == nil {
		t.Fatalf("CollectDGXRelease = %+v, %+v", dgx, errs)
	}
	if dgx.OTAVersion != "7.5.0" || dgx.SerialNumber != "1234567890123" || dgx.FastOSVersion != "1.91.51" {
		t.Errorf("dgx = %+v", dgx)
	}
	if got := readKernelRelease(5); got != kernelGB10 {
		t.Errorf("readKernelRelease = %q", got)
	}
	dmi := readDMI(5)
	if dmi["sys_vendor"] != "NVIDIA" || dmi["product_name"] != "NVIDIA_DGX_Spark" || dmi["product_version"] != "A.7" || dmi["bios_version"] != "5.36_0ACUM023" {
		t.Errorf("readDMI = %v", dmi)
	}

	// The Jetson detector reads through the same root.
	if is, _ := DetectJetson(); is {
		t.Error("no tegra files under the sim root, yet DetectJetson = true")
	}
	mk("/etc/nv_tegra_release", tegraReleaseSample+"\n")
	if is, rel := DetectJetson(); !is || rel != tegraReleaseSample {
		t.Errorf("DetectJetson via sim root = (%v, %q)", is, rel)
	}

	// Without the release files CollectDGXRelease reports nothing.
	t.Setenv(SimRootEnv, t.TempDir())
	if dgx, errs := CollectDGXRelease(); dgx != nil || len(errs) != 0 {
		t.Errorf("empty root: %+v %+v", dgx, errs)
	}
	if got := SimPath("/etc/x"); !strings.HasPrefix(got, SimRoot()) {
		t.Errorf("SimPath after root change = %q", got)
	}
}

// --- ApplyPlatformFlags: rows 5, 7, 8, 9 and flag rules A-C ---------------

func gb10GPU() types.GPUInfo {
	return types.GPUInfo{Index: 0, Name: "NVIDIA GB10", Vendor: "NVIDIA", IsNVIDIA: true, PCIBusID: "0000000F:01:00.0",
		DriverVersion: "580.159.03", ComputeCap: "12.1", MemoryReporting: MemoryReportingNotSupported}
}

func gen1x1PCIe() []types.PCIeInfo {
	// GB10 misreports its link as GEN 1@ 1x (spec 2.1).
	return []types.PCIeInfo{{GPUIndex: 0, CurrentSpeed: "Gen1", MaxSpeed: "Gen1", CurrentWidth: "x1", MaxWidth: "x1"}}
}

func newReport(arch, class string, gpus ...types.GPUInfo) *types.Report {
	r := &types.Report{GPUs: gpus, GPUPCIe: gen1x1PCIe()}
	r.PCIe = &r.GPUPCIe[0]
	r.System.Architecture = arch
	r.Platform.Class = class
	r.Metadata.Platform = "linux"
	return r
}

func TestApplyPlatformFlags_Rows(t *testing.T) {
	cases := []struct {
		name          string
		report        *types.Report
		class, soc    string
		unified       bool
		gpuOnPackage  bool
		pcieOnPackage bool
		computeCap    string
	}{
		{
			name:   "stock DGX OS (row 4 matched in phase 1) still gets flag rule A",
			report: newReport("arm64", ClassDGXSpark, gb10GPU()),
			class:  ClassDGXSpark, soc: socGB10, unified: true, gpuOnPackage: true, pcieOnPackage: true, computeCap: "12.1",
		},
		{
			name:   "row 5 by nvidia-smi name NVIDIA GB10",
			report: newReport("arm64", "", gb10GPU()),
			class:  ClassDGXSpark, soc: socGB10, unified: true, gpuOnPackage: true, pcieOnPackage: true, computeCap: "12.1",
		},
		{
			name: "row 5 by lspci device id when nvidia-smi is dead (GSP failure)",
			report: newReport("arm64", "", types.GPUInfo{Index: 0, Name: "NVIDIA Corporation Device", Vendor: "NVIDIA", IsNVIDIA: true,
				PCIBusID: "000f:01:00.0", PCIVendorID: "10de", PCIDeviceID: "2e12"}),
			class: ClassDGXSpark, soc: socGB10, unified: true, gpuOnPackage: true, pcieOnPackage: true,
		},
		{
			name: "row 7: GH200 with numeric HBM memory and CC 9.0 is grace-hopper (flag rule C)",
			report: newReport("arm64", "", types.GPUInfo{Index: 0, Name: "NVIDIA GH200 480GB", Vendor: "NVIDIA", IsNVIDIA: true,
				VRAMTotalMB: 97871, ComputeCap: "9.0", MemoryReporting: MemoryReportingDedicated}),
			class: ClassGraceHopper, soc: "GH200", unified: false, gpuOnPackage: false, pcieOnPackage: false, computeCap: "9.0",
		},
		{
			name: "row 7: DGX Station GB300 (numeric memory) is grace-hopper, not dgx-spark",
			report: newReport("arm64", "", types.GPUInfo{Index: 0, Name: "NVIDIA GB300", Vendor: "NVIDIA", IsNVIDIA: true,
				VRAMTotalMB: 294912, ComputeCap: "10.3", MemoryReporting: MemoryReportingDedicated}),
			class: ClassGraceHopper, soc: "GB300", unified: false, computeCap: "10.3",
		},
		{
			name: "Jetson Thor with nvidia-smi: class from row 3, [N/A] memory -> flag rules A and B",
			report: newReport("arm64", ClassJetson, types.GPUInfo{Index: 0, Name: "NVIDIA Thor", Vendor: "NVIDIA", IsNVIDIA: true,
				ComputeCap: "11.0", MemoryReporting: MemoryReportingNotSupported}),
			class: ClassJetson, soc: "", unified: true, gpuOnPackage: true, pcieOnPackage: true, computeCap: "11.0",
		},
		{
			name: "Thor booted without /etc/nv_tegra_release: class stays empty, flag rule B still applies",
			report: newReport("arm64", "", types.GPUInfo{Index: 0, Name: "NVIDIA Thor", Vendor: "NVIDIA", IsNVIDIA: true,
				ComputeCap: "11.0", MemoryReporting: MemoryReportingNotSupported}),
			class: "", soc: "", unified: true, gpuOnPackage: true, pcieOnPackage: true, computeCap: "11.0",
		},
		{
			name: "row 9: CC 12.1 + [N/A] without a PCI-id match never asserts a class",
			report: newReport("arm64", "", types.GPUInfo{Index: 0, Name: "NVIDIA Graphics Device", Vendor: "NVIDIA", IsNVIDIA: true,
				ComputeCap: "12.1", MemoryReporting: MemoryReportingNotSupported}),
			class: "", soc: socUnknownCC121, unified: true, gpuOnPackage: true, pcieOnPackage: true, computeCap: "12.1",
		},
		{
			name: "row 8: arm64 with a discrete RTX 6000 Ada (numeric memory)",
			report: newReport("arm64", "", types.GPUInfo{Index: 0, Name: "NVIDIA RTX 6000 Ada Generation", Vendor: "NVIDIA", IsNVIDIA: true,
				VRAMTotalMB: 49140, ComputeCap: "8.9", MemoryReporting: MemoryReportingDedicated}),
			class: ClassArm64DGPU, soc: "", unified: false, computeCap: "8.9",
		},
		{
			name: "x86 desktop RTX 3090: nothing matches, no flags",
			report: newReport("amd64", "", types.GPUInfo{Index: 0, Name: "NVIDIA GeForce RTX 3090", Vendor: "NVIDIA", IsNVIDIA: true,
				VRAMTotalMB: 24576, ComputeCap: "8.6", MemoryReporting: MemoryReportingDedicated}),
			class: "", soc: "", unified: false, computeCap: "8.6",
		},
		{
			name: "N1X on Linux classed rtx-spark in phase 1 keeps its class despite CC 12.1",
			report: newReport("arm64", ClassRTXSpark, types.GPUInfo{Index: 0, Name: "NVIDIA GB20B", Vendor: "NVIDIA", IsNVIDIA: true,
				ComputeCap: "12.1", MemoryReporting: MemoryReportingNotSupported}),
			class: ClassRTXSpark, soc: socN1X, unified: true, gpuOnPackage: true, pcieOnPackage: true, computeCap: "12.1",
		},
		{
			name:   "no GPUs at all: nothing changes",
			report: newReport("amd64", ""),
			class:  "", soc: "", unified: false,
		},
	}
	for _, c := range cases {
		r := c.report
		ApplyPlatformFlags(r)
		p := r.Platform
		if p.Class != c.class || p.GPUSoC != c.soc || p.UnifiedMemory != c.unified || p.ComputeCap != c.computeCap {
			t.Errorf("%s: platform = class %q soc %q unified %v cc %q; want %q %q %v %q", c.name, p.Class, p.GPUSoC, p.UnifiedMemory, p.ComputeCap, c.class, c.soc, c.unified, c.computeCap)
		}
		for _, g := range r.GPUs {
			if g.IsNVIDIA && g.OnPackage != c.gpuOnPackage {
				t.Errorf("%s: GPU %d OnPackage = %v, want %v", c.name, g.Index, g.OnPackage, c.gpuOnPackage)
			}
		}
		for _, pc := range r.GPUPCIe {
			if pc.OnPackage != c.pcieOnPackage {
				t.Errorf("%s: PCIe OnPackage = %v, want %v", c.name, pc.OnPackage, c.pcieOnPackage)
			}
		}
		if r.PCIe != nil && r.PCIe.OnPackage != c.pcieOnPackage {
			t.Errorf("%s: r.PCIe.OnPackage = %v, want %v", c.name, r.PCIe.OnPackage, c.pcieOnPackage)
		}
	}
	ApplyPlatformFlags(nil) // must not panic
}

// Flag rule B is per GPU: on a mixed rig only the [N/A] GPU and its PCIe row
// are marked on-package, while the platform is still flagged unified.
func TestApplyPlatformFlags_RuleBPerGPU(t *testing.T) {
	r := &types.Report{
		GPUs: []types.GPUInfo{
			{Index: 0, Name: "NVIDIA Graphics Device", IsNVIDIA: true, MemoryReporting: MemoryReportingNotSupported},
			{Index: 1, Name: "NVIDIA GeForce RTX 4090", IsNVIDIA: true, VRAMTotalMB: 24564, MemoryReporting: MemoryReportingDedicated},
			{Index: 2, Name: "Intel(R) UHD Graphics", Vendor: "Intel"},
		},
		GPUPCIe: []types.PCIeInfo{{GPUIndex: 0}, {GPUIndex: 1}},
	}
	r.System.Architecture = "amd64"
	ApplyPlatformFlags(r)
	if !r.Platform.UnifiedMemory || r.Platform.Class != "" {
		t.Errorf("platform = %+v", r.Platform)
	}
	if !r.GPUs[0].OnPackage || r.GPUs[1].OnPackage || r.GPUs[2].OnPackage {
		t.Errorf("GPU OnPackage = %v %v %v", r.GPUs[0].OnPackage, r.GPUs[1].OnPackage, r.GPUs[2].OnPackage)
	}
	if !r.GPUPCIe[0].OnPackage || r.GPUPCIe[1].OnPackage {
		t.Errorf("PCIe OnPackage = %v %v", r.GPUPCIe[0].OnPackage, r.GPUPCIe[1].OnPackage)
	}
}

// On Windows the RTX Spark adapter is recognised from the GPU inventory when
// phase 1 had no WMI answer (row 2 fallback).
func TestApplyPlatformFlags_WindowsRTXSparkFallback(t *testing.T) {
	r := &types.Report{GPUs: []types.GPUInfo{{Index: 0, Name: "NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)", Vendor: "NVIDIA", IsNVIDIA: true}}}
	r.Metadata.Platform = "windows"
	r.System.Architecture = "arm64"
	ApplyPlatformFlags(r)
	if r.Platform.Class != ClassRTXSpark || r.Platform.GPUSoC != socN1X || !r.Platform.UnifiedMemory || !r.GPUs[0].OnPackage {
		t.Errorf("platform = %+v gpu = %+v", r.Platform, r.GPUs[0])
	}
	r = &types.Report{GPUs: []types.GPUInfo{{Index: 0, Name: "NVIDIA Display", Vendor: "NVIDIA", IsNVIDIA: true, PCIVendorID: "10DE", PCIDeviceID: "2E06"}}}
	r.Metadata.Platform = "windows"
	ApplyPlatformFlags(r)
	if r.Platform.Class != ClassRTXSpark {
		t.Errorf("DEV_2E06 from WMI inventory not recognised: %+v", r.Platform)
	}
}

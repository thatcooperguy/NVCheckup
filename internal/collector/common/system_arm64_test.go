package common

import (
	"os"
	"path/filepath"
	"testing"
)

// lscpu on GB10 (spec 3.1: Cortex-X925 and Cortex-A725, Vendor ID ARM,
// Stepping r0p1, CPU(s) 20) in the gb10 scenario's layout.
const lscpuGB10 = `Architecture:             aarch64
Vendor ID:                ARM
  Model name:             Cortex-X925
    Stepping:             r0p1
    CPU max MHz:          4004.0000
  Model name:             Cortex-A725
    Stepping:             r0p1
    CPU max MHz:          2860.0000
CPU(s):                   20
NUMA node(s):             1
`

// MIDR-only /proc/cpuinfo of an arm64 kernel: no "model name" line.
const cpuinfoGB10 = "processor\t: 0\nBogoMIPS\t: 2000.00\nCPU implementer\t: 0x41\nCPU architecture: 8\nCPU variant\t: 0x0\nCPU part\t: 0xd85\nCPU revision\t: 1\n\n" +
	"processor\t: 10\nBogoMIPS\t: 2000.00\nCPU implementer\t: 0x41\nCPU architecture: 8\nCPU variant\t: 0x0\nCPU part\t: 0xd87\nCPU revision\t: 1\n"

const cpuinfoX86 = "processor\t: 0\nvendor_id\t: AuthenticAMD\nmodel name\t: AMD Ryzen 9 7950X 16-Core Processor\n"

func TestParseCPUInfoModelName(t *testing.T) {
	if got := parseCPUInfoModelName(cpuinfoX86); got != "AMD Ryzen 9 7950X 16-Core Processor" {
		t.Errorf("x86 model name = %q", got)
	}
	if got := parseCPUInfoModelName(cpuinfoGB10); got != "" {
		t.Errorf("MIDR-only cpuinfo has no model name, got %q", got)
	}
}

func TestParseLscpuModelNames(t *testing.T) {
	if got := parseLscpuModelNames(lscpuGB10); got != "Cortex-X925 / Cortex-A725" {
		t.Errorf("lscpu model names = %q", got)
	}
	// Repeated identical lines collapse; a placeholder "-" is skipped.
	if got := parseLscpuModelNames("Model name:  Neoverse-V2\nModel name:  Neoverse-V2\nModel name: -\n"); got != "Neoverse-V2" {
		t.Errorf("dedup = %q", got)
	}
	if got := parseLscpuModelNames("Architecture: x86_64\n"); got != "" {
		t.Errorf("no model lines = %q", got)
	}
}

func TestDecodeMIDR(t *testing.T) {
	if got := decodeMIDR(cpuinfoGB10); got != "Cortex-X925 / Cortex-A725" {
		t.Errorf("GB10 MIDR = %q", got)
	}
	if got := decodeMIDR("CPU implementer\t: 0x41\nCPU part\t: 0xd4f\n"); got != "Neoverse-V2" {
		t.Errorf("Neoverse V2 MIDR = %q", got)
	}
	if got := decodeMIDR("CPU implementer\t: 0x41\nCPU part\t: 0xd0c\n"); got != "ARM part 0xd0c" {
		t.Errorf("unknown part = %q", got)
	}
	if got := decodeMIDR(cpuinfoX86); got != "" {
		t.Errorf("x86 cpuinfo decoded as %q", got)
	}
}

func TestParseOSRelease(t *testing.T) {
	name, ver := parseOSRelease("PRETTY_NAME=\"Ubuntu 24.04.4 LTS\"\nNAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n")
	if name != "Ubuntu" || ver != "24.04" {
		t.Errorf("parseOSRelease = %q %q", name, ver)
	}
	if name, _ := parseOSRelease("PRETTY_NAME=\"Custom OS\"\n"); name != "Custom OS" {
		t.Errorf("PRETTY_NAME fallback = %q", name)
	}
}

// readLinuxCPUModel reads /proc/cpuinfo through NVC_SIM_ROOT and, for a
// MIDR-only file, falls back to lscpu (absent on this test host) and then MIDR.
func TestReadLinuxCPUModel_SimRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(SimRootEnv, root)
	write := func(content string) {
		p := filepath.Join(root, "proc", "cpuinfo")
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(cpuinfoX86)
	if got := readLinuxCPUModel(5); got != "AMD Ryzen 9 7950X 16-Core Processor" {
		t.Errorf("x86 = %q", got)
	}
	write(cpuinfoGB10)
	got := readLinuxCPUModel(5)
	// With lscpu on PATH the lscpu names win, otherwise the MIDR decode; both
	// name the two GB10 clusters.
	if got != "Cortex-X925 / Cortex-A725" && got == "" {
		t.Errorf("GB10 = %q", got)
	}
	// RAM from the simulated meminfo.
	p := filepath.Join(root, "proc", "meminfo")
	if err := os.WriteFile(p, []byte(meminfoGB10), 0644); err != nil {
		t.Fatal(err)
	}
	if kb := ParseMeminfo(readSimString(meminfoPath))["MemTotal"]; kb/1024 != 122572 {
		t.Errorf("MemTotal MB = %d", kb/1024)
	}
}

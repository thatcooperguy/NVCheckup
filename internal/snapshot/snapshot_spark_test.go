package snapshot

import (
	"strings"
	"testing"
)

// Two DGX Spark snapshots across the July 2026 OTA (spec 2.1 FE table): OTA
// 7.2.3 -> 7.5.0, SoC firmware 2.150.x -> 2.155.11, kernel and driver moved,
// MemTotal changed with the display-reserve toggle.
const sparkFixtureA = `{
  "metadata": {"tool_version": "0.2.2", "timestamp": "2026-06-01T10:00:00Z", "mode": "full", "platform": "linux", "schema_version": "1"},
  "system": {"os_name": "Ubuntu", "os_version": "24.04", "kernel_version": "6.14.0-1015-nvidia", "architecture": "arm64", "cpu_model": "Cortex-X925 / Cortex-A725", "ram_total_mb": 122572},
  "gpus": [{"index": 0, "name": "NVIDIA GB10", "vendor": "NVIDIA", "driver_version": "580.126.09", "is_nvidia": true, "compute_cap": "12.1", "on_package": true, "memory_reporting": "not-supported"}],
  "driver": {"version": "580.126.09", "cuda_version": "13.0"},
  "platform": {"class": "dgx-spark", "gpu_soc": "GB10", "unified_memory": true, "bios_version": "5.36_0ACUM018", "nvidia_kernel_flavour": true,
               "firmware": [{"name": "UEFI Device Firmware", "version": "2.150.3"}, {"name": "Embedded Controller", "version": "3.5.1"}]},
  "dgx_os": {"name": "DGX Spark", "ota_version": "7.2.3", "sw_build_version": "7.2.3", "serial_number": "<serial>"},
  "unified_memory": {"mem_total_kb": 125513944, "swap_total_kb": 16777212, "swappiness": 60}
}`

const sparkFixtureB = `{
  "metadata": {"tool_version": "0.2.2", "timestamp": "2026-08-01T10:00:00Z", "mode": "full", "platform": "linux", "schema_version": "1"},
  "system": {"os_name": "Ubuntu", "os_version": "24.04", "kernel_version": "6.17.0-1026-nvidia", "architecture": "arm64", "cpu_model": "Cortex-X925 / Cortex-A725", "ram_total_mb": 124620},
  "gpus": [{"index": 0, "name": "NVIDIA GB10", "vendor": "NVIDIA", "driver_version": "580.159.03", "is_nvidia": true, "compute_cap": "12.1", "on_package": true, "memory_reporting": "not-supported"}],
  "driver": {"version": "580.159.03", "cuda_version": "13.0"},
  "platform": {"class": "dgx-spark", "gpu_soc": "GB10", "unified_memory": true, "bios_version": "5.36_0ACUM023", "nvidia_kernel_flavour": true,
               "firmware": [{"name": "UEFI Device Firmware", "version": "2.155.11"}, {"name": "Embedded Controller", "version": "3.5.8"}, {"name": "USB Power Delivery Controller", "version": "0.5.22"}]},
  "dgx_os": {"name": "DGX Spark", "ota_version": "7.5.0", "ota_name": "OTA2607", "sw_build_version": "7.2.3", "serial_number": "<serial>"},
  "unified_memory": {"mem_total_kb": 127611904, "swap_total_kb": 16777212, "swappiness": 60}
}`

func TestDiff_SparkFields(t *testing.T) {
	dir := t.TempDir()
	a, err := loadSnapshot(writeFixture(t, dir, "a.json", sparkFixtureA))
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadSnapshot(writeFixture(t, dir, "b.json", sparkFixtureB))
	if err != nil {
		t.Fatal(err)
	}
	got := fieldSet(Diff(a, b))

	want := map[string][2]string{
		"kernel_version":        {"6.14.0-1015-nvidia", "6.17.0-1026-nvidia"},
		"driver_version":        {"580.126.09", "580.159.03"},
		"platform.bios_version": {"5.36_0ACUM018", "5.36_0ACUM023"},
		"platform.firmware[UEFI Device Firmware].version":          {"2.150.3", "2.155.11"},
		"platform.firmware[Embedded Controller].version":           {"3.5.1", "3.5.8"},
		"platform.firmware[USB Power Delivery Controller].version": {"", "0.5.22"},
		"dgx_os.ota_version":          {"7.2.3", "7.5.0"},
		"dgx_os.ota_name":             {"", "OTA2607"},
		"unified_memory.mem_total_kb": {"125513944", "127611904"},
	}
	for field, vals := range want {
		d, ok := got[field]
		if !ok {
			t.Errorf("missing difference %q (have %v)", field, keys(got))
			continue
		}
		if d.ValueA != vals[0] || d.ValueB != vals[1] {
			t.Errorf("%s: got %q -> %q, want %q -> %q", field, d.ValueA, d.ValueB, vals[0], vals[1])
		}
	}
	for _, unchanged := range []string{"platform.class", "platform.gpu_soc", "platform.unified_memory", "dgx_os.sw_build_version", "gpu[0].compute_cap", "gpu[0].memory_reporting", "unified_memory.swap_total_kb", "gpu[0].name"} {
		if _, ok := got[unchanged]; ok {
			t.Errorf("unchanged field %q reported", unchanged)
		}
	}
	if got["dgx_os.ota_version"].Severity != "WARN" || got["platform.firmware[UEFI Device Firmware].version"].Severity != "INFO" {
		t.Errorf("severities: ota %q firmware %q", got["dgx_os.ota_version"].Severity, got["platform.firmware[UEFI Device Firmware].version"].Severity)
	}
}

// A snapshot written before the platform fields existed has nil pointers and
// is compared on the legacy fields only.
func TestDiff_SparkFieldsNilSafe(t *testing.T) {
	dir := t.TempDir()
	a, err := loadSnapshot(writeFixture(t, dir, "a.json", fixtureA))
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadSnapshot(writeFixture(t, dir, "b.json", sparkFixtureB))
	if err != nil {
		t.Fatal(err)
	}
	got := fieldSet(Diff(a, b))
	for field := range got {
		if strings.HasPrefix(field, "platform.") || strings.HasPrefix(field, "dgx_os.") || strings.HasPrefix(field, "unified_memory.") {
			t.Errorf("platform field %q compared against a legacy snapshot", field)
		}
	}
	if d, ok := got["gpu[0].memory_reporting"]; !ok || d.ValueA != "" || d.ValueB != "not-supported" {
		t.Errorf("gpu[0].memory_reporting = %+v", d)
	}
	if fv := firmwareVersions(nil); len(fv) != 0 {
		t.Errorf("firmwareVersions(nil) = %v", fv)
	}
}

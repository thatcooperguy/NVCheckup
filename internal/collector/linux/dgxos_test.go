package linux

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// dgxReleaseFixture is the verbatim /etc/dgx-release of spec 3.1 row 4 (S104
// key list; values from the gb10 scenario).
const dgxReleaseFixture = `DGX_NAME="DGX Spark"
DGX_PRETTY_NAME="NVIDIA DGX Spark"
DGX_SWBUILD_DATE="2025-09-10-13-50-03"
DGX_SWBUILD_VERSION="7.2.3"
DGX_COMMIT_ID="833b4a7"
DGX_PLATFORM="DGX Server for KVM"
DGX_SERIAL_NUMBER="1234567890123"
DGX_OTA_VERSION="7.5.0"
DGX_OTA_DATE="Wed Jul 15 09:06:56 AM PDT 2026"
`

const fastosReleaseFixture = `NAME="DGX SPARK FASTOS"
VERSION="1.91.51"
BUILD_TYPE="customer"
`

func TestParseDGXRelease(t *testing.T) {
	var info types.DGXOSInfo
	applyDGXRelease(&info, parseDGXRelease(dgxReleaseFixture))
	want := types.DGXOSInfo{
		Name:           "DGX Spark",
		PrettyName:     "NVIDIA DGX Spark",
		SWBuildVersion: "7.2.3",
		SWBuildDate:    "2025-09-10-13-50-03",
		OTAVersion:     "7.5.0",
		OTADate:        "Wed Jul 15 09:06:56 AM PDT 2026",
		Platform:       "DGX Server for KVM",
		CommitID:       "833b4a7",
		SerialNumber:   "1234567890123",
	}
	if !reflect.DeepEqual(info, want) {
		t.Errorf("parseDGXRelease = %+v, want %+v", info, want)
	}
}

func TestParseFastOSRelease(t *testing.T) {
	if got := parseFastOSRelease(fastosReleaseFixture); got != "1.91.51" {
		t.Errorf("FastOS version = %q, want 1.91.51", got)
	}
	if got := parseFastOSRelease("NAME=\"OTHER OS\"\nVERSION=\"2.0\"\n"); got != "OTHER OS 2.0" {
		t.Errorf("foreign fastos = %q", got)
	}
}

func TestParseOTASummary(t *testing.T) {
	cases := []struct {
		out        string
		wantName   string
		wantFailed []string
	}{
		// gb10 scenario summary line
		{"detected_ota OTA2607, match 100.0%, total_checks 153, passed_checks 153, failed: []", "OTA2607", nil},
		// spec 3.2 GSP failure: OTA checker failed: ["driver"]
		{"detected_ota OTA2607, match 98.7%, total_checks 153, passed_checks 151, failed: [\"driver\"]", "OTA2607", []string{"driver"}},
		{"failed: [\"driver\", \"firmware\"]", "", []string{"driver", "firmware"}},
		{"no ota information", "", nil},
	}
	for _, c := range cases {
		name, failed := parseOTASummary(c.out)
		if name != c.wantName || !reflect.DeepEqual(failed, c.wantFailed) {
			t.Errorf("parseOTASummary(%q) = %q %v, want %q %v", c.out, name, failed, c.wantName, c.wantFailed)
		}
	}
}

func TestParseTornScore(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"torn-score: 3\n", 3, true},
		{"0", 0, true},
		{"no score", 0, false},
		// The OTA name must never be read as the score (would be 2607).
		{"OTA2607 torn-score: 0", 0, true},
		{"detected_ota OTA2607\ntorn_score 2\n", 2, true},
		{"detected_ota OTA2607\n1\n", 1, true},
		{"OTA2607", 0, false},
		{"Torn score for OTA2607: 4", 4, true},
	}
	for _, c := range cases {
		n, ok := parseTornScore(c.in)
		if ok != c.ok || n != c.want {
			t.Errorf("parseTornScore(%q) = %d %v, want %d %v", c.in, n, ok, c.want, c.ok)
		}
	}
}

// dpkgListFixture is the gb10 scenario dpkg -l list (spec 3.2 package names).
const dpkgListFixture = `Desired=Unknown/Install/Remove/Purge/Hold
||/ Name  Version  Architecture  Description
ii  nvidia-driver-580-open  580.159.03-0ubuntu0.24.04.1  arm64  NVIDIA driver (open kernel) metapackage
ii  nvidia-dkms-580-open  580.159.03-0ubuntu0.24.04.1  arm64  NVIDIA DKMS package (open kernel)
ii  nvidia-firmware-580-580.159.03  580.159.03-0ubuntu0.24.04.1  arm64  Firmware files used by the kernel module
ii  linux-modules-nvidia-580-open-6.17.0-1026-nvidia  6.17.0-1026.26+3  arm64  Linux kernel nvidia modules
ii  nvidia-persistenced  580.159.03-0ubuntu0.24.04.1  arm64  daemon to maintain GPU application state
ii  dgx-release  7.5.0  all  DGX release information
rc  nvidia-driver-570-server  570.86.15-0ubuntu1  arm64  removed, config files remain
`

func TestDgxPackageFactsFromDpkgList(t *testing.T) {
	pkgs := parseDpkgList(dpkgListFixture)
	if len(pkgs) != 7 {
		t.Fatalf("parsed %d packages, want 7: %+v", len(pkgs), pkgs)
	}
	driver, firmware, modules := dgxPackageFacts(pkgs, "6.17.0-1026-nvidia")
	if driver != "580.159.03-0ubuntu0.24.04.1" {
		t.Errorf("driver pkg = %q", driver)
	}
	if firmware != "580.159.03-0ubuntu0.24.04.1" {
		t.Errorf("firmware pkg = %q", firmware)
	}
	if !modules {
		t.Error("modules for the running kernel should be present")
	}
	if _, _, modules := dgxPackageFacts(pkgs, "6.17.0-1031-nvidia"); modules {
		t.Error("modules for a different kernel must not count")
	}
}

func TestDgxPackageFactsFromDpkgQuery(t *testing.T) {
	out := "nvidia-driver-580-open\t580.159.03-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-firmware-580-580.159.03\t580.159.03-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-driver-570-server\t570.86.15-0ubuntu1\tdeinstall ok config-files\n" +
		"malformed line without tabs\n"
	pkgs := parseDpkgQuery(out)
	if len(pkgs) != 3 {
		t.Fatalf("parsed %d rows, want 3", len(pkgs))
	}
	if pkgs[2].Installed {
		t.Error("config-files state must not count as installed")
	}
	driver, firmware, _ := dgxPackageFacts(pkgs, "")
	if driver != "580.159.03-0ubuntu0.24.04.1" || firmware != "580.159.03-0ubuntu0.24.04.1" {
		t.Errorf("driver/firmware = %q/%q", driver, firmware)
	}
	// Only a foreign -server driver installed: still reported as the driver version.
	only := []dpkgPackage{{Name: "nvidia-driver-570-server", Version: "570.86.15", Installed: true}}
	if d, _, _ := dgxPackageFacts(only, ""); d != "570.86.15" {
		t.Errorf("fallback driver = %q", d)
	}
}

// TestDgxPackageFactsLeftoverFirmware: after an OTA the previous versioned
// firmware package stays installed until autoremove and sorts first in
// dpkg-query output; the firmware version must still pair with the driver so
// dgx-spark-ota-torn does not fire on a healthy machine.
func TestDgxPackageFactsLeftoverFirmware(t *testing.T) {
	out := "nvidia-driver-580-open\t580.159.03-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-firmware-580-580.126.09\t580.126.09-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-firmware-580-580.159.03\t580.159.03-0ubuntu0.24.04.1\tinstall ok installed\n"
	driver, firmware, _ := dgxPackageFacts(parseDpkgQuery(out), "")
	if driver != firmware || firmware != "580.159.03-0ubuntu0.24.04.1" {
		t.Errorf("driver/firmware = %q/%q, want the paired 580.159.03 row", driver, firmware)
	}
	// Without a driver row the highest firmware wins (dpkg ordering, not row order).
	noDriver := "nvidia-firmware-580-580.173.02\t580.173.02-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-firmware-580-580.95.05\t580.95.05-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-firmware-580-580.126.09\t580.126.09-0ubuntu0.24.04.1\tinstall ok installed\n"
	if _, fw, _ := dgxPackageFacts(parseDpkgQuery(noDriver), ""); fw != "580.173.02-0ubuntu0.24.04.1" {
		t.Errorf("highest firmware = %q", fw)
	}
	// A driver newer than every firmware (plain apt upgrade, the torn case)
	// still reports the highest firmware so the mismatch is visible.
	torn := "nvidia-driver-580-open\t580.173.02-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-firmware-580-580.126.09\t580.126.09-0ubuntu0.24.04.1\tinstall ok installed\n" +
		"nvidia-firmware-580-580.159.03\t580.159.03-0ubuntu0.24.04.1\tinstall ok installed\n"
	if d, fw, _ := dgxPackageFacts(parseDpkgQuery(torn), ""); d == fw || fw != "580.159.03-0ubuntu0.24.04.1" {
		t.Errorf("torn driver/firmware = %q/%q", d, fw)
	}
}

func TestCompareDebVersion(t *testing.T) {
	less := [][2]string{
		{"580.126.09-0ubuntu0.24.04.1", "580.159.03-0ubuntu0.24.04.1"},
		{"580.95.05", "580.126.09"},
		{"580.159.03-0ubuntu0.24.04.1", "580.159.03-0ubuntu0.24.04.2"},
		{"1.0~rc1", "1.0"},
		{"1.0", "1:0.1"},
		{"2.9.0", "2.9.0+cu130"},
		{"6.17.0-1026.26+3", "6.17.0-1031.31"},
	}
	for _, c := range less {
		if compareDebVersion(c[0], c[1]) >= 0 {
			t.Errorf("compareDebVersion(%q, %q) should be < 0", c[0], c[1])
		}
		if compareDebVersion(c[1], c[0]) <= 0 {
			t.Errorf("compareDebVersion(%q, %q) should be > 0", c[1], c[0])
		}
	}
	if compareDebVersion("580.159.03-0ubuntu0.24.04.1", "580.159.03-0ubuntu0.24.04.1") != 0 {
		t.Error("equal versions must compare 0")
	}
}

// procNetTCPFixture: 127.0.0.1:11000 (0x2AF8) listening, sshd on 22, an
// established connection on 8000 (must not count) and an IPv6 listener on
// 30000 (0x7530).
const procNetTCPFixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:2AF8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000000000000000 100 0 0 10 0
   1: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10001 1 0000000000000000 100 0 0 10 0
   2: 0100007F:1F40 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 22222 1 0000000000000000 20 4 30 10 -1
`

const procNetTCP6Fixture = `  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 00000000000000000000000000000000:7530 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 33333 1 0000000000000000 100 0 0 10 0
`

func TestProcNetTCPListening(t *testing.T) {
	got := ProcNetTCPListening(procNetTCPFixture)
	if want := []int{22, 11000}; !reflect.DeepEqual(got, want) {
		t.Errorf("listening ports = %v, want %v", got, want)
	}
	if got := ProcNetTCPListening(procNetTCP6Fixture); !reflect.DeepEqual(got, []int{30000}) {
		t.Errorf("tcp6 listening = %v, want [30000]", got)
	}
}

func TestUnitStateAndFwupdError(t *testing.T) {
	if got := fwupdErrorFromOutput("", "WARNING: libfwupd version 1.9.34 does not match daemon 1.9.30\nother"); got != "libfwupd version 1.9.34 does not match daemon 1.9.30" {
		t.Errorf("fwupd mismatch = %q", got)
	}
	if got := fwupdErrorFromOutput("", "Failed to connect to daemon: timeout"); got != "Failed to connect to daemon: timeout" {
		t.Errorf("fwupd generic error = %q", got)
	}
}

func TestAptSourceFirstLine(t *testing.T) {
	cases := []struct {
		content string
		ok      bool
	}{
		{"deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://nvidia.github.io/libnvidia-container/stable/deb/$(ARCH) /\n", true},
		{"# comment\ndeb https://example /\n", true},
		{"Types: deb\nURIs: https://example\n", true},
		{"", true},
		{"nvidia.github.io/libnvidia-container/stable/deb/$(ARCH) /\n", false},
		{"<html>\n", false},
	}
	for _, c := range cases {
		if ok, _ := aptSourceFirstLineOK(c.content); ok != c.ok {
			t.Errorf("aptSourceFirstLineOK(%q) = %v, want %v", c.content, ok, c.ok)
		}
	}
}

// fwupdDevicesFixture mirrors the gb10 scenario's fwupd_devices in the tree
// layout fwupdmgr get-devices prints, with the hex version form.
const fwupdDevicesFixture = `NVIDIA DGX Spark
│
├─Embedded Controller:
│     Device ID:          8b1e3d
│     Current version:    0x03000508
│     Vendor:             NVIDIA (DMI:NVIDIA)
│     GUID:               3d13c989-e6a8-4ead-95ee-921f09868f65 ← EC
│     Device Flags:       • Internal device
│                         • Updatable
│
├─UEFI Device Firmware:
│     Device ID:          77aa11
│     Current version:    0x02009b0b
│     GUID:               b488217b-3895-4fc0-b1bf-ab7005a2d45a
│     Update State:       Needs reboot
│
└─USB Power Delivery Controller:
      Device ID:          9c0f21
      Current version:    0x00000516
      GUID:               dd1a238a-5f8e-46bd-9401-a88da99c5a96
`

func TestParseFwupdDevices(t *testing.T) {
	comps := parseFwupdDevices(fwupdDevicesFixture)
	want := []types.FirmwareComponent{
		{Name: "Embedded Controller", GUID: "3d13c989-e6a8-4ead-95ee-921f09868f65", Version: "3.5.8"},
		{Name: "UEFI Device Firmware", GUID: "b488217b-3895-4fc0-b1bf-ab7005a2d45a", Version: "2.155.11", Pending: "Needs reboot"},
		{Name: "USB Power Delivery Controller", GUID: "dd1a238a-5f8e-46bd-9401-a88da99c5a96", Version: "0.5.22"},
	}
	if !reflect.DeepEqual(comps, want) {
		t.Errorf("parseFwupdDevices = %+v, want %+v", comps, want)
	}
}

// fwupdUpgradesFixture follows fwupdmgr's tree layout: the bullet list of
// current devices, then one device block with two release blocks (newest
// first). ASSUMPTION: layout pending a GB10 capture (spec section 12).
const fwupdUpgradesFixture = `Devices with the latest available firmware version:
 • Embedded Controller
 • USB-PD Controller
DGX Spark
│
└─System Firmware:
  │   Device ID:          1a2b3c4d5e6f
  │   Summary:            UEFI System Firmware
  │   Current version:    0x02009b0b
  │   Minimum Version:    0x02000000
  │   Vendor:             NVIDIA (DMI:NVIDIA)
  │   GUIDs:              230c8b18-8d9b-53ec-838b-6cfc0383493a
  │
  ├─UEFI Firmware Update:
  │     New version:      0x0200a001
  │     Remote ID:        lvfs
  │     Summary:          UEFI System Firmware update
  │     Vendor:           NVIDIA
  │
  └─UEFI Firmware Update:
        New version:      0x02009c00
        Remote ID:        lvfs
`

func TestParseFwupdUpgradesAndApplyPending(t *testing.T) {
	pending := parseFwupdUpgrades(fwupdUpgradesFixture)
	if want := map[string]string{"System Firmware": "2.160.1"}; !reflect.DeepEqual(pending, want) {
		t.Errorf("parseFwupdUpgrades = %v, want %v", pending, want)
	}
	if got := parseFwupdUpgrades("No updates available\n"); len(got) != 0 {
		t.Errorf("no-upgrades output must yield nothing, got %v", got)
	}
	comps := []types.FirmwareComponent{
		{Name: "Embedded Controller", Version: "3.5.8"},
		{Name: "System Firmware", Version: "2.155.11"},
	}
	comps = applyPendingFirmware(comps, map[string]string{"System Firmware": "2.160.1", "USB-PD Controller": "0.5.22"})
	if len(comps) != 3 || comps[1].Pending != "2.160.1" || comps[0].Pending != "" {
		t.Errorf("applyPendingFirmware = %+v", comps)
	}
	if comps[2].Name != "USB-PD Controller" || comps[2].Pending != "0.5.22" || comps[2].Version != "" {
		t.Errorf("device only in get-upgrades = %+v", comps[2])
	}
}

func TestNormalizeFirmwareVersion(t *testing.T) {
	cases := map[string]string{
		"0x03000508": "3.5.8",    // EC 3.5.8 (spec 2.1 FE table)
		"0x02009b0b": "2.155.11", // SoC 2.155.11
		"0x00000516": "0.5.22",   // USB PD 0.5.22
		"3.5.8":      "3.5.8",
		"1.110.13":   "1.110.13",
		"":           "",
	}
	for in, want := range cases {
		if got := normalizeFirmwareVersion(in); got != want {
			t.Errorf("normalizeFirmwareVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseBootListAndClassify(t *testing.T) {
	list := `IDX BOOT ID                          FIRST ENTRY                 LAST ENTRY
 -2 1f2e3d4c5b6a79880000000000000002 Mon 2026-08-24 08:00:01 PDT Mon 2026-08-24 22:10:00 PDT
 -1 1f2e3d4c5b6a79880000000000000001 Tue 2026-09-01 08:00:01 PDT Tue 2026-09-01 18:00:00 PDT
  0 1f2e3d4c5b6a79880000000000000000 Wed 2026-09-02 07:59:59 PDT Wed 2026-09-02 09:00:00 PDT
`
	boots := parseBootList(list)
	if len(boots) != 2 || boots[0].Index != -1 || boots[1].Index != -2 {
		t.Fatalf("parseBootList = %+v", boots)
	}
	if boots[0].LastDay != time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("boot -1 last day = %v", boots[0].LastDay)
	}

	// -o cat output (message only, no hostname) is what the collector reads.
	b := classifyBootTail("Reached target Power-Off.\nJournal stopped\n")
	if !b.Clean || b.LoggedFailure || b.Logless() || b.LastLine != "Journal stopped" {
		t.Errorf("clean tail: %+v", b)
	}
	// The default short format still classifies (hostname present).
	b = classifyBootTail("Sep 01 17:59:58 spark systemd[1]: Reached target Power-Off.\nSep 01 17:59:59 spark systemd-journald[300]: Journal stopped\n")
	if !b.Clean || b.LastLine != "Sep 01 17:59:59 spark systemd-journald[300]: Journal stopped" {
		t.Errorf("clean short tail: %+v", b)
	}
	// A log-less power-off: ordinary lines, then nothing.
	b = classifyBootTail("Started GNOME Display Manager.\nnvidia-persistenced: Started (1234)\n")
	if b.Clean || b.LoggedFailure || !b.Logless() {
		t.Errorf("logless tail: %+v", b)
	}
	// Boots that ended in a logged failure are unclean but not log-less and
	// must not count towards UncleanBoots (rule gb10-logless-hard-poweroff:
	// "AND no panic/OOM/Xid/thermal lines").
	for _, tail := range []string{
		"NVRM: Xid (PCI:000f:01:00): 119, Timeout after 6s of waiting for RPC response from GPU0 GSP!\n",
		"Kernel panic - not syncing: Fatal exception\n",
		"Out of memory: Killed process 4242 (python3) total-vm:120000000kB\n",
		"thermal thermal_zone0: critical temperature reached (105 C), shutting down\n",
	} {
		b = classifyBootTail(tail)
		if b.Clean || !b.LoggedFailure || b.Logless() {
			t.Errorf("logged-failure tail %q: %+v", tail, b)
		}
	}
}

func TestParseGDMSleepPolicyAndSuspendMarkers(t *testing.T) {
	content := "# sleep-inactive-ac-type='suspend'\n[org/gnome/settings-daemon/plugins/power]\nsleep-inactive-ac-type='nothing'\n"
	if got := parseGDMSleepPolicy(content); got != "nothing" {
		t.Errorf("gdm sleep policy = %q, want nothing", got)
	}
	if got := parseGDMSleepPolicy("# sleep-inactive-ac-type='suspend'\n"); got != "" {
		t.Errorf("commented policy = %q, want empty", got)
	}
	attempts, failed := parseSuspendMarkers("PM: suspend entry (s2idle)\nPM: suspend exit\nPM: suspend entry (s2idle)\nNVRM: nv_set_system_power_state: failed to enter power state\n")
	if attempts != 2 || !failed {
		t.Errorf("suspend markers = %d %v, want 2 true", attempts, failed)
	}
}

// writeFixture creates a file under root, creating parents.
func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCollectDGXOSFromSimRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(simRootEnv, root)
	writeFixture(t, root, "etc/dgx-release", dgxReleaseFixture)
	writeFixture(t, root, "etc/fastos-release", fastosReleaseFixture)
	writeFixture(t, root, "proc/net/tcp", procNetTCPFixture)
	writeFixture(t, root, "etc/apt/sources.list.d/nvidia-container-toolkit.list", "garbage first line\n")
	writeFixture(t, root, "proc/sys/kernel/osrelease", "6.17.0-1026-nvidia\n")
	writeFixture(t, root, "lib/modules/6.17.0-1026-nvidia/kernel/nvidia-580/nvidia.ko.zst", "")

	info, _ := CollectDGXOS(5)
	if info.Name != "DGX Spark" || info.OTAVersion != "7.5.0" || info.SerialNumber != "1234567890123" {
		t.Errorf("dgx-release fields not read through NVC_SIM_ROOT: %+v", info)
	}
	if info.FastOSVersion != "1.91.51" {
		t.Errorf("FastOSVersion = %q", info.FastOSVersion)
	}
	if !info.DashboardPortOpen {
		t.Error("port 11000 listener in fixture /proc/net/tcp must set DashboardPortOpen")
	}
	if !strings.HasPrefix(info.AptSourceCorrupt, "nvidia-container-toolkit.list: garbage") {
		t.Errorf("AptSourceCorrupt = %q", info.AptSourceCorrupt)
	}
	if nvidiaModulesOnDisk("6.17.0-1026-nvidia") != true {
		t.Error("module file under the sim root should count as modules-for-kernel")
	}
	if got := runningKernel(5); got != "6.17.0-1026-nvidia" {
		t.Errorf("runningKernel = %q", got)
	}
}

func TestCollectDGXHostStateFromSimRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(simRootEnv, root)
	writeFixture(t, root, "sys/class/thermal/thermal_zone0/type", "acpitz\n")
	writeFixture(t, root, "sys/class/thermal/thermal_zone0/temp", "93000\n")
	writeFixture(t, root, "sys/class/thermal/thermal_zone1/type", "cpu-thermal\n")
	writeFixture(t, root, "sys/class/thermal/thermal_zone1/temp", "50000\n")
	writeFixture(t, root, "etc/gdm3/greeter.dconf-defaults", "sleep-inactive-ac-type='nothing'\n")
	if err := os.MkdirAll(filepath.Join(root, "sys", "fs", "pstore"), 0o755); err != nil {
		t.Fatal(err)
	}

	var p types.PlatformInfo
	CollectDGXHostState(5, &p)
	if want := map[string]int{"thermal_zone0": 93000}; !reflect.DeepEqual(p.ACPIThermalMC, want) {
		t.Errorf("ACPIThermalMC = %v, want %v", p.ACPIThermalMC, want)
	}
	if p.PstoreEmpty == nil || !*p.PstoreEmpty {
		t.Errorf("PstoreEmpty = %v, want true", p.PstoreEmpty)
	}
	if p.GDMSleepPolicy != "nothing" {
		t.Errorf("GDMSleepPolicy = %q", p.GDMSleepPolicy)
	}
	if p.ClockCapUnit != "" {
		t.Errorf("ClockCapUnit = %q without a unit file", p.ClockCapUnit)
	}

	// An installed but stopped clock-cap unit is still reported (rule
	// gb10-clock-cap-active: "gb10-clock-cap.service exists").
	writeFixture(t, root, "etc/systemd/system/gb10-clock-cap.service", "[Service]\nExecStart=/usr/bin/nvidia-smi -lgc 300,2200\n")
	var q types.PlatformInfo
	CollectDGXHostState(5, &q)
	if q.ClockCapUnit != "gb10-clock-cap.service (inactive)" {
		t.Errorf("ClockCapUnit = %q, want the unit marked inactive", q.ClockCapUnit)
	}
}

func TestSimPath(t *testing.T) {
	t.Setenv(simRootEnv, "")
	if got := simPath("/etc/dgx-release"); got != "/etc/dgx-release" {
		t.Errorf("simPath unset = %q", got)
	}
	t.Setenv(simRootEnv, "/tmp/sim/")
	if got := simPath("/etc/dgx-release"); got != "/tmp/sim/etc/dgx-release" {
		t.Errorf("simPath set = %q", got)
	}
}

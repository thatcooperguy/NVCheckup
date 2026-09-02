package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// collectLinuxForTest runs the Linux file readers (not the nvidia-smi process
// count, which CollectUnifiedMemory adds) against the current NVC_SIM_ROOT.
func collectLinuxForTest(t *testing.T) (types.UnifiedMemoryInfo, []types.CollectorError) {
	t.Helper()
	var info types.UnifiedMemoryInfo
	var errs []types.CollectorError
	collectUnifiedMemoryLinux(&info, &errs, 5)
	return info, errs
}

// /proc/meminfo of the gb10 scenario (MemTotal 125,513,944 kB = 119.7 GiB,
// spec 2.1) in the kernel's own layout.
const meminfoGB10 = `MemTotal:       125513944 kB
MemFree:        56076940 kB
MemAvailable:   121519624 kB
Buffers:         1048576 kB
Cached:         62914560 kB
SwapCached:            0 kB
SwapTotal:      16777212 kB
SwapFree:       16777212 kB
HugePages_Total:       0
HugePages_Free:        0
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:       2048 kB
`

func TestParseMeminfo_GB10(t *testing.T) {
	m := ParseMeminfo(meminfoGB10)
	if m["MemTotal"] != 125513944 || m["MemAvailable"] != 121519624 || m["SwapFree"] != 16777212 || m["Hugepagesize"] != 2048 || m["HugePages_Total"] != 0 {
		t.Errorf("ParseMeminfo = %v", m)
	}
	if _, ok := m["HugePages_Total"]; !ok {
		t.Error("count fields without a unit must still be parsed")
	}
}

func TestAllocatableKB_Spec33(t *testing.T) {
	// allocatable = MemAvailable + SwapFree
	if got := AllocatableKB(121519624, 16777212, 0, 0, 2048); got != 121519624+16777212 {
		t.Errorf("no hugepages: %d", got)
	}
	// HugePages_Total != 0: allocatable = HugePages_Free * Hugepagesize, swap counts 0
	if got := AllocatableKB(121519624, 16777212, 1000, 750, 2048); got != 750*2048 {
		t.Errorf("hugepages: %d", got)
	}
}

func TestParseSwaps(t *testing.T) {
	out := "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/swapfile                               file\t\t16777212\t0\t\t-2\n/dev/zram0                              partition\t8388604\t1024\t\t100\n"
	devs := parseSwaps(out)
	if len(devs) != 2 || devs[0] != "/swapfile" || devs[1] != "/dev/zram0" {
		t.Errorf("parseSwaps = %v", devs)
	}
	if devs := parseSwaps("Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n"); len(devs) != 0 {
		t.Errorf("header-only file gave %v", devs)
	}
}

func TestParsePSI(t *testing.T) {
	some, full := parsePSI("some avg10=0.35 avg60=0.10 avg300=0.02 total=123456\nfull avg10=1.25 avg60=0.40 avg300=0.05 total=6789\n")
	if some != 0.35 || full != 1.25 {
		t.Errorf("parsePSI = %v %v", some, full)
	}
	if some, full := parsePSI("garbage\n"); some != 0 || full != 0 {
		t.Errorf("garbage parsed as %v %v", some, full)
	}
}

func TestParseVmstatField(t *testing.T) {
	if n, ok := parseVmstatField("nr_free_pages 14019235\npswpin 4242\npswpout 99\n", "pswpin"); !ok || n != 4242 {
		t.Errorf("pswpin = %d %v", n, ok)
	}
	if _, ok := parseVmstatField("pswpout 99\n", "pswpin"); ok {
		t.Error("missing field must report !ok")
	}
}

func TestCountComputeAppRows(t *testing.T) {
	if n := countComputeAppRows("1234\n5678\n"); n != 2 {
		t.Errorf("two pids counted as %d", n)
	}
	if n := countComputeAppRows("1234, python\n"); n != 1 {
		t.Errorf("row with extra columns counted as %d", n)
	}
	for _, out := range []string{"", "Not Supported\n", "[N/A]\n", "No devices were found\n"} {
		if n := countComputeAppRows(out); n != 0 {
			t.Errorf("%q counted as %d", out, n)
		}
	}
}

func TestCountMemoryEvents(t *testing.T) {
	log := "[ 1000.1] Out of memory: Killed process 4242 (python3) total-vm:123kB, anon-rss:1kB\n" +
		"[ 1000.2] oom_reaper: reaped process 4242 (python3)\n" +
		"[ 2000.0] NVRM: nvCheckOkFailedNoLog: Check failed: Out of memory [NV_ERR_NO_MEMORY] (0x00000051) returned from ...\n" +
		"[ 2000.1] Out of memory: Killed process 4300 (ollama) ...\n" +
		"[ 3000.0] NVRM: loading NVIDIA Open GPU Kernel Module for aarch64  580.159.03  Release Build\n"
	oom, nvrm := CountMemoryEvents(log)
	if oom != 2 || nvrm != 1 {
		t.Errorf("CountMemoryEvents = %d %d", oom, nvrm)
	}
	if oom, nvrm := CountMemoryEvents(""); oom != 0 || nvrm != 0 {
		t.Error("empty log counted events")
	}
}

func TestParseWindowsMemory(t *testing.T) {
	total, free := parseWindowsMemory("total_kb=133693440\r\nfree_kb=98765432\r\n")
	if total != 133693440 || free != 98765432 {
		t.Errorf("parseWindowsMemory = %d %d", total, free)
	}
}

// The Linux collector against a fixture tree under NVC_SIM_ROOT: every field
// of the gb10 scenario lands, the allocatable figure follows spec 3.3, and a
// missing PSI file yields a note rather than a failure.
func TestCollectUnifiedMemoryLinux_SimRoot(t *testing.T) {
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
	mk(meminfoPath, meminfoGB10)
	mk(swapsPath, "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/swapfile                               file\t\t16777212\t0\t\t-2\n")
	mk(swappinessPath, "60\n")
	mk(vmstatPath, "pswpin 100\npswpout 5\n")

	infoOut, notes := collectLinuxForTest(t)
	if infoOut.MemTotalKB != 125513944 || infoOut.MemAvailableKB != 121519624 || infoOut.SwapTotalKB != 16777212 || infoOut.CachedKB != 62914560 {
		t.Errorf("meminfo fields = %+v", infoOut)
	}
	if infoOut.AllocatableKB != 121519624+16777212 {
		t.Errorf("AllocatableKB = %d", infoOut.AllocatableKB)
	}
	if len(infoOut.SwapDevices) != 1 || infoOut.SwapDevices[0] != "/swapfile" || infoOut.Swappiness != 60 {
		t.Errorf("swap fields = %+v", infoOut)
	}
	if infoOut.Pswpin != 100 || infoOut.PswpinDelta != 0 {
		t.Errorf("pswpin = %d delta %d", infoOut.Pswpin, infoOut.PswpinDelta)
	}
	// PSI absent (CONFIG_PSI off) is a note, not a failure.
	found := false
	for _, e := range notes {
		if e.Collector == "unified_memory.psi" && !e.Fatal {
			found = true
		}
		if e.Fatal {
			t.Errorf("unexpected fatal error %+v", e)
		}
	}
	if !found {
		t.Errorf("missing PSI file should produce a unified_memory.psi note, got %+v", notes)
	}

	// With PSI present the averages are read.
	mk(psiMemoryPath, "some avg10=0.35 avg60=0.10 avg300=0.02 total=1\nfull avg10=1.25 avg60=0.40 avg300=0.05 total=2\n")
	infoOut, _ = collectLinuxForTest(t)
	if infoOut.PSISomeAvg10 != 0.35 || infoOut.PSIFullAvg10 != 1.25 {
		t.Errorf("PSI = %v %v", infoOut.PSISomeAvg10, infoOut.PSIFullAvg10)
	}

	// An empty root: every reader degrades to a note, nothing panics.
	t.Setenv(SimRootEnv, t.TempDir())
	infoOut, notes = collectLinuxForTest(t)
	if infoOut.MemTotalKB != 0 || len(notes) == 0 {
		t.Errorf("empty root: info %+v notes %d", infoOut, len(notes))
	}
	for _, e := range notes {
		if !strings.HasPrefix(e.Collector, "unified_memory.") {
			t.Errorf("note from unexpected collector %+v", e)
		}
	}
}

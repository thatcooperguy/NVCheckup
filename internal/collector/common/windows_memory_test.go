package common

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/util"
)

func TestCollectWindowsHostMemoryReadOnlyProjection(t *testing.T) {
	calls := 0
	m, err := collectWindowsHostMemory(7, func(timeout int, name string, args ...string) util.CommandResult {
		calls++
		if timeout != 7 || name != "powershell" || !reflect.DeepEqual(args, []string{"-NoProfile", "-NonInteractive", "-Command", windowsHostMemoryScript}) {
			t.Fatalf("unexpected invocation: %d %s %v", timeout, name, args)
		}
		// Pin the approved reads and exclude virtual-memory estimates and
		// configuration/service actions from the shared collector.
		if strings.Count(windowsHostMemoryScript, "Get-CimInstance ") != 2 {
			t.Fatal("expected exactly two CIM reads")
		}
		for _, want := range []string{"Win32_OperatingSystem", "Win32_PerfFormattedData_PerfOS_Memory", "$perf.CommitLimit", "$perf.CommittedBytes", "catch {}"} {
			if !strings.Contains(windowsHostMemoryScript, want) {
				t.Errorf("projection missing %s", want)
			}
		}
		for _, bad := range []string{"FreeVirtualMemory", "TotalVirtualMemorySize", "Set-", "Remove-", "Start-", "Stop-", "Invoke-", "Download"} {
			if strings.Contains(windowsHostMemoryScript, bad) {
				t.Errorf("unexpected operation: %s", bad)
			}
		}
		return util.CommandResult{Stdout: `{"TotalVisibleMemorySize":67108864,"FreePhysicalMemory":33554432,"CommitLimit":85899345920,"CommittedBytes":84825604096}`}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := time.Parse(time.RFC3339, m.CapturedAt); err != nil {
		t.Fatalf("snapshot time missing: %v", err)
	}
	free, known := m.CommitHeadroomBytes()
	if calls != 1 || !known || free != 1024*1024*1024 || !m.LowCommitHeadroom() || *m.PhysicalFreeKB != 33554432 || *m.PhysicalTotalKB != 67108864 {
		t.Fatalf("bad snapshot: %+v", m)
	}
}

func TestParseWindowsHostMemoryMissingAndZero(t *testing.T) {
	for _, text := range []string{`{}`, `{"CommitLimit":null,"CommittedBytes":null}`, `{"TotalVisibleMemorySize":67108864,"FreePhysicalMemory":0}`} {
		m, err := parseWindowsHostMemory(text)
		if err != nil {
			t.Fatal(err)
		}
		if _, known := m.CommitHeadroomBytes(); known || m.LowCommitHeadroom() {
			t.Fatalf("unknown treated as known: %s", text)
		}
		if strings.Contains(text, "FreePhysicalMemory") && (m.PhysicalFreeKB == nil || *m.PhysicalFreeKB != 0) {
			t.Fatal("physical zero lost")
		}
	}
	m, err := parseWindowsHostMemory(`{"CommitLimit":85899345920,"CommittedBytes":0}`)
	if err != nil {
		t.Fatal(err)
	}
	if free, known := m.CommitHeadroomBytes(); !known || free != 85899345920 {
		t.Fatal("committed zero lost")
	}
	for _, text := range []string{`garbage`, `{"CommittedBytes":-1}`, `{"CommittedBytes":1.25}`} {
		if _, err := parseWindowsHostMemory(text); err == nil {
			t.Errorf("accepted invalid JSON counter: %s", text)
		}
	}
}

func TestCollectWindowsHostMemoryCommandFailure(t *testing.T) {
	_, err := collectWindowsHostMemory(1, func(int, string, ...string) util.CommandResult {
		return util.CommandResult{Err: errors.New("timed out"), Stdout: `{}`}
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("failure lost: %v", err)
	}
}

// Explicitly opt in to a live, read-only Windows check. Normal CI uses only
// the injected runner/parser fixtures above and never queries the host here.
func TestCollectWindowsHostMemoryLive(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("NVC_TEST_LIVE_WINDOWS_MEMORY") != "1" {
		t.Skip("set NVC_TEST_LIVE_WINDOWS_MEMORY=1 on Windows for a live CIM snapshot")
	}
	m, err := CollectWindowsHostMemory(15)
	if err != nil {
		t.Fatal(err)
	}
	if m.PhysicalTotalKB == nil || *m.PhysicalTotalKB == 0 {
		t.Fatal("physical memory query unavailable")
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("live Windows snapshot: %s", data)
	if _, known := m.CommitHeadroomBytes(); !known {
		t.Log("system commit counters unavailable; diagnostic remains unknown")
	}
}

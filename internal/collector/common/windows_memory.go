package common

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// CommitLimit/CommittedBytes are the system performance counters, in bytes.
// Win32_OperatingSystem.FreeVirtualMemory is not substituted for either one.
const windowsHostMemoryScript = `$ErrorActionPreference = 'Stop'; ` +
	`$os = Get-CimInstance Win32_OperatingSystem; ` +
	`$perf = $null; try { $perf = Get-CimInstance Win32_PerfFormattedData_PerfOS_Memory } catch {}; ` +
	`[pscustomobject]@{TotalVisibleMemorySize=$os.TotalVisibleMemorySize; FreePhysicalMemory=$os.FreePhysicalMemory; ` +
	`CommitLimit=$perf.CommitLimit; CommittedBytes=$perf.CommittedBytes} | ConvertTo-Json -Compress`

const windowsHostMemorySource = "Win32_OperatingSystem (physical kB); Win32_PerfFormattedData_PerfOS_Memory (system commit bytes)"

// CollectWindowsHostMemory is one bounded read-only command with two CIM reads.
// It performs no configuration, pagefile changes, service actions or downloads.
func CollectWindowsHostMemory(timeout int) (*types.WindowsMemoryInfo, error) {
	return collectWindowsHostMemory(timeout, util.RunCommand)
}

func collectWindowsHostMemory(timeout int, run func(int, string, ...string) util.CommandResult) (*types.WindowsMemoryInfo, error) {
	r := run(timeout, "powershell", "-NoProfile", "-NonInteractive", "-Command", windowsHostMemoryScript)
	if r.Err != nil {
		return nil, fmt.Errorf("Windows memory CIM query failed: %w", r.Err)
	}
	m, err := parseWindowsHostMemory(r.Stdout)
	if err == nil {
		m.CapturedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return m, err
}

func parseWindowsHostMemory(text string) (*types.WindowsMemoryInfo, error) {
	var raw struct {
		PhysicalTotal *uint64 `json:"TotalVisibleMemorySize"`
		PhysicalFree  *uint64 `json:"FreePhysicalMemory"`
		CommitLimit   *uint64 `json:"CommitLimit"`
		Committed     *uint64 `json:"CommittedBytes"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("Windows memory CIM JSON: %w", err)
	}
	return &types.WindowsMemoryInfo{
		PhysicalTotalKB: raw.PhysicalTotal, PhysicalFreeKB: raw.PhysicalFree,
		CommitLimitBytes: raw.CommitLimit, CommittedBytes: raw.Committed,
		Source: windowsHostMemorySource,
	}, nil
}

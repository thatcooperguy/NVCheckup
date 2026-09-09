package llmplan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func windowsSnapshot(usedGiB uint64) *types.WindowsMemoryInfo {
	total, free, limit, used := uint64(64*1024*1024), uint64(32*1024*1024), uint64(80*1024*1024*1024), usedGiB*1024*1024*1024
	return &types.WindowsMemoryInfo{PhysicalTotalKB: &total, PhysicalFreeKB: &free, CommitLimitBytes: &limit, CommittedBytes: &used, Source: "synthetic system commit counters"}
}

func TestWindowsCommitWarningDoesNotChangeGPUFit(t *testing.T) {
	r := rtx3090Report()
	o := DefaultOptions()
	o.GOOS, o.Model, o.Runtime, o.Offline = "windows", "llama-3.1-8b-instruct", "llamacpp", true
	build := func(memory *types.WindowsMemoryInfo) *Plan {
		t.Helper()
		r.System.WindowsMemory = memory
		pool, _ := DerivePool(r, o.GOOS, 1, 0, true)
		if !pool.Discrete || pool.TotalBytes != 24*GiB || pool.AvailableBytes != 23000*1024*1024 || pool.AllocatableBytes != pool.AvailableBytes || pool.SwapTotalBytes != 0 {
			t.Fatalf("host commit contaminated VRAM: %+v", pool)
		}
		p, err := Build(r, pool, nil, false, o)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	baseline := build(windowsSnapshot(40))
	limited := build(windowsSnapshot(79))
	if !reflect.DeepEqual(baseline.Fit, limited.Fit) || !reflect.DeepEqual(baseline.Memory, limited.Memory) || !reflect.DeepEqual(baseline.Advice, limited.Advice) || !limited.Fit.FitsTotal {
		t.Fatal("host warning changed model sizing or advice")
	}
	if strings.Contains(strings.Join(baseline.Warnings, " "), "commit headroom is low") {
		t.Fatal("unexpected headroom warning")
	}
	for _, output := range []string{RenderText(limited), RenderMarkdown(limited)} {
		for _, want := range []string{"79.0 GiB committed / 80.0 GiB limit", "headroom 1.0 GiB", "physical RAM free 32.0 GiB", "heuristic", "GPU fit does not establish host readiness", "1-2 jobs"} {
			if !strings.Contains(output, want) {
				t.Errorf("missing %q", want)
			}
		}
	}
	js, err := RenderJSON(limited)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Plan
	if err := json.Unmarshal([]byte(js), &decoded); err != nil {
		t.Fatal(err)
	}
	if headroom, known := decoded.HostWindowsMemory.CommitHeadroomBytes(); !known || headroom != uint64(GiB) {
		t.Fatal("JSON diagnostic lost")
	}
	if !strings.Contains(strings.Join(decoded.Warnings, " "), "commit headroom is low") {
		t.Fatal("JSON warning missing")
	}
}

func TestWindowsSavedReportNeverProbesHost(t *testing.T) {
	t.Setenv("NVC_SIM_ROOT", "")
	for _, tc := range []struct {
		name             string
		report           *types.Report
		total, available float64
	}{
		{"old report", &types.Report{System: types.SystemInfo{RAMTotalMB: 65536}}, 64 * GiB, 0},
		{"no memory", &types.Report{}, 0, 0},
		{"recorded physical memory", &types.Report{System: types.SystemInfo{WindowsMemory: windowsSnapshot(79)}}, 64 * GiB, 32 * GiB},
		{"recorded GPU", rtx3090Report(), 24 * GiB, 23000 * 1024 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, _ := derivePool(tc.report, "windows", 1, 0, true, func(int) (MemoryPool, error) {
				t.Fatal("saved report queried the live Windows host")
				return MemoryPool{}, nil
			})
			if pool.TotalBytes != tc.total || pool.AvailableBytes != tc.available {
				t.Fatalf("wrong saved pool: %+v", pool)
			}
			if pool.WindowsMemory != tc.report.System.WindowsMemory {
				t.Fatal("recorded diagnostic was replaced")
			}
		})
	}
}

func TestWindowsLivePoolReusesRecordedSnapshot(t *testing.T) {
	t.Setenv("NVC_SIM_ROOT", "")
	r := &types.Report{System: types.SystemInfo{WindowsMemory: windowsSnapshot(79)}}
	pool, _ := derivePool(r, "windows", 1, 0, false, func(int) (MemoryPool, error) {
		t.Fatal("already recorded memory queried again")
		return MemoryPool{}, nil
	})
	if pool.TotalBytes != 64*GiB || pool.AvailableBytes != 32*GiB || pool.AllocatableBytes != 32*GiB || pool.SwapKnown {
		t.Fatalf("commit counted as physical capacity: %+v", pool)
	}
	for _, m := range []*types.WindowsMemoryInfo{nil, {}, {PhysicalTotalKB: new(uint64)}} {
		if _, err := poolFromWindowsMemory(m); err == nil {
			t.Fatal("missing/zero physical total accepted")
		}
	}
}

func TestWindowsUnknownAndExhaustedCommitRenderDifferently(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    *types.WindowsMemoryInfo
		want string
		low  bool
	}{
		{"not recorded", nil, "unknown (not recorded", false},
		{"missing counters", &types.WindowsMemoryInfo{}, "headroom unknown", false},
		{"zero limit", &types.WindowsMemoryInfo{CommitLimitBytes: new(uint64), CommittedBytes: new(uint64)}, "headroom unknown", false},
		{"zero committed", windowsSnapshot(0), "headroom 80.0 GiB", false},
		{"zero headroom", windowsSnapshot(80), "headroom 0.0 GiB", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &Plan{Platform: PlanPlatform{OS: "windows"}, HostWindowsMemory: tc.m}
			for _, output := range []string{RenderText(p), RenderMarkdown(p)} {
				if !strings.Contains(output, tc.want) || strings.Contains(output, "commit headroom is low") != tc.low {
					t.Fatalf("wrong known/unknown rendering: %s", output)
				}
			}
		})
	}
}

func TestWindowsCommitDoesNotChangeLinuxSparkRules(t *testing.T) {
	r := gb10Report()
	r.System.WindowsMemory = windowsSnapshot(80) // foreign data must not affect Linux.
	pool, _ := DerivePool(r, "linux", 1, 0, true)
	if pool.WindowsMemory != nil || !pool.Unified {
		t.Fatalf("foreign host data affected Linux: %+v", pool)
	}
	o := DefaultOptions()
	o.GOOS, o.Model, o.Runtime, o.Offline = "linux", "llama-3.1-8b-instruct", "vllm", true
	p, err := Build(r, pool, nil, false, o)
	if err != nil {
		t.Fatal(err)
	}
	if p.HostWindowsMemory != nil || strings.Contains(strings.Join(p.Warnings, " "), "Windows commit") {
		t.Fatal("Linux received Windows diagnostic")
	}
}

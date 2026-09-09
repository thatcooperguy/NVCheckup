package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestWindowsHostCommitSavedReport(t *testing.T) {
	r := createTestReport()
	r.Metadata.Platform = "windows"
	free, limit, used := uint64(32*1024*1024), uint64(80*1024*1024*1024), uint64(79*1024*1024*1024)
	r.System.WindowsMemory = &types.WindowsMemoryInfo{PhysicalFreeKB: &free, CommitLimitBytes: &limit, CommittedBytes: &used, Source: "fixture recorded counters"}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var saved types.Report
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	for _, render := range []func(*types.Report) string{GenerateText, GenerateMarkdown} {
		output := render(&saved)
		for _, want := range []string{"79.0 GiB committed / 80.0 GiB limit", "headroom 1.0 GiB", "physical RAM free 32.0 GiB", "fixture recorded counters", "GPU fit does not establish host readiness", "heuristic"} {
			if !strings.Contains(output, want) {
				t.Errorf("saved report missing %q", want)
			}
		}
		saved.System.WindowsMemory = nil
		if output := render(&saved); !strings.Contains(output, "unknown (not recorded") || strings.Contains(output, "commit headroom is low") {
			t.Fatal("missing data was not unknown")
		}
		saved.System.WindowsMemory = r.System.WindowsMemory
	}
}

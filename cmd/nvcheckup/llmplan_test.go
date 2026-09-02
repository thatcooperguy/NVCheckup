package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestParseLLMPlanFlags(t *testing.T) {
	var stderr bytes.Buffer
	if _, _, err := parseLLMPlanFlags([]string{"--model", "8b", "--ctx", "lots"}, &stderr); err == nil {
		t.Fatal("--ctx lots must fail")
	}
	o, f, err := parseLLMPlanFlags([]string{"--model", "8b", "--ctx", "32K", "--params-b", "1.5", "--json", "--out", "reports", "--nodes", "2", "--headroom-gib", "0"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if o.Model != "8b" || o.Context != 32768 || o.ParamsB != 1.5 || o.Nodes != 2 || o.HeadroomGiB != 0 {
		t.Errorf("options = %+v", o)
	}
	if !f.json || f.out != "reports" || !f.outSet || f.md {
		t.Errorf("flags = %+v", f)
	}
	o, _, err = parseLLMPlanFlags(nil, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if o.HeadroomGiB != -1 || o.Runtime != "auto" || o.Profile != "chat" || o.KVDtype != "auto" || o.Nodes != 1 {
		t.Errorf("defaults = %+v", o)
	}
	if _, _, err = parseLLMPlanFlags([]string{"--model", "8b", "extra"}, &stderr); err == nil {
		t.Error("positional arguments must be rejected")
	}
	// Out-of-range numbers are errors, not silently ignored.
	for _, bad := range [][]string{
		{"--model", "8b", "--memory-gib", "-5"},
		{"--model", "8b", "--memory-gib", "0"},
		{"--model", "8b", "--headroom-gib", "-3"},
		{"--model", "8b", "--headroom-gib", "-1"},
		{"--model", "8b", "--concurrency", "-1"},
		{"--model", "8b", "--nodes", "0"},
		{"--model", "8b", "--nodes", "3"},
		{"--model", "8b", "--timeout", "0"},
	} {
		if _, _, err := parseLLMPlanFlags(bad, &stderr); err == nil {
			t.Errorf("%v must be rejected", bad)
		}
	}
	o, _, err = parseLLMPlanFlags([]string{"--model", "8b", "--memory-gib", "64", "--headroom-gib", "0"}, &stderr)
	if err != nil || o.MemoryGiB != 64 || o.HeadroomGiB != 0 {
		t.Errorf("valid --memory-gib/--headroom-gib rejected: %v %+v", err, o)
	}
}

func TestRunLLMPlan_NonInteractiveNeedsModel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runLLMPlan(nil, strings.NewReader(""), &stdout, &stderr, false, "linux"); code != types.ExitError {
		t.Errorf("exit %d, want 3", code)
	}
	if !strings.Contains(stderr.String(), "--model") {
		t.Errorf("stderr: %s", stderr.String())
	}
}

func TestRunLLMPlan_ListModels(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runLLMPlan([]string{"--list-models"}, strings.NewReader(""), &stdout, &stderr, false, "linux"); code != types.ExitOK {
		t.Errorf("exit %d", code)
	}
	for _, id := range []string{"llama-3.1-8b-instruct", "gpt-oss-120b", "nemotron-3-super-120b-a12b-nvfp4"} {
		if !strings.Contains(stdout.String(), id) {
			t.Errorf("--list-models lacks %s", id)
		}
	}
}

// TestRunLLMPlan_FromReport plans offline from a saved report.json and writes
// plan.txt/plan.json into --out only.
func TestRunLLMPlan_FromReport(t *testing.T) {
	t.Setenv("TRITON_PTXAS_PATH", "")
	t.Setenv("NVC_SIM_ROOT", "")
	dir := t.TempDir()
	report := types.Report{
		Metadata:      types.ReportMetadata{Platform: "linux"},
		System:        types.SystemInfo{OSName: "Ubuntu 24.04", Architecture: "aarch64"},
		GPUs:          []types.GPUInfo{{Name: "NVIDIA GB10", IsNVIDIA: true, DriverVersion: "580.95.05", MemoryReporting: "not-supported"}},
		Driver:        types.DriverInfo{Version: "580.95.05", CUDAVersion: "13.0"},
		Linux:         &types.LinuxInfo{ContainerRuntime: "docker", NVContainerToolkit: "1.17.8"},
		Platform:      types.PlatformInfo{Class: "dgx-spark", GPUSoC: "GB10", UnifiedMemory: true},
		UnifiedMemory: &types.UnifiedMemoryInfo{MemTotalKB: 125513944, MemAvailableKB: 115000000, CachedKB: 20000000},
	}
	data, _ := json.Marshal(report)
	reportPath := filepath.Join(dir, "report.json")
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	var stdout, stderr bytes.Buffer
	code := runLLMPlan([]string{"--report", reportPath, "--model", "llama-3.1-8b-instruct", "--profile", "agent", "--runtime", "vllm", "--json", "--out", out}, strings.NewReader(""), &stdout, &stderr, false, "linux")
	if code != types.ExitOK {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "FITS:") || !strings.Contains(stdout.String(), "--gpu-memory-utilization 0.40") {
		t.Errorf("stdout:\n%s", stdout.String())
	}
	for _, name := range []string{"plan.txt", "plan.json"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("%s not written: %v", name, err)
		}
	}
	// stdout carries exactly the text written to plan.txt (notes included).
	if txt, err := os.ReadFile(filepath.Join(out, "plan.txt")); err != nil || !strings.Contains(stdout.String(), string(txt)) {
		t.Errorf("stdout must contain plan.txt verbatim (err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(out, "plan.md")); err == nil {
		t.Error("plan.md written without --md")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 { // report.json and out/
		t.Errorf("unexpected files next to --out: %v", entries)
	}

	// 70B BF16 at 128K does not fit -> 2, and the run must not fail when only the verdict is negative.
	stdout.Reset()
	code = runLLMPlan([]string{"--report", reportPath, "--model", "70b", "--quant", "bf16", "--context", "131072", "--runtime", "vllm"}, strings.NewReader(""), &stdout, &stderr, false, "linux")
	if code != types.ExitCritical {
		t.Errorf("70B BF16: exit %d, want 2", code)
	}
	// Bad flag values -> 3.
	if code = runLLMPlan([]string{"--report", reportPath, "--model", "8b", "--quant", "nf4"}, strings.NewReader(""), &stdout, &stderr, false, "linux"); code != types.ExitError {
		t.Errorf("nf4: exit %d, want 3", code)
	}
	if code = runLLMPlan([]string{"--report", filepath.Join(dir, "missing.json"), "--model", "8b"}, strings.NewReader(""), &stdout, &stderr, false, "linux"); code != types.ExitError {
		t.Errorf("missing report: exit %d, want 3", code)
	}
}

// TestRunLLMPlan_InteractivePrompt: with a TTY (simulated) and no --model the
// wizard prompts; piped answers pick the model and the plan is produced.
func TestRunLLMPlan_InteractivePrompt(t *testing.T) {
	t.Setenv("TRITON_PTXAS_PATH", "")
	dir := t.TempDir()
	report := types.Report{
		Metadata:      types.ReportMetadata{Platform: "linux"},
		GPUs:          []types.GPUInfo{{Name: "NVIDIA GB10", IsNVIDIA: true, DriverVersion: "580.95.05", MemoryReporting: "not-supported"}},
		Driver:        types.DriverInfo{Version: "580.95.05", CUDAVersion: "13.0"},
		Platform:      types.PlatformInfo{Class: "dgx-spark", GPUSoC: "GB10", UnifiedMemory: true},
		UnifiedMemory: &types.UnifiedMemoryInfo{MemTotalKB: 125513944, MemAvailableKB: 115000000},
	}
	data, _ := json.Marshal(report)
	reportPath := filepath.Join(dir, "report.json")
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// answers: model 5 (gpt-oss-120b), default quant, agent, default ctx, default conc, vllm, single node
	code := runLLMPlan([]string{"--report", reportPath}, strings.NewReader("5\n\nb\n\n\nvllm\na\n"), &stdout, &stderr, true, "linux")
	if code != types.ExitOK && code != types.ExitWarnings {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "1. Which model?") || !strings.Contains(stdout.String(), "gpt-oss-120b (MXFP4)") || !strings.Contains(stdout.String(), "--gpu-memory-utilization 0.65") {
		t.Errorf("stdout:\n%s", stdout.String())
	}
}

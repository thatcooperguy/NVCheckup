package llmplan

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/redact"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func statusOf(ps []Prereq, id string) (Prereq, bool) {
	for _, p := range ps {
		if p.ID == id {
			return p, true
		}
	}
	return Prereq{}, false
}

func expect(t *testing.T, ps []Prereq, id, status string) Prereq {
	t.Helper()
	p, ok := statusOf(ps, id)
	if !ok {
		t.Errorf("prerequisite %q missing", id)
		return p
	}
	if p.Status != status {
		t.Errorf("%s = %s (%s), want %s", id, p.Status, p.Detail, status)
	}
	return p
}

func evalGB10(t *testing.T, r *types.Report, rt Runtime, kv KVDtype, nodes int, ports []int, known bool) []Prereq {
	t.Helper()
	in := Inputs{Model: mustModel(t, "llama-3.1-8b-instruct"), Quant: QuantBF16, KV: kv, Context: 32768, Concurrency: 4, Runtime: rt, Nodes: nodes,
		PoolBytes: poolGB10Bytes, AvailableBytes: float64(r.UnifiedMemory.MemAvailableKB) * 1024, FloorBytes: floorLinux}
	s := Compute(in)
	cmd := RenderCommand(in, s, "chat", ClusterFacts{})
	return Evaluate(Facts{Report: r, Pool: poolFromUnifiedMemory(r.UnifiedMemory), Ports: ports, PortsKnown: known, GOOS: "linux"}, in, s, cmd)
}

func TestEvaluate_HealthyGB10(t *testing.T) {
	r := gb10Report()
	ps := evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true)
	expect(t, ps, "driver-present", StatusPass)
	expect(t, ps, "cuda-13", StatusPass)
	expect(t, ps, "ota-not-torn", StatusPass)
	expect(t, ps, "torch-cu130-sm120", StatusPass)
	expect(t, ps, "triton-ptxas-path", StatusSkip)
	expect(t, ps, "swap-in-use", StatusPass)
	expect(t, ps, "page-cache", StatusPass)
	expect(t, ps, "model-server-ports", StatusPass)
	expect(t, ps, "container-image", StatusSkip)
	expect(t, ps, "docker-gpu-runtime", StatusPass)
	expect(t, ps, "ipc-shm", StatusPass)
	expect(t, ps, "memavailable-fits", StatusPass)
	if _, ok := statusOf(ps, "cx7-link"); ok {
		t.Error("cx7-link is only evaluated for --nodes 2")
	}
	if WorstStatus(ps) != StatusPass {
		t.Errorf("healthy GB10 should be all PASS/SKIP, got %s", WorstStatus(ps))
	}
}

func TestEvaluate_Failures(t *testing.T) {
	r := gb10Report()
	r.Driver.Version, r.GPUs[0].DriverVersion = "570.86.10", "570.86.10"
	r.Driver.CUDAVersion, r.AI.CUDADriverVersion = "12.8", "12.8"
	r.DGXOS.OTATorn = intPtr(3)
	r.AI.PyTorchInfo.Version, r.AI.PyTorchInfo.CUDAVersion = "2.7.0+cu128", "12.8"
	r.AI.KeyPackages = []types.PackageInfo{{Name: "triton", Version: "3.3.0"}}
	r.UnifiedMemory.SwapTotalKB, r.UnifiedMemory.SwapFreeKB = 8000000, 4000000
	r.UnifiedMemory.MemAvailableKB = 20000000 // 19 GiB < 43 GiB needed
	r.Linux.NVContainerToolkit = ""
	ps := evalGB10(t, r, RuntimeVLLM, KVF16, 2, []int{22, 8000, 11434}, true)
	expect(t, ps, "driver-present", StatusWarn)
	expect(t, ps, "cuda-13", StatusWarn)
	expect(t, ps, "ota-not-torn", StatusFail)
	expect(t, ps, "torch-cu130-sm120", StatusWarn)
	p := expect(t, ps, "triton-ptxas-path", StatusWarn)
	if !strings.Contains(p.Detail, "TRITON_PTXAS_PATH=/usr/local/cuda/bin/ptxas") {
		t.Errorf("triton detail: %s", p.Detail)
	}
	expect(t, ps, "swap-in-use", StatusWarn)
	p = expect(t, ps, "model-server-ports", StatusWarn)
	if !strings.Contains(p.Detail, "8000, 11434") {
		t.Errorf("ports detail: %s", p.Detail)
	}
	expect(t, ps, "docker-gpu-runtime", StatusFail)
	expect(t, ps, "memavailable-fits", StatusFail)
	expect(t, ps, "cx7-link", StatusFail)
	if WorstStatus(ps) != StatusFail {
		t.Error("worst status must be FAIL")
	}
}

// Grace Hopper (GPUSoC "GH200", Class grace-hopper) is not a Spark: the 580 /
// CUDA 13 sm_121 expectations of spec 7.7 do not apply, so a 570 driver with
// CUDA 12.8 passes and the OTA check is absent.
func TestEvaluate_GraceHopper(t *testing.T) {
	r := gh200Report()
	pool, _ := DerivePool(r, "linux", 5, 0, true)
	in := Inputs{Model: mustModel(t, "llama-3.3-70b-instruct"), Quant: QuantBF16, KV: KVF16, Context: 32768, Concurrency: 1, Runtime: RuntimeVLLM, Nodes: 1,
		PoolBytes: pool.TotalBytes, AvailableBytes: pool.AvailableBytes}
	s := Compute(in)
	cmd := RenderCommand(in, s, "chat", ClusterFacts{})
	ps := Evaluate(Facts{Report: r, Pool: pool, GOOS: "linux"}, in, s, cmd)
	p := expect(t, ps, "driver-present", StatusPass)
	if strings.Contains(p.Detail, "sm_121") {
		t.Errorf("GH200 must not get the sm_121 note: %s", p.Detail)
	}
	expect(t, ps, "cuda-13", StatusPass)
	if _, ok := statusOf(ps, "ota-not-torn"); ok {
		t.Error("ota-not-torn is dgx-spark only")
	}
	// 70B BF16 (131.5 GiB) does not fit 95.6 GiB of HBM: the sizing sees the
	// discrete pool, not the 480 GB of system RAM.
	if s.FitsTotal {
		t.Errorf("70B BF16 must not fit a 97871 MiB GH200, total %s pool %s", fmtGiB(s.TotalBytes), fmtGiB(s.PoolBytes))
	}
}

func TestEvaluate_MissingDriverAndUnknowns(t *testing.T) {
	r := gb10Report()
	r.Driver = types.DriverInfo{}
	r.GPUs[0].DriverVersion = ""
	r.AI = nil
	r.DGXOS = nil
	ps := evalGB10(t, r, RuntimeLlamaCpp, KVQ8_0, 1, nil, false)
	expect(t, ps, "driver-present", StatusFail)
	expect(t, ps, "cuda-13", StatusWarn)
	expect(t, ps, "torch-cu130-sm120", StatusSkip)
	expect(t, ps, "model-server-ports", StatusSkip)
	if _, ok := statusOf(ps, "ota-not-torn"); ok {
		t.Error("ota-not-torn only when DGX OS data exists")
	}
	if _, ok := statusOf(ps, "docker-gpu-runtime"); ok {
		t.Error("docker checks only for container runtimes")
	}
}

// Placeholder version strings ("N/A", "[N/A]", "Not Supported") are not
// versions: they take the "not detected / not reported" branches.
func TestEvaluate_NAPlaceholders(t *testing.T) {
	for _, s := range []string{"", "N/A", "[N/A]", "n/a", "[Not Supported]", "Not Supported", " [N/A] "} {
		if versionOrEmpty(s) != "" {
			t.Errorf("versionOrEmpty(%q) = %q, want empty", s, versionOrEmpty(s))
		}
	}
	if versionOrEmpty("580.95.05") != "580.95.05" || versionOrEmpty(" 13.0 ") != "13.0" {
		t.Error("real versions must pass through")
	}
	r := gb10Report()
	r.Driver.Version, r.GPUs[0].DriverVersion = "N/A", "[N/A]"
	r.Driver.CUDAVersion, r.AI.CUDADriverVersion = "[N/A]", "Not Supported"
	ps := evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true)
	p := expect(t, ps, "driver-present", StatusFail)
	if !strings.Contains(p.Detail, "no NVIDIA driver version detected") || strings.Contains(p.Detail, "N/A") {
		t.Errorf("driver detail: %s", p.Detail)
	}
	p = expect(t, ps, "cuda-13", StatusWarn)
	if !strings.Contains(p.Detail, "CUDA version not reported") || strings.Contains(p.Detail, "N/A") {
		t.Errorf("cuda detail: %s", p.Detail)
	}
}

// Privacy: the TRITON_PTXAS_PATH environment value bypasses core.Run's report
// redaction, so llm-plan redacts it itself before it reaches any output.
func TestEvaluate_TritonEnvRedacted(t *testing.T) {
	fakeHome := filepath.Join(string(filepath.Separator)+"home", "alice")
	ptxas := filepath.Join(fakeHome, ".venv", "lib", "python3.12", "site-packages", "triton", "backends", "nvidia", "bin", "ptxas")
	t.Setenv("TRITON_PTXAS_PATH", ptxas)
	old := testRedactor
	testRedactor = redact.NewWithIdentity(true, "alice", "alice-spark", fakeHome)
	defer func() { testRedactor = old }()

	r := gb10Report()
	r.AI.KeyPackages = []types.PackageInfo{{Name: "triton", Version: "3.5.0"}}
	o := DefaultOptions()
	o.GOOS = "linux"
	o.Model, o.Runtime = "llama-3.1-8b-instruct", "vllm"
	p, err := Build(r, poolFromUnifiedMemory(r.UnifiedMemory), nil, true, o)
	if err != nil {
		t.Fatal(err)
	}
	pr := expect(t, p.Prerequisites, "triton-ptxas-path", StatusPass)
	if !strings.Contains(pr.Detail, "<home>") || !strings.Contains(pr.Detail, "ptxas") {
		t.Errorf("triton detail must keep the redacted path: %s", pr.Detail)
	}
	js, err := RenderJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{"plan.txt": RenderText(p), "plan.json": js, "plan.md": RenderMarkdown(p)} {
		if strings.Contains(out, fakeHome) || strings.Contains(out, filepath.ToSlash(fakeHome)) || strings.Contains(out, jsonEscaped(fakeHome)) || strings.Contains(out, "alice") {
			t.Errorf("%s leaks the home directory of TRITON_PTXAS_PATH", name)
		}
		if !strings.Contains(out, "TRITON_PTXAS_PATH=<home>") && !strings.Contains(out, jsonEscaped("TRITON_PTXAS_PATH=<home>")) {
			t.Errorf("%s lacks the redacted TRITON_PTXAS_PATH value", name)
		}
	}
}

func TestEvaluate_PressureFloorAfterLoad(t *testing.T) {
	r := gb10Report()
	r.UnifiedMemory.MemAvailableKB = 48 * 1024 * 1024 // 48 GiB: covers 43 GiB but leaves < 8 GiB
	ps := evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true)
	p := expect(t, ps, "memavailable-fits", StatusWarn)
	if !strings.Contains(p.Detail, "unified-memory-pressure") {
		t.Errorf("detail: %s", p.Detail)
	}
}

func TestEvaluate_ContainerImages(t *testing.T) {
	r := gb10Report()
	r.Ecosystem = &types.EcosystemInfo{Images: []types.ContainerImage{{Ref: "nvcr.io/nvidia/vllm:26.05-py3", Arch: "arm64"}}}
	expect(t, evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true), "container-image", StatusPass)
	r.Ecosystem.Images = []types.ContainerImage{{Ref: "nvcr.io/nvidia/vllm:26.05-py3", Arch: "amd64"}}
	expect(t, evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true), "container-image", StatusFail)
	r.Ecosystem.Images = []types.ContainerImage{{Ref: "nvcr.io/nvidia/pytorch:25.11-py3", Arch: "arm64"}}
	expect(t, evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true), "container-image", StatusWarn)
	r.Ecosystem.SnapDocker = true
	expect(t, evalGB10(t, r, RuntimeVLLM, KVF16, 1, nil, true), "docker-gpu-runtime", StatusFail)
}

func TestCX7Check(t *testing.T) {
	r := gb10Report()
	r.Cluster = &types.ClusterInfo{Ports: []types.FabricPort{
		{RDMADev: "rocep1s0f0", Netdev: "enp1s0f0np0", State: "4: ACTIVE", PhysState: "5: LinkUp", SpeedMbps: 200000, MTU: 9000},
		{RDMADev: "roceP2p1s0f0", Netdev: "enP2p1s0f0np0", State: "4: ACTIVE", PhysState: "5: LinkUp", SpeedMbps: 200000, MTU: 9000},
		{RDMADev: "rocep1s0f1", Netdev: "enp1s0f1np1", State: "1: DOWN", PhysState: "3: Disabled"},
		{RDMADev: "roceP2p1s0f1", Netdev: "enP2p1s0f1np1", State: "1: DOWN", PhysState: "3: Disabled"},
	}}
	p := cx7Check(r)
	if p.Status != StatusPass || !strings.Contains(p.Detail, "roceP2p1s0f0,rocep1s0f0") {
		t.Errorf("healthy cage: %+v", p)
	}
	if devs := ActiveRDMADevs(r); strings.Join(devs, ",") != "roceP2p1s0f0,rocep1s0f0" {
		t.Errorf("active devs = %v", devs)
	}
	r.Cluster.Ports[1].SpeedMbps = 100000
	if p = cx7Check(r); p.Status != StatusWarn {
		t.Errorf("slow twin: %+v", p)
	}
	r.Cluster.Ports[1].State = "1: DOWN"
	if p = cx7Check(r); p.Status != StatusWarn || !strings.Contains(p.Detail, "only one twin") {
		t.Errorf("one twin: %+v", p)
	}
	r.Cluster.Ports[0].State = "1: DOWN"
	if p = cx7Check(r); p.Status != StatusFail {
		t.Errorf("no twin: %+v", p)
	}
	if ps := evalGB10(t, r, RuntimeVLLM, KVF16, 2, nil, true); WorstStatus(ps) != StatusFail {
		t.Error("two-node plan without an active fabric must FAIL")
	}
}

// driverPrereq evaluates the checklist for a saved report of any platform
// (pool from the report only) and returns its driver-present line.
func driverPrereq(t *testing.T, r *types.Report, goos string) Prereq {
	t.Helper()
	pool, _ := DerivePool(r, goos, 5, 0, true)
	in := Inputs{Model: mustModel(t, "llama-3.1-8b-instruct"), Quant: QuantBF16, KV: KVF16, Context: 8192, Concurrency: 1, Runtime: RuntimeLlamaCpp, Nodes: 1,
		PoolBytes: pool.TotalBytes, AvailableBytes: pool.AvailableBytes}
	s := Compute(in)
	cmd := RenderCommand(in, s, "chat", ClusterFacts{})
	ps := Evaluate(Facts{Report: r, Pool: pool, GOOS: goos}, in, s, cmd)
	p, ok := statusOf(ps, "driver-present")
	if !ok {
		t.Fatal("driver-present missing")
	}
	return p
}

// isWDDMVersion: four numeric fields with a 30-40 WDDM generation first; a
// Linux branch, a bare "616.00" or anything with letters is not one.
func TestIsWDDMVersion(t *testing.T) {
	for s, want := range map[string]bool{
		"32.0.16.1600": true, "32.0.15.8129": true, " 31.0.15.3667 ": true, "30.0.14.7168": true,
		"580.95.05": false, "616.00": false, "581.29": false, "32.0.16": false, "32.0.16.1600.1": false,
		"29.0.15.1000": false, "41.0.1.1": false, "32.0.16.16a0": false, "32..16.1600": false, "": false, "N/A": false,
	} {
		if got := isWDDMVersion(s); got != want {
			t.Errorf("isWDDMVersion(%q) = %v, want %v", s, got, want)
		}
	}
}

// driver-present on RTX Spark (spec 2.2, 8): the developer-preview package
// has no nvidia-smi.exe, so Driver.Version is empty and the WDDM string from
// WMI ("32.0.16.1600") is the only driver source. It is never read as a
// branch against the 580 minimum; the 616.00 Developer Preview WARNs because
// rule rtx-spark-driver-developer-preview is WARN (pre-release, not for
// production or benchmarking), any other WDDM string is a PASS with the
// mapping labelled unconfirmed, and Linux / Windows x64 keep their results.
func TestEvaluate_DriverPresent(t *testing.T) {
	// RTX Spark as the Windows collectors leave it without nvidia-smi.exe.
	n1xWDDM := func() *types.Report {
		r := n1xWindowsReport()
		r.Driver = types.DriverInfo{}
		return r
	}
	cases := []struct {
		name   string
		rep    func() *types.Report
		goos   string
		status string
		want   []string // substrings the detail must contain
		reject []string // substrings the detail must not contain
	}{
		{"rtx-spark WDDM 16.1600 without nvidia-smi", n1xWDDM, "windows", StatusWarn,
			[]string{"WDDM driver 32.0.16.1600", "616.00", "developer preview", "nvidia-smi.exe not shipped", "rtx-spark-driver-developer-preview"}, []string{"580"}},
		{"rtx-spark WDDM string of another driver", func() *types.Report {
			r := n1xWDDM()
			r.GPUs[0].DriverVersion = "32.0.16.2005"
			return r
		}, "windows", StatusPass, []string{"WDDM driver 32.0.16.2005", "nvidia-smi.exe absent", "version mapping unconfirmed"}, []string{"616.00", "580"}},
		{"rtx-spark WDDM from Platform.WoA only", func() *types.Report {
			r := n1xWDDM()
			r.GPUs[0].DriverVersion = ""
			r.Platform.WoA = &types.WoAInfo{DriverVersion: "32.0.16.1600", InfFilename: "nv_surface_woa.inf", DeveloperPreview: true}
			return r
		}, "windows", StatusWarn, []string{"WDDM driver 32.0.16.1600", "616.00"}, []string{"580"}},
		{"rtx-spark INF-flagged preview with an unconfirmed WDDM string", func() *types.Report {
			r := n1xWDDM()
			r.GPUs[0].DriverVersion = "32.0.16.2005"
			r.Platform.WoA = &types.WoAInfo{DriverVersion: "32.0.16.2005", InfFilename: "nv_surface_woa.inf", DeveloperPreview: true}
			return r
		}, "windows", StatusWarn, []string{"WDDM driver 32.0.16.2005", "nv_surface_woa.inf", "version mapping unconfirmed"}, []string{"616.00", "580"}},
		{"rtx-spark inferred from Windows on Arm and the adapter name", func() *types.Report {
			r := n1xWDDM()
			r.Platform.Class, r.Platform.GPUSoC = "", ""
			return r
		}, "windows", StatusWarn, []string{"WDDM driver 32.0.16.1600", "616.00"}, []string{"580"}},
		{"rtx-spark with nvidia-smi 616.00", n1xWindowsReport, "windows", StatusWarn,
			[]string{"driver 616.00", "developer preview", "rtx-spark-driver-developer-preview"}, []string{"WDDM", "not shipped", "580"}},
		{"rtx-spark without any driver string", func() *types.Report {
			r := n1xWDDM()
			r.GPUs[0].DriverVersion = ""
			return r
		}, "windows", StatusFail, []string{"no NVIDIA driver version detected"}, nil},
		{"windows x64 3090 with nvidia-smi 581.29", func() *types.Report {
			r := rtx3090Report()
			r.Driver.Version, r.GPUs[0].DriverVersion = "581.29", "581.29"
			return r
		}, "windows", StatusPass, []string{"driver 581.29"}, []string{"WDDM", "developer preview"}},
		{"windows x64 3090 without nvidia-smi (WDDM only)", func() *types.Report {
			r := rtx3090Report()
			r.Driver = types.DriverInfo{}
			r.GPUs[0].DriverVersion = "32.0.15.8129"
			return r
		}, "windows", StatusPass, []string{"WDDM driver 32.0.15.8129", "version mapping unconfirmed"}, []string{"580", "616.00", "developer preview"}},
		{"linux GB10 580.95.05", gb10Report, "linux", StatusPass, []string{"driver 580.95.05"}, []string{"WDDM"}},
		{"linux GB10 570 branch", func() *types.Report {
			r := gb10Report()
			r.Driver.Version, r.GPUs[0].DriverVersion = "570.86.10", "570.86.10"
			return r
		}, "linux", StatusWarn, []string{"driver 570.86.10", "580 branch"}, nil},
		{"linux GH200 570 off Spark", gh200Report, "linux", StatusPass, []string{"driver 570.86.15"}, []string{"580"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := driverPrereq(t, tc.rep(), tc.goos)
			if p.Status != tc.status {
				t.Errorf("status = %s (%s), want %s", p.Status, p.Detail, tc.status)
			}
			for _, w := range tc.want {
				if !strings.Contains(p.Detail, w) {
					t.Errorf("detail %q lacks %q", p.Detail, w)
				}
			}
			for _, w := range tc.reject {
				if strings.Contains(p.Detail, w) {
					t.Errorf("detail %q must not contain %q", p.Detail, w)
				}
			}
		})
	}
}

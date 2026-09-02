package llmplan

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/thatcooperguy/nvcheckup/internal/redact"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// Prereq statuses (spec 7.7: PASS/WARN/FAIL from the read-only report; SKIP
// when the report has no data for the check).
const (
	StatusPass = "PASS"
	StatusWarn = "WARN"
	StatusFail = "FAIL"
	StatusSkip = "SKIP"
)

// Prereq is one line of the prerequisite table (spec 7.8 prerequisites[{id,status,detail}]).
type Prereq struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Facts bundles the read-only inputs of the prerequisite checks.
type Facts struct {
	Report     *types.Report
	Pool       MemoryPool
	Ports      []int
	PortsKnown bool
	TritonEnv  string // TRITON_PTXAS_PATH of this process
	GOOS       string
}

// Spec 7.7 thresholds.
const (
	minDriverMajor    = 580      // spec 2.1 / 7.7: driver >= 580
	minCUDAMajor      = 13       // spec 7.7: CUDA 13 (sm_121)
	maxSwapUsedBytes  = 1 * GiB  // spec 7.7: swap used < 1 GiB
	pressureWarnBytes = 8 * GiB  // spec 7.4: unified-memory-pressure WARN < 8 GiB
	torchCUDATag      = "+cu130" // spec 7.7: torch +cu130
	tritonPtxasPath   = "/usr/local/cuda/bin/ptxas"
)

// versionOrEmpty maps the placeholders nvidia-smi and the collectors print for
// an absent value ("N/A", "[N/A]", "[Not Supported]", "Not Supported", any
// bracketed token) to "", so they take the "not detected / not reported"
// branches instead of being treated as a version (same rule as the common
// collector's isNotAvailable, replicated because llmplan must not import
// collector internals).
func versionOrEmpty(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return ""
	}
	switch strings.ToLower(s) {
	case "n/a", "not supported", "not available", "unknown":
		return ""
	}
	return s
}

// Values that come from this process's environment (TRITON_PTXAS_PATH) bypass
// core.Run's report redaction, so they are redacted here before they reach
// plan.txt/plan.json/plan.md (the <home>/<user>/<host> token classes of
// internal/redact). testRedactor lets tests use a deterministic identity.
var (
	defaultRedactor     *redact.Redactor
	defaultRedactorOnce sync.Once
	testRedactor        *redact.Redactor
)

func redactValue(s string) string {
	if testRedactor != nil {
		return testRedactor.Redact(s)
	}
	defaultRedactorOnce.Do(func() { defaultRedactor = redact.New(true) })
	return defaultRedactor.Redact(s)
}

// availLabel names the "available now" figure: MemAvailable on a shared pool,
// free VRAM on a discrete GPU.
func availLabel(p MemoryPool) string {
	if p.Discrete {
		return "VRAM free"
	}
	return "MemAvailable"
}

func majorOf(v string) int {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, ". "); i > 0 {
		v = v[:i]
	}
	n, _ := strconv.Atoi(v)
	return n
}

// Evaluate runs the spec 7.7 checklist against the report and sizing.
func Evaluate(f Facts, in Inputs, s Sizing, cmd Command) []Prereq {
	var out []Prereq
	add := func(id, status, detail string) { out = append(out, Prereq{ID: id, Status: status, Detail: detail}) }
	r := f.Report
	spark := SparkSoC(r) != ""

	// Driver present / branch.
	driver := ""
	cuda := ""
	if r != nil {
		driver = versionOrEmpty(r.Driver.Version)
		cuda = versionOrEmpty(r.Driver.CUDAVersion)
		for _, g := range r.GPUs {
			if driver == "" && g.IsNVIDIA {
				driver = versionOrEmpty(g.DriverVersion)
			}
		}
		if cuda == "" && r.AI != nil {
			cuda = versionOrEmpty(r.AI.CUDADriverVersion)
		}
	}
	if driver == "" {
		add("driver-present", StatusFail, "no NVIDIA driver version detected (nvidia-smi absent or failed)")
	} else if spark && majorOf(driver) < minDriverMajor {
		add("driver-present", StatusWarn, fmt.Sprintf("driver %s; DGX OS 7 / sm_121 expects the 580 branch (spec 2.1, 7.7)", driver))
	} else {
		add("driver-present", StatusPass, "driver "+driver)
	}
	switch {
	case cuda == "" && spark:
		add("cuda-13", StatusWarn, "CUDA version not reported; sm_121 (compute capability 12.1) needs CUDA 13 (spec 7.7)")
	case cuda == "":
		add("cuda-13", StatusSkip, "CUDA version not reported")
	case spark && majorOf(cuda) < minCUDAMajor:
		add("cuda-13", StatusWarn, fmt.Sprintf("CUDA %s; sm_121 needs CUDA 13 (spec 7.7)", cuda))
	default:
		add("cuda-13", StatusPass, "CUDA "+cuda)
	}

	// OTA not torn (dgx-spark only).
	if r != nil && r.DGXOS != nil {
		switch {
		case len(r.DGXOS.OTAFailed) > 0:
			add("ota-not-torn", StatusFail, "nvidia-spark-ota-check reports failed components: "+strings.Join(r.DGXOS.OTAFailed, ", "))
		case r.DGXOS.OTATorn != nil && *r.DGXOS.OTATorn > 0:
			add("ota-not-torn", StatusFail, fmt.Sprintf("OTA torn score %d; finish the update before loading models (dgx-spark-ota-torn)", *r.DGXOS.OTATorn))
		case r.DGXOS.OTATorn == nil:
			add("ota-not-torn", StatusSkip, "nvidia-spark-ota-check not available")
		default:
			add("ota-not-torn", StatusPass, "OTA torn score 0")
		}
	}

	// torch +cu130 with sm_120 in the arch list.
	if r != nil && r.AI != nil && r.AI.PyTorchInfo != nil && r.AI.PyTorchInfo.Version != "" {
		t := r.AI.PyTorchInfo
		arch := t.ArchList
		if len(arch) == 0 && r.Ecosystem != nil {
			arch = r.Ecosystem.TorchArchList
		}
		cuOK := strings.Contains(t.Version, torchCUDATag) || majorOf(t.CUDAVersion) >= minCUDAMajor
		archOK := false
		for _, a := range arch {
			if a == "sm_120" || a == "sm_121" {
				archOK = true
			}
		}
		switch {
		case !spark:
			add("torch-cu130-sm120", StatusPass, fmt.Sprintf("torch %s (CUDA %s)", t.Version, orNA(t.CUDAVersion)))
		case cuOK && archOK:
			add("torch-cu130-sm120", StatusPass, fmt.Sprintf("torch %s, arch list includes sm_120/sm_121", t.Version))
		case cuOK:
			add("torch-cu130-sm120", StatusWarn, fmt.Sprintf("torch %s is cu130 but sm_120 is not in torch.cuda.get_arch_list() (%s); rebuild or use an NGC image (spec 7.7)", t.Version, strings.Join(arch, ",")))
		default:
			add("torch-cu130-sm120", StatusWarn, fmt.Sprintf("torch %s is not a +cu130 build; sm_121 needs cu130 wheels or nvcr.io/nvidia/pytorch >= 25.11 (spec 7.7)", t.Version))
		}
	} else {
		add("torch-cu130-sm120", StatusSkip, "torch not found in the probed python (container runtimes bring their own)")
	}

	// TRITON_PTXAS_PATH when Triton is present.
	tritonPresent := false
	if r != nil {
		if r.Ecosystem != nil && r.Ecosystem.TritonPtxasVersion != "" {
			tritonPresent = true
		}
		if r.AI != nil {
			for _, p := range r.AI.KeyPackages {
				if strings.EqualFold(p.Name, "triton") {
					tritonPresent = true
				}
			}
		}
	}
	tritonEnv := f.TritonEnv
	if tritonEnv == "" && r != nil && r.Ecosystem != nil {
		tritonEnv = r.Ecosystem.TritonPtxasPath
	}
	switch {
	case !tritonPresent:
		add("triton-ptxas-path", StatusSkip, "Triton not installed in the probed python")
	case tritonEnv != "":
		// Privacy: the raw environment value may contain the user's home
		// directory; it is redacted before it is printed anywhere.
		add("triton-ptxas-path", StatusPass, "TRITON_PTXAS_PATH="+redactValue(tritonEnv))
	default:
		add("triton-ptxas-path", StatusWarn, "Triton is installed but TRITON_PTXAS_PATH is unset; export TRITON_PTXAS_PATH="+tritonPtxasPath+" (spec 7.7, sm121-triton-ptxas-stale)")
	}

	// Swap used < 1 GiB.
	if f.Pool.SwapKnown {
		used := f.Pool.SwapUsedBytes()
		if used >= maxSwapUsedBytes {
			add("swap-in-use", StatusWarn, fmt.Sprintf("swap in use: %s (spec 7.7 wants < 1 GiB; unified-memory-swap-in-use)", fmtGiB(used)))
		} else {
			add("swap-in-use", StatusPass, fmt.Sprintf("swap in use %s", fmtGiB(used)))
		}
	} else {
		add("swap-in-use", StatusSkip, "swap usage not available")
	}

	// Page cache vs MemAvailable.
	if f.Pool.Discrete {
		add("page-cache", StatusSkip, "page cache vs MemAvailable is a unified-memory check; the pool here is dedicated VRAM")
	} else if f.Pool.AvailableBytes > 0 && f.Pool.CachedBytes > 0 {
		add("page-cache", StatusPass, fmt.Sprintf("page cache %s is reclaimable and already counted in MemAvailable %s (MemFree %s)", fmtGiB(f.Pool.CachedBytes), fmtGiB(f.Pool.AvailableBytes), fmtGiB(f.Pool.FreeBytes)))
	} else if f.Pool.AvailableBytes > 0 {
		add("page-cache", StatusPass, fmt.Sprintf("MemAvailable %s", fmtGiB(f.Pool.AvailableBytes)))
	} else {
		add("page-cache", StatusSkip, "MemAvailable not available")
	}

	// No other resident model server.
	if f.PortsKnown {
		var busy []string
		for _, w := range watchedPorts {
			for _, p := range f.Ports {
				if p == w {
					busy = append(busy, strconv.Itoa(p))
				}
			}
		}
		if len(busy) > 0 {
			add("model-server-ports", StatusWarn, fmt.Sprintf("port(s) %s already listening: another model server may be resident (MemAvailable reflects it; llm-plan stops nothing)", strings.Join(busy, ", ")))
		} else {
			add("model-server-ports", StatusPass, "ports 8000/30000/11434/8355 are free")
		}
	} else {
		add("model-server-ports", StatusSkip, "listening ports not available")
	}

	// Container image, docker GPU runtime, ipc/shm.
	if in.Runtime.IsContainer() {
		if r != nil && r.Ecosystem != nil && len(r.Ecosystem.Images) > 0 {
			repo := cmd.Image
			if i := strings.LastIndex(repo, ":"); i > 0 {
				repo = repo[:i]
			}
			status, detail := StatusWarn, fmt.Sprintf("%s not present locally; pull it yourself (llm-plan never pulls)", cmd.Image)
			for _, img := range r.Ecosystem.Images {
				if !strings.HasPrefix(img.Ref, repo) {
					continue
				}
				switch {
				case img.Arch != "" && img.Arch != "arm64" && f.GOOS == "linux" && isArm(r):
					status, detail = StatusFail, fmt.Sprintf("%s is %s, not linux/arm64 (arm64-container-amd64-image)", img.Ref, img.Arch)
				case img.Ref == cmd.Image:
					status, detail = StatusPass, img.Ref+" present"
				default:
					status, detail = StatusWarn, fmt.Sprintf("%s present but the spec names %s", img.Ref, cmd.Image)
				}
				if status == StatusPass {
					break
				}
			}
			add("container-image", status, detail)
		} else {
			add("container-image", StatusSkip, fmt.Sprintf("local image inventory not collected; the template uses %s", cmd.Image))
		}

		switch {
		case f.GOOS == "windows":
			add("docker-gpu-runtime", StatusWarn, "GPU containers for "+in.Runtime.Display()+" are not covered on Windows on Arm (spec 7.6: no win_arm64 torch wheels on the cu130 index as of 2026-09-02, S93); use llama.cpp")
		case r == nil || r.Linux == nil:
			add("docker-gpu-runtime", StatusSkip, "container runtime not probed")
		case r.Ecosystem != nil && r.Ecosystem.SnapDocker:
			add("docker-gpu-runtime", StatusFail, "docker is the snap package; GPU passthrough is blocked (docker-snap-gpu-blocked)")
		case r.Linux.ContainerRuntime == "":
			add("docker-gpu-runtime", StatusFail, "docker not found on PATH")
		case r.Linux.NVContainerToolkit != "" || (r.Ecosystem != nil && (r.Ecosystem.DockerCDI || hasNvidiaRuntime(r.Ecosystem.DockerRuntimes))):
			add("docker-gpu-runtime", StatusPass, fmt.Sprintf("%s with NVIDIA Container Toolkit %s", r.Linux.ContainerRuntime, orNA(r.Linux.NVContainerToolkit)))
		default:
			add("docker-gpu-runtime", StatusFail, r.Linux.ContainerRuntime+" found but no NVIDIA Container Toolkit (--gpus all will fail)")
		}

		if in.Runtime == RuntimeSGLang {
			add("ipc-shm", StatusPass, "template uses --ipc=host --shm-size 32g (spec 7.6)")
		} else {
			add("ipc-shm", StatusPass, "template uses --ipc=host (spec 7.6)")
		}
	}

	// MemAvailable >= W + KV + R (free VRAM on a discrete GPU).
	avail := availLabel(f.Pool)
	switch {
	case !s.FitsNowKnown:
		add("memavailable-fits", StatusSkip, avail+" unknown; only the design fit against the pool total was evaluated")
	case s.FitsNow:
		after := f.Pool.AvailableBytes - s.NowBytes
		if f.Pool.Unified && after < pressureWarnBytes {
			add("memavailable-fits", StatusWarn, fmt.Sprintf("MemAvailable %s covers W + KV + R = %s, but only %s would remain (< 8 GiB unified-memory-pressure WARN, spec 7.4)", fmtGiB(f.Pool.AvailableBytes), fmtGiB(s.NowBytes), fmtGiB(after)))
		} else {
			add("memavailable-fits", StatusPass, fmt.Sprintf("%s %s >= W + KV + R = %s", avail, fmtGiB(f.Pool.AvailableBytes), fmtGiB(s.NowBytes)))
		}
	default:
		add("memavailable-fits", StatusFail, fmt.Sprintf("%s %s < W + KV + R = %s right now; free memory (other servers, caches) before starting", avail, fmtGiB(f.Pool.AvailableBytes), fmtGiB(s.NowBytes)))
	}

	// Ollama q8_0 KV needs an FA-capable architecture (spec 7.6).
	if in.Runtime == RuntimeOllama && in.KV == KVQ8_0 {
		if OllamaSupportsQ8KV(in.Model.OllamaArch) {
			add("ollama-kv-q8-arch", StatusPass, fmt.Sprintf("architecture %s honours OLLAMA_KV_CACHE_TYPE=q8_0", in.Model.OllamaArch))
		} else {
			add("ollama-kv-q8-arch", StatusWarn, fmt.Sprintf("architecture %q is not FA-capable in Ollama; q8_0 KV silently falls back to f16 (spec 7.6)", in.Model.OllamaArch))
		}
	}

	// ConnectX-7 link for the two-node target (spec 9).
	if in.Nodes >= 2 {
		out = append(out, cx7Check(r))
	}
	return out
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

func isArm(r *types.Report) bool {
	a := strings.ToLower(r.System.Architecture)
	return strings.Contains(a, "aarch64") || strings.Contains(a, "arm64")
}

func hasNvidiaRuntime(rts []string) bool {
	for _, x := range rts {
		if strings.Contains(strings.ToLower(x), "nvidia") {
			return true
		}
	}
	return false
}

// cx7Check evaluates the spec 9 healthy-fabric criteria from the cluster
// collector: both twins of the cabled cage ACTIVE/LinkUp at 200000 Mb/s,
// MTU 9000.
func cx7Check(r *types.Report) Prereq {
	if r == nil || r.Cluster == nil || len(r.Cluster.Ports) == 0 {
		return Prereq{ID: "cx7-link", Status: StatusFail, Detail: "no ConnectX-7 ports enumerated (cx7-not-enumerated); the two-node target needs the QSFP fabric (spec 9)"}
	}
	var active, slow, smallMTU int
	var devs []string
	for _, p := range r.Cluster.Ports {
		up := strings.Contains(p.State, "ACTIVE") && strings.Contains(p.PhysState, "LinkUp")
		if !up {
			continue
		}
		active++
		devs = append(devs, p.RDMADev)
		if p.SpeedMbps != 200000 {
			slow++
		}
		if p.MTU != 9000 {
			smallMTU++
		}
	}
	sort.Strings(devs)
	switch {
	case active == 0:
		return Prereq{ID: "cx7-link", Status: StatusFail, Detail: fmt.Sprintf("%d ConnectX-7 port(s) enumerated, none ACTIVE/LinkUp; cable the QSFP cage (spec 9)", len(r.Cluster.Ports))}
	case active < 2:
		return Prereq{ID: "cx7-link", Status: StatusWarn, Detail: fmt.Sprintf("only one twin (%s) is ACTIVE; a cabled cage shows both twins up (spec 9)", strings.Join(devs, ","))}
	case slow > 0 || smallMTU > 0:
		return Prereq{ID: "cx7-link", Status: StatusWarn, Detail: fmt.Sprintf("%s ACTIVE but %d port(s) not at 200000 Mb/s and %d not at MTU 9000 (spec 9)", strings.Join(devs, ","), slow, smallMTU)}
	}
	return Prereq{ID: "cx7-link", Status: StatusPass, Detail: fmt.Sprintf("%s ACTIVE/LinkUp at 200000 Mb/s, MTU 9000", strings.Join(devs, ","))}
}

// ActiveRDMADevs lists the RDMA devices the cluster collector saw ACTIVE, for
// NCCL_IB_HCA (spec 9: never a hard-coded port).
func ActiveRDMADevs(r *types.Report) []string {
	if r == nil || r.Cluster == nil {
		return nil
	}
	var devs []string
	for _, p := range r.Cluster.Ports {
		if strings.Contains(p.State, "ACTIVE") && p.RDMADev != "" {
			devs = append(devs, p.RDMADev)
		}
	}
	sort.Strings(devs)
	return devs
}

// WorstStatus folds a prerequisite list into one status.
func WorstStatus(ps []Prereq) string {
	worst := StatusPass
	for _, p := range ps {
		switch p.Status {
		case StatusFail:
			return StatusFail
		case StatusWarn:
			worst = StatusWarn
		}
	}
	return worst
}

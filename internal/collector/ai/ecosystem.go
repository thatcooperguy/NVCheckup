package ai

// Ecosystem collector for DGX Spark / RTX Spark (spec WP1 item 8, rule
// catalog sm121-*, arm64-*, docker-*, onnxruntime-*). Everything is optional
// and bounded: one Python probe (isolated mode, timeout+10 s like the
// TensorFlow probe), a few docker/ptxas invocations, and file reads under
// NVC_SIM_ROOT (spec section 10). Nothing here can fail the phase.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	linuxCollector "github.com/thatcooperguy/nvcheckup/internal/collector/linux"
	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const (
	// dockerDaemonJSON carries runtimes and features.cdi (rule docker-cdi-spec-missing).
	dockerDaemonJSON = "/etc/docker/daemon.json"
	// snapDockerDir exists when Docker was installed from snap (rule docker-snap-gpu-blocked).
	snapDockerDir = "/snap/docker"
	// tritonPtxasEnv overrides Triton's bundled ptxas (spec 7.6, rule sm121-triton-ptxas-stale).
	tritonPtxasEnv = "TRITON_PTXAS_PATH"
	// maxImagesInspected bounds the docker image enumeration.
	maxImagesInspected = 40
)

// cdiSpecFiles are the CDI specs docker needs when CDI is enabled (rule
// docker-cdi-spec-missing: /etc/cdi/nvidia.yaml or /var/run/cdi/nvidia.yaml).
var cdiSpecFiles = []string{"/etc/cdi/nvidia.yaml", "/var/run/cdi/nvidia.yaml"}

// inferencePorts are the listeners that matter on a Spark (spec WP1 item 8
// and 7.7): vLLM 8000, SGLang/llama.cpp 30000, Ollama 11434, TRT-LLM 8355,
// DGX Dashboard 11000, Neo4j 7474.
var inferencePorts = []int{8000, 30000, 11434, 8355, 11000, 7474}

// libcudartGlobs are the on-disk locations searched for the CUDA runtime
// (rule arm64-cuda12-wheel-on-cuda13: libcudart.so.12 vs .13).
var libcudartGlobs = []string{
	"/usr/local/cuda*/lib64/libcudart.so.*",
	"/usr/local/cuda*/targets/*/lib/libcudart.so.*",
	"/usr/lib/aarch64-linux-gnu/libcudart.so.*",
	"/usr/lib/x86_64-linux-gnu/libcudart.so.*",
	"/usr/lib64/libcudart.so.*",
}

// simPath prefixes an absolute path with NVC_SIM_ROOT when set (spec section
// 10). Local copy; the integrator may swap in the common helper.
func simPath(p string) string {
	root := os.Getenv("NVC_SIM_ROOT")
	if root == "" {
		return p
	}
	return strings.TrimRight(root, "/") + p
}

// CollectEcosystem gathers the AI software-ecosystem facts of EcosystemInfo.
func CollectEcosystem(timeout int) (types.EcosystemInfo, []types.CollectorError) {
	var info types.EcosystemInfo
	var errs []types.CollectorError

	info.TritonPtxasPath = os.Getenv(tritonPtxasEnv)
	if python := quietPython(timeout); python != "" {
		collectPythonEcosystem(&info, &errs, timeout, python)
	}
	collectLibcudart(&info)
	collectDocker(&info, &errs, timeout)
	info.ListeningPorts = filterPorts(linuxCollector.ListeningTCPPorts(), inferencePorts)

	return info, errs
}

// quietPython picks the interpreter like findPython but records no error:
// CollectAIInfo already explains a missing Python once.
func quietPython(timeout int) string {
	probe := func(cmd string) (bool, bool) {
		if !util.CommandExists(cmd) {
			return false, false
		}
		r := util.RunCommand(timeout, cmd, "-I", "-c", "import sys; print(sys.version_info[0])")
		return true, r.Err == nil && strings.TrimSpace(r.Stdout) == "3"
	}
	python, _ := selectPython(pythonCandidates(), probe)
	return python
}

// ecosystemProbeScript is the single Python probe. Distribution versions come
// from importlib.metadata so heavy modules (flash_attn) are never imported;
// torch and onnxruntime are imported because their runtime answers
// (get_arch_list, get_available_providers) are what the rules need.
const ecosystemProbeScript = `
import json, glob, os, site, importlib.util
out = {}
def dist_version(*names):
    try:
        from importlib import metadata
    except Exception:
        return ""
    for n in names:
        try:
            return metadata.version(n)
        except Exception:
            pass
    return ""
try:
    import torch
    t = {"version": torch.__version__, "cuda": getattr(torch.version, "cuda", None) or "", "arch_list": []}
    try:
        # get_arch_list() does not initialise CUDA; the capability warning of
        # rule sm121-torch-capability-warning-benign ("Found GPU0 NVIDIA GB10
        # which is of cuda capability 12.1 ...", spec 3.2) is emitted by
        # _lazy_init/_check_capability, so initialise first (stderr keeps it).
        if torch.cuda.is_available():
            torch.cuda.init()
    except Exception:
        pass
    try:
        t["arch_list"] = list(torch.cuda.get_arch_list())
    except Exception:
        pass
    out["torch"] = t
except ImportError:
    pass
except Exception as e:
    out["torch_error"] = str(e)
try:
    spec = importlib.util.find_spec("triton")
    if spec is not None and spec.submodule_search_locations:
        base = list(spec.submodule_search_locations)[0]
        cands = glob.glob(os.path.join(base, "backends", "nvidia", "bin", "ptxas*")) + glob.glob(os.path.join(base, "third_party", "cuda", "bin", "ptxas*"))
        out["triton"] = {"version": dist_version("triton"), "ptxas": cands[0] if cands else ""}
except Exception:
    pass
out["flash_attn"] = dist_version("flash_attn", "flash-attn", "vllm-flash-attn")
out["ort"] = dist_version("onnxruntime")
out["ort_gpu"] = dist_version("onnxruntime-gpu")
if out["ort"] or out["ort_gpu"]:
    try:
        import onnxruntime as ort
        out["ort_providers"] = list(ort.get_available_providers())
        out["ort_version"] = ort.__version__
    except Exception as e:
        out["ort_error"] = str(e)
found = []
try:
    dirs = list(site.getsitepackages()) + [site.getusersitepackages()]
    for d in dirs:
        found += glob.glob(os.path.join(d, "nvidia", "cuda_runtime", "lib", "libcudart.so*"))
        found += glob.glob(os.path.join(d, "nvidia", "cu13", "lib", "libcudart.so*"))
except Exception:
    pass
out["libcudart"] = found
print(json.dumps(out))
`

// ecosystemProbe mirrors the JSON printed by ecosystemProbeScript.
type ecosystemProbe struct {
	Torch *struct {
		Version  string   `json:"version"`
		CUDA     string   `json:"cuda"`
		ArchList []string `json:"arch_list"`
	} `json:"torch"`
	TorchError string `json:"torch_error"`
	Triton     *struct {
		Version string `json:"version"`
		Ptxas   string `json:"ptxas"`
	} `json:"triton"`
	FlashAttn    string   `json:"flash_attn"`
	ORT          string   `json:"ort"`
	ORTGPU       string   `json:"ort_gpu"`
	ORTProviders []string `json:"ort_providers"`
	ORTVersion   string   `json:"ort_version"`
	ORTError     string   `json:"ort_error"`
	Libcudart    []string `json:"libcudart"`
}

// parseEcosystemProbe decodes the probe's JSON (the last line of stdout, so
// stray prints from site hooks are ignored).
func parseEcosystemProbe(stdout string) (ecosystemProbe, error) {
	var p ecosystemProbe
	err := json.Unmarshal([]byte(lastLine(stdout)), &p)
	return p, err
}

// applyEcosystemProbe copies probe results into EcosystemInfo. torchStderr is
// the probe's stderr, which carries the capability warning of rule
// sm121-torch-capability-warning-benign.
func applyEcosystemProbe(info *types.EcosystemInfo, p ecosystemProbe, torchStderr string) {
	if p.Torch != nil {
		info.TorchArchList = p.Torch.ArchList
	}
	info.TorchWarnings = parseTorchWarnings(torchStderr)
	if p.TorchError != "" {
		info.TorchWarnings = append(info.TorchWarnings, "torch import error: "+p.TorchError)
	}
	info.FlashAttnVersion = p.FlashAttn
	switch {
	case p.ORTVersion != "":
		info.ORTVersion = p.ORTVersion
	case p.ORTGPU != "":
		info.ORTVersion = p.ORTGPU
	default:
		info.ORTVersion = p.ORT
	}
	info.ORTProviders = p.ORTProviders
	hasCUDA := false
	for _, prov := range p.ORTProviders {
		if prov == "CUDAExecutionProvider" {
			hasCUDA = true
		}
	}
	// Both distributions installed and no CUDA provider: the CPU wheel shadows
	// the GPU build (rule onnxruntime-cuda-provider-missing).
	info.ORTGPUShadowed = p.ORT != "" && p.ORTGPU != "" && !hasCUDA
	info.LibcudartVersions = mergeLibcudart(info.LibcudartVersions, p.Libcudart)
}

func collectPythonEcosystem(info *types.EcosystemInfo, errs *[]types.CollectorError, timeout int, python string) {
	// torch and onnxruntime imports are slow; allow the TensorFlow-style margin.
	r := util.RunCommand(timeout+10, python, "-I", "-c", ecosystemProbeScript)
	if r.Err != nil || strings.TrimSpace(r.Stdout) == "" {
		msg := "ecosystem probe did not run using interpreter " + python
		if r.Err != nil {
			msg += ": " + r.Err.Error()
		}
		if s := lastLine(r.Stderr); s != "" {
			msg += " (" + s + ")"
		}
		*errs = append(*errs, types.CollectorError{Collector: "ai.ecosystem", Error: msg})
		return
	}
	p, err := parseEcosystemProbe(r.Stdout)
	if err != nil {
		*errs = append(*errs, types.CollectorError{Collector: "ai.ecosystem", Error: "could not parse ecosystem probe output: " + err.Error()})
		return
	}
	applyEcosystemProbe(info, p, r.Stderr)
	if p.Triton != nil && p.Triton.Ptxas != "" && info.TritonPtxasPath == "" {
		// No override: report the bundled binary that Triton will use.
		info.TritonPtxasPath = p.Triton.Ptxas
	}
	// TritonPtxasVersion is the version of the ptxas Triton will actually run:
	// the TRITON_PTXAS_PATH override when set, otherwise the bundled binary.
	// Rule sm121-triton-ptxas-stale ("bundled ptxas < 13.0 AND
	// TRITON_PTXAS_PATH unset") therefore reduces to TritonPtxasVersion < 13.0:
	// a user who exported a CUDA 13 ptxas reports that version here.
	if p.Triton != nil && info.TritonPtxasPath != "" {
		pr := util.RunCommand(timeout, info.TritonPtxasPath, "--version")
		if pr.Err == nil {
			info.TritonPtxasVersion = parseNvccVersion(pr.Stdout)
		}
	}
}

// torchWarningStartRe matches the first line of a Python warning
// ("/path/torch/cuda/__init__.py:262: UserWarning: text").
var torchWarningStartRe = regexp.MustCompile(`^\S+:\d+:\s*\w*Warning:\s*(.*)$`)

// parseTorchWarnings extracts warnings from a Python probe's stderr. Python
// prints "<file>:<line>: UserWarning: <text>" followed by indented
// continuation lines and a "warnings.warn(" trailer; the block is joined into
// one line so the spec string "Found GPU0 NVIDIA GB10 which is of cuda
// capability 12.1. Minimum and Maximum cuda capability supported by this
// version of PyTorch is (8.0) - (12.0)" survives intact. Unrelated stderr
// noise is dropped.
func parseTorchWarnings(stderr string) []string {
	var warnings []string
	var cur []string
	flush := func() {
		if text := strings.Join(strings.Fields(strings.Join(cur, " ")), " "); text != "" {
			warnings = append(warnings, text)
		}
		cur = nil
	}
	inBlock := false
	for _, raw := range strings.Split(stderr, "\n") {
		line := strings.TrimRight(raw, "\r")
		if m := torchWarningStartRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if inBlock {
				flush()
			}
			inBlock = true
			cur = append(cur, m[1])
			continue
		}
		if !inBlock {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "warnings.warn("), trimmed == "":
			flush()
			inBlock = false
		case strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"):
			cur = append(cur, trimmed)
		default:
			flush()
			inBlock = false
		}
	}
	if inBlock {
		flush()
	}
	return dedupeStrings(warnings)
}

// libcudartMajorRe extracts the SONAME major of a libcudart path.
var libcudartMajorRe = regexp.MustCompile(`libcudart\.so\.(\d+)`)

// mergeLibcudart adds the "libcudart.so.<major>" names of paths to the
// existing list, sorted and de-duplicated.
func mergeLibcudart(existing []string, paths []string) []string {
	set := map[string]bool{}
	for _, e := range existing {
		set[e] = true
	}
	for _, p := range paths {
		if m := libcudartMajorRe.FindStringSubmatch(filepath.Base(p)); m != nil {
			set["libcudart.so."+m[1]] = true
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// collectLibcudart records which CUDA runtime majors exist on disk.
func collectLibcudart(info *types.EcosystemInfo) {
	var paths []string
	for _, g := range libcudartGlobs {
		m, _ := filepath.Glob(simPath(g))
		paths = append(paths, m...)
	}
	info.LibcudartVersions = mergeLibcudart(info.LibcudartVersions, paths)
}

// dockerDaemonConfig is the subset of /etc/docker/daemon.json we read.
type dockerDaemonConfig struct {
	Runtimes map[string]json.RawMessage `json:"runtimes"`
	Features map[string]bool            `json:"features"`
}

// parseDockerDaemonJSON returns the runtime names (sorted) and whether
// features.cdi is enabled.
func parseDockerDaemonJSON(content string) (runtimes []string, cdi bool, err error) {
	var cfg dockerDaemonConfig
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, false, err
	}
	for name := range cfg.Runtimes {
		runtimes = append(runtimes, name)
	}
	sort.Strings(runtimes)
	return runtimes, cfg.Features["cdi"], nil
}

// parseDockerImageList parses "docker image ls --format {{.Repository}}:{{.Tag}}"
// rows, dropping dangling "<none>" entries and capping the list.
func parseDockerImageList(out string) []string {
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "<none>") {
			continue
		}
		refs = append(refs, line)
		if len(refs) >= maxImagesInspected {
			break
		}
	}
	return refs
}

// pairImageArchitectures zips image refs with the lines of
// "docker image inspect --format {{.Architecture}} refs...". A short answer
// (inspect failed part-way) leaves the remaining architectures empty.
func pairImageArchitectures(refs []string, inspectOut string) []types.ContainerImage {
	archs := strings.Split(strings.TrimSpace(inspectOut), "\n")
	images := make([]types.ContainerImage, 0, len(refs))
	for i, ref := range refs {
		img := types.ContainerImage{Ref: ref}
		if i < len(archs) && len(archs) == len(refs) {
			img.Arch = strings.TrimSpace(archs[i])
		}
		images = append(images, img)
	}
	return images
}

func collectDocker(info *types.EcosystemInfo, errs *[]types.CollectorError, timeout int) {
	if data, err := os.ReadFile(simPath(dockerDaemonJSON)); err == nil {
		runtimes, cdi, perr := parseDockerDaemonJSON(string(data))
		if perr != nil {
			*errs = append(*errs, types.CollectorError{Collector: "ai.ecosystem.docker", Error: "daemon.json does not parse: " + perr.Error()})
		}
		info.DockerRuntimes = runtimes
		info.DockerCDI = cdi
	}
	for _, f := range cdiSpecFiles {
		if _, err := os.Stat(simPath(f)); err == nil {
			info.CDISpecPresent = true
		}
	}
	if _, err := os.Stat(simPath(snapDockerDir)); err == nil {
		info.SnapDocker = true
	} else if util.CommandExists("snap") {
		r := util.RunCommand(timeout, "snap", "list", "docker")
		info.SnapDocker = r.Err == nil && strings.Contains(r.Stdout, "docker")
	}

	if !util.CommandExists("docker") {
		return
	}
	r := util.RunCommand(timeout, "docker", "image", "ls", "--format", "{{.Repository}}:{{.Tag}}")
	if r.Err != nil {
		// Daemon down or socket not accessible for this user: a normal state
		// worth one line, never a failure of the phase.
		if s := lastLine(r.Stderr); s != "" {
			*errs = append(*errs, types.CollectorError{Collector: "ai.ecosystem.docker", Error: util.TruncateString(s, 160)})
		}
		return
	}
	refs := parseDockerImageList(r.Stdout)
	if len(refs) > 0 {
		ir := util.RunCommand(timeout, "docker", append([]string{"image", "inspect", "--format", "{{.Architecture}}"}, refs...)...)
		info.Images = pairImageArchitectures(refs, ir.Stdout)
	}
	if len(info.DockerRuntimes) == 0 {
		ir := util.RunCommand(timeout, "docker", "info", "--format", "{{json .Runtimes}}")
		if ir.Err == nil {
			var runtimes map[string]json.RawMessage
			if json.Unmarshal([]byte(strings.TrimSpace(ir.Stdout)), &runtimes) == nil {
				for name := range runtimes {
					info.DockerRuntimes = append(info.DockerRuntimes, name)
				}
				sort.Strings(info.DockerRuntimes)
			}
		}
	}
}

// filterPorts keeps the listening ports that are in the interesting set.
func filterPorts(listening, interesting []int) []int {
	want := map[int]bool{}
	for _, p := range interesting {
		want[p] = true
	}
	var out []int
	for _, p := range listening {
		if want[p] {
			out = append(out, p)
		}
	}
	return out
}

// dedupeStrings removes duplicates preserving order.
func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

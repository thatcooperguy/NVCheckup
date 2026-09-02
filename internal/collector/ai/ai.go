// Package ai provides collectors for AI/CUDA framework diagnostics.
package ai

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// CollectAIInfo gathers AI framework and CUDA environment information.
func CollectAIInfo(timeout int) (types.AIInfo, []types.CollectorError) {
	var info types.AIInfo
	var errs []types.CollectorError

	collectCUDAToolkit(&info, &errs, timeout)
	collectCuDNN(&info)
	collectPythonEnvs(&info, &errs, timeout)
	info.CondaPresent = util.CommandExists("conda")

	// One interpreter is chosen once and shared by every framework probe so
	// the report never mixes results from different Pythons.
	python := findPython(timeout, &errs)
	if python == "" {
		return info, errs
	}
	collectPyTorch(&info, &errs, timeout, python)
	collectTensorFlow(&info, &errs, timeout, python)
	collectKeyPackages(&info, &errs, timeout, python)

	return info, errs
}

// nvccReleaseRe matches the release field of "nvcc --version":
//
//	Cuda compilation tools, release 12.4, V12.4.131
var nvccReleaseRe = regexp.MustCompile(`release\s+(\d+(?:\.\d+)*)`)

// cudaDirVersionRe extracts the version from an install directory such as
// /usr/local/cuda-12.4 or /opt/cuda-12.4.
var cudaDirVersionRe = regexp.MustCompile(`cuda[- ]?(\d+(?:\.\d+)*)`)

// parseNvccVersion returns the toolkit release ("12.4") from nvcc --version
// output, or "" when the release line is missing.
func parseNvccVersion(output string) string {
	if m := nvccReleaseRe.FindStringSubmatch(output); m != nil {
		return m[1]
	}
	return ""
}

// cudaVersionFromPath extracts a toolkit version embedded in an install path
// ("/usr/local/cuda-12.4" -> "12.4"), or "" when the path carries none.
func cudaVersionFromPath(path string) string {
	if m := cudaDirVersionRe.FindStringSubmatch(path); m != nil {
		return m[1]
	}
	return ""
}

// linuxCudaHome is the conventional toolkit location on Linux. It is usually a
// symlink, and on Debian/Ubuntu often a symlink to /etc/alternatives/cuda
// which in turn points at the versioned directory, so it must be resolved
// fully before the version can be read from the path.
const linuxCudaHome = "/usr/local/cuda"

func collectCUDAToolkit(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	// Check nvcc on PATH
	if util.CommandExists("nvcc") {
		r := util.RunCommand(timeout, "nvcc", "--version")
		if r.Err == nil {
			info.NvccPath = "nvcc"
			info.CUDAToolkitVersion = parseNvccVersion(r.Stdout)
		}
	}

	// CUDA_HOME (the CUDA 13 toolkit convention on DGX OS, spec WP1 item 8)
	// is honoured before the /usr/local/cuda symlink on every OS.
	if cudaHome := os.Getenv("CUDA_HOME"); cudaHome != "" {
		nvccName := "nvcc"
		if runtime.GOOS == "windows" {
			nvccName = "nvcc.exe"
		}
		nvccPath := filepath.Join(cudaHome, "bin", nvccName)
		if _, err := os.Stat(nvccPath); err == nil {
			if info.NvccPath == "" {
				info.NvccPath = nvccPath
			}
			if info.CUDAToolkitVersion == "" {
				r := util.RunCommand(timeout, nvccPath, "--version")
				if r.Err == nil {
					info.CUDAToolkitVersion = parseNvccVersion(r.Stdout)
				}
			}
		}
		if info.CUDAToolkitVersion == "" {
			info.CUDAToolkitVersion = cudaVersionFromPath(cudaHome)
		}
	}

	// On Linux, follow /usr/local/cuda (through /etc/alternatives when present)
	if runtime.GOOS == "linux" {
		// Through NVC_SIM_ROOT (spec section 10); the resolved target is
		// already a real path and is used as such below.
		if target, err := filepath.EvalSymlinks(common.SimPath(linuxCudaHome)); err == nil {
			nvccPath := filepath.Join(target, "bin", "nvcc")
			if _, err := os.Stat(nvccPath); err == nil {
				if info.NvccPath == "" {
					info.NvccPath = nvccPath
				}
				if info.CUDAToolkitVersion == "" {
					r := util.RunCommand(timeout, nvccPath, "--version")
					if r.Err == nil {
						info.CUDAToolkitVersion = parseNvccVersion(r.Stdout)
					}
				}
			}
			if info.CUDAToolkitVersion == "" {
				info.CUDAToolkitVersion = cudaVersionFromPath(target)
			}
		}
	}

	// On Windows, check common CUDA install locations
	if runtime.GOOS == "windows" {
		cudaPath := os.Getenv("CUDA_PATH")
		if cudaPath != "" {
			nvccPath := filepath.Join(cudaPath, "bin", "nvcc.exe")
			if _, err := os.Stat(nvccPath); err == nil {
				if info.NvccPath == "" {
					info.NvccPath = nvccPath
				}
				if info.CUDAToolkitVersion == "" {
					r := util.RunCommand(timeout, nvccPath, "--version")
					if r.Err == nil {
						info.CUDAToolkitVersion = parseNvccVersion(r.Stdout)
					}
				}
			}
		}
	}
}

// cudnnDefineRe matches "#define CUDNN_MAJOR 9" style lines. Anchoring on the
// #define keyword keeps "#define CUDNN_VERSION (CUDNN_MAJOR * 1000 + ...)"
// from matching.
var cudnnDefineRe = regexp.MustCompile(`(?m)^\s*#\s*define\s+CUDNN_(MAJOR|MINOR|PATCHLEVEL)\s+(\d+)\b`)

// parseCudnnHeader extracts "major.minor.patch" from the contents of
// cudnn_version.h (cuDNN 8+) or cudnn.h (cuDNN 7 and older). It returns ""
// when the header defines no CUDNN_MAJOR.
func parseCudnnHeader(content string) string {
	found := map[string]string{}
	for _, m := range cudnnDefineRe.FindAllStringSubmatch(content, -1) {
		if _, dup := found[m[1]]; !dup {
			found[m[1]] = m[2]
		}
	}
	major, ok := found["MAJOR"]
	if !ok {
		return ""
	}
	version := major
	minor, ok := found["MINOR"]
	if !ok {
		return version
	}
	version += "." + minor
	if patch, ok := found["PATCHLEVEL"]; ok {
		version += "." + patch
	}
	return version
}

// collectCuDNN reads the cuDNN header directly with os.ReadFile. The previous
// implementation interpolated CUDA_PATH into a PowerShell command line, which
// let a crafted environment variable ($(...) or a stray quote) run arbitrary
// commands; reading the file from Go involves no shell at all.
func collectCuDNN(info *types.AIInfo) {
	for _, path := range cudnnHeaderCandidates() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if v := parseCudnnHeader(string(data)); v != "" {
			info.CuDNNVersion = v
			return
		}
	}
}

// cudnnHeaderCandidates lists header files in priority order: CUDA_PATH
// first, then the standard toolkit and standalone cuDNN install locations.
func cudnnHeaderCandidates() []string {
	var dirs []string
	if p := os.Getenv("CUDA_PATH"); p != "" {
		dirs = append(dirs, filepath.Join(p, "include"))
	}
	if runtime.GOOS == "windows" {
		programFiles := os.Getenv("ProgramFiles")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		dirs = append(dirs, globPaths(filepath.Join(programFiles, "NVIDIA GPU Computing Toolkit", "CUDA", "v*", "include"))...)
		// Standalone cuDNN installs; cuDNN 9 adds a per-CUDA-version
		// subfolder (include\12.x\cudnn_version.h).
		dirs = append(dirs, globPaths(filepath.Join(programFiles, "NVIDIA", "CUDNN", "v*", "include"))...)
		dirs = append(dirs, globPaths(filepath.Join(programFiles, "NVIDIA", "CUDNN", "v*", "include", "*"))...)
	} else {
		// Mapped through NVC_SIM_ROOT once, here; every candidate below is
		// therefore a real (already mapped) path and is read as is.
		for _, d := range []string{"/usr/include", "/usr/local/cuda/include"} {
			dirs = append(dirs, common.SimPath(d))
		}
		dirs = append(dirs, globPaths(common.SimPath("/usr/local/cuda-*/include"))...)
		for _, d := range []string{"/usr/include/x86_64-linux-gnu", "/usr/include/aarch64-linux-gnu"} {
			dirs = append(dirs, common.SimPath(d))
		}
	}

	var files []string
	for _, d := range dirs {
		files = append(files, filepath.Join(d, "cudnn_version.h"), filepath.Join(d, "cudnn.h"))
		// Debian/Ubuntu packages install versioned names such as cudnn_version_v9.h.
		files = append(files, globPaths(filepath.Join(d, "cudnn_version_v*.h"))...)
	}
	return files
}

func globPaths(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func collectPythonEnvs(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	pythonCmds := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		pythonCmds = []string{"python", "python3", "py"}
	}

	seen := make(map[string]bool)
	for _, cmd := range pythonCmds {
		if !util.CommandExists(cmd) {
			continue
		}
		r := util.RunCommand(timeout, cmd, "--version")
		if r.Err == nil {
			version := strings.TrimSpace(r.Stdout + r.Stderr) // Python 2 outputs to stderr
			version = strings.TrimPrefix(version, "Python ")
			if !seen[version] {
				seen[version] = true

				// Get path
				var pathCmd string
				if runtime.GOOS == "windows" {
					pathCmd = "where"
				} else {
					pathCmd = "which"
				}
				rPath := util.RunCommand(timeout, pathCmd, cmd)
				path := strings.TrimSpace(rPath.Stdout)
				if path != "" {
					// Take first line only (where on Windows can return multiple)
					path = strings.Split(path, "\n")[0]
				}

				info.PythonVersions = append(info.PythonVersions, types.PythonEnv{
					Path:    strings.TrimSpace(path),
					Version: version,
				})
			}
		}
	}
}

// pythonCandidates lists the interpreter names tried, in order of preference.
func pythonCandidates() []string {
	if runtime.GOOS == "windows" {
		return []string{"python", "python3", "py"}
	}
	return []string{"python3", "python"}
}

// pythonProbe reports whether cmd exists on PATH and, if so, whether it runs
// Python 3 in isolated mode.
type pythonProbe func(cmd string) (exists, works bool)

// findPython returns the first interpreter that actually runs Python 3 in
// isolated mode. util.CommandExists alone is not enough: on Windows the
// Microsoft Store "python3" alias is a stub that only prints an install hint,
// and Python 2 does not accept -I, which every probe relies on. When at least
// one candidate exists on PATH but none of them works, a CollectorError is
// recorded so the empty PyTorch/TensorFlow sections are explained instead of
// silently blank.
func findPython(timeout int, errs *[]types.CollectorError) string {
	probe := func(cmd string) (bool, bool) {
		if !util.CommandExists(cmd) {
			return false, false
		}
		r := util.RunCommand(timeout, cmd, "-I", "-c", "import sys; print(sys.version_info[0])")
		return true, r.Err == nil && strings.TrimSpace(r.Stdout) == "3"
	}
	python, tried := selectPython(pythonCandidates(), probe)
	if python == "" && len(tried) > 0 {
		*errs = append(*errs, noWorkingPythonError(tried))
	}
	return python
}

// noWorkingPythonError describes candidates that exist on PATH but do not run
// Python 3 (Microsoft Store stubs, Python 2), so the report explains why the
// framework sections are empty.
func noWorkingPythonError(tried []string) types.CollectorError {
	return types.CollectorError{
		Collector: "ai.python",
		Error:     "no working Python 3 interpreter (tried: " + strings.Join(tried, ", ") + "); PyTorch/TensorFlow checks skipped",
	}
}

// selectPython returns the first candidate that probe reports as working,
// together with the candidates that existed on PATH but failed the probe
// (Microsoft Store stubs, Python 2). It is pure so it can be unit-tested.
func selectPython(candidates []string, probe pythonProbe) (python string, tried []string) {
	for _, cmd := range candidates {
		exists, works := probe(cmd)
		if !exists {
			continue
		}
		if works {
			return cmd, tried
		}
		tried = append(tried, cmd)
	}
	return "", tried
}

// runProbe executes a Python snippet in isolated mode (-I). Isolated mode
// ignores PYTHONPATH, user site-packages and, crucially, the current working
// directory, so a stray torch.py or json.py next to the user cannot be
// imported in place of the real module. A probe that fails to run at all (as
// opposed to reporting an ImportError inside its own JSON) is recorded as a
// CollectorError naming the interpreter, so empty fields are never silent.
func runProbe(timeout int, python, collector, script string, errs *[]types.CollectorError) (string, bool) {
	out, _, ok := runProbeFull(timeout, python, collector, script, errs)
	return out, ok
}

// runProbeFull is runProbe that also returns the probe's stderr, which the
// PyTorch probe needs for the capability warning (spec 3.2 "Ecosystem").
func runProbeFull(timeout int, python, collector, script string, errs *[]types.CollectorError) (stdout, stderr string, ok bool) {
	r := util.RunCommand(timeout, python, "-I", "-c", script)
	out := strings.TrimSpace(r.Stdout)
	if r.Err == nil && out != "" {
		return out, r.Stderr, true
	}
	msg := "probe did not run using interpreter " + python
	if r.Err != nil {
		msg += ": " + r.Err.Error()
	}
	if s := lastLine(r.Stderr); s != "" {
		msg += " (" + s + ")"
	}
	*errs = append(*errs, types.CollectorError{Collector: collector, Error: msg})
	return "", r.Stderr, false
}

func collectPyTorch(info *types.AIInfo, errs *[]types.CollectorError, timeout int, python string) {
	script := `
import json, sys
try:
    import torch
    result = {
        "version": torch.__version__,
        "cuda_version": getattr(torch.version, 'cuda', None) or "",
        "cuda_available": torch.cuda.is_available(),
        "device_name": "",
        "arch_list": []
    }
    if torch.cuda.is_available() and torch.cuda.device_count() > 0:
        try:
            result["device_name"] = torch.cuda.get_device_name(0)
        except Exception:
            pass
    try:
        result["arch_list"] = list(torch.cuda.get_arch_list())
    except Exception:
        pass
    print(json.dumps(result))
except ImportError:
    print(json.dumps({"error": "not_installed"}))
except Exception as e:
    print(json.dumps({"error": str(e)}))
`

	stdout, stderr, ok := runProbeFull(timeout, python, "ai.pytorch", script, errs)
	if !ok {
		return
	}
	ptInfo := &types.PyTorchInfo{}
	// torch prints the sm_121 capability warning of spec 3.2 on stderr
	// ("Found GPU0 NVIDIA GB10 which is of cuda capability 12.1. ...").
	ptInfo.Warnings = parseTorchWarnings(stderr)
	if strings.Contains(stdout, `"error"`) {
		if strings.Contains(stdout, "not_installed") {
			// PyTorch not installed: a normal state, not an error.
			return
		}
		ptInfo.Error = extractJSONValue(stdout, "error")
	} else {
		ptInfo.Version = extractJSONValue(stdout, "version")
		ptInfo.CUDAVersion = extractJSONValue(stdout, "cuda_version")
		ptInfo.CUDAAvailable = strings.Contains(stdout, `"cuda_available": true`)
		ptInfo.DeviceName = extractJSONValue(stdout, "device_name")
		// torch.cuda.get_arch_list(), e.g. ["sm_80", ..., "sm_120"]; rules
		// sm121-kernel-missing / sm121-torch-capability-warning-benign.
		ptInfo.ArchList = extractJSONStringList(stdout, "arch_list")
	}
	info.PyTorchInfo = ptInfo
}

func collectTensorFlow(info *types.AIInfo, errs *[]types.CollectorError, timeout int, python string) {
	script := `
import json, sys
try:
    import tensorflow as tf
    gpus = []
    try:
        physical_gpus = tf.config.list_physical_devices('GPU')
        gpus = [g.name for g in physical_gpus]
    except Exception:
        pass
    print(json.dumps({"version": tf.__version__, "gpus": gpus}))
except ImportError:
    print(json.dumps({"error": "not_installed"}))
except Exception as e:
    print(json.dumps({"error": str(e)}))
`

	// Importing TensorFlow is slow; allow extra time.
	stdout, ok := runProbe(timeout+10, python, "ai.tensorflow", script, errs)
	if !ok {
		return
	}
	tfInfo := &types.TFInfo{}
	if strings.Contains(stdout, `"error"`) {
		if strings.Contains(stdout, "not_installed") {
			return
		}
		tfInfo.Error = extractJSONValue(stdout, "error")
	} else {
		tfInfo.Version = extractJSONValue(stdout, "version")
		gpuRe := regexp.MustCompile(`"gpus":\s*\[([^\]]*)\]`)
		if m := gpuRe.FindStringSubmatch(stdout); m != nil {
			itemRe := regexp.MustCompile(`"([^"]+)"`)
			for _, gm := range itemRe.FindAllStringSubmatch(m[1], -1) {
				tfInfo.GPUs = append(tfInfo.GPUs, gm[1])
			}
		}
	}
	info.TensorFlowInfo = tfInfo
}

func collectKeyPackages(info *types.AIInfo, errs *[]types.CollectorError, timeout int, python string) {
	script := `
import json
packages = {}
for pkg in ["torch", "tensorflow", "jax", "onnxruntime", "transformers", "numpy", "scipy"]:
    try:
        mod = __import__(pkg)
        packages[pkg] = getattr(mod, "__version__", "unknown")
    except ImportError:
        pass
print(json.dumps(packages))
`

	stdout, ok := runProbe(timeout, python, "ai.packages", script, errs)
	if !ok {
		return
	}
	pairRe := regexp.MustCompile(`"(\w+)":\s*"([^"]*)"`)
	for _, m := range pairRe.FindAllStringSubmatch(stdout, -1) {
		info.KeyPackages = append(info.KeyPackages, types.PackageInfo{
			Name:    m[1],
			Version: m[2],
		})
	}
}

// extractJSONStringList returns the string items of a JSON array value
// ("key": ["a", "b"]) or nil when the key is absent or the array is empty.
func extractJSONStringList(jsonStr, key string) []string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `":\s*\[([^\]]*)\]`)
	m := re.FindStringSubmatch(jsonStr)
	if m == nil {
		return nil
	}
	itemRe := regexp.MustCompile(`"([^"]*)"`)
	var items []string
	for _, im := range itemRe.FindAllStringSubmatch(m[1], -1) {
		items = append(items, im[1])
	}
	return items
}

func extractJSONValue(jsonStr, key string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `":\s*"([^"]*)"`)
	if m := re.FindStringSubmatch(jsonStr); m != nil {
		return m[1]
	}
	return ""
}

// lastLine returns the last non-empty line of s; for a Python traceback that
// is the exception message.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

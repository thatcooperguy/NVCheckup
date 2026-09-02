// Package ai provides collectors for AI/CUDA framework diagnostics.
package ai

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

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
	python := findPython(timeout)
	if python == "" {
		return info, errs
	}
	collectPyTorch(&info, &errs, timeout, python)
	collectTensorFlow(&info, &errs, timeout, python)
	collectKeyPackages(&info, &errs, timeout, python)

	return info, errs
}

func collectCUDAToolkit(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	// Check nvcc
	if util.CommandExists("nvcc") {
		r := util.RunCommand(timeout, "nvcc", "--version")
		if r.Err == nil {
			info.NvccPath = "nvcc"
			// Parse version: "Cuda compilation tools, release 12.2, V12.2.140"
			re := regexp.MustCompile(`release\s+([\d.]+)`)
			if m := re.FindStringSubmatch(r.Stdout); m != nil {
				info.CUDAToolkitVersion = m[1]
			}
		}
	}

	// On Linux, check /usr/local/cuda symlink
	if runtime.GOOS == "linux" {
		if target, err := os.Readlink("/usr/local/cuda"); err == nil {
			if info.CUDAToolkitVersion == "" {
				// Extract version from path like /usr/local/cuda-12.2
				re := regexp.MustCompile(`cuda[- ]?([\d.]+)`)
				if m := re.FindStringSubmatch(target); m != nil {
					info.CUDAToolkitVersion = m[1]
				}
			}
			if info.NvccPath == "" {
				nvccPath := filepath.Join(target, "bin", "nvcc")
				if _, err := os.Stat(nvccPath); err == nil {
					info.NvccPath = nvccPath
				}
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
				r := util.RunCommand(timeout, nvccPath, "--version")
				if r.Err == nil && info.CUDAToolkitVersion == "" {
					re := regexp.MustCompile(`release\s+([\d.]+)`)
					if m := re.FindStringSubmatch(r.Stdout); m != nil {
						info.CUDAToolkitVersion = m[1]
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
		dirs = append(dirs, "/usr/include", "/usr/local/cuda/include")
		dirs = append(dirs, globPaths("/usr/local/cuda-*/include")...)
		dirs = append(dirs, "/usr/include/x86_64-linux-gnu", "/usr/include/aarch64-linux-gnu")
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

// findPython returns the first interpreter that actually runs Python 3 in
// isolated mode. util.CommandExists alone is not enough: on Windows the
// Microsoft Store "python3" alias is a stub that only prints an install hint,
// and Python 2 does not accept -I, which every probe relies on.
func findPython(timeout int) string {
	candidates := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		candidates = []string{"python", "python3", "py"}
	}
	for _, cmd := range candidates {
		if !util.CommandExists(cmd) {
			continue
		}
		r := util.RunCommand(timeout, cmd, "-I", "-c", "import sys; print(sys.version_info[0])")
		if r.Err == nil && strings.TrimSpace(r.Stdout) == "3" {
			return cmd
		}
	}
	return ""
}

// runProbe executes a Python snippet in isolated mode (-I). Isolated mode
// ignores PYTHONPATH, user site-packages and, crucially, the current working
// directory, so a stray torch.py or json.py next to the user cannot be
// imported in place of the real module. A probe that fails to run at all (as
// opposed to reporting an ImportError inside its own JSON) is recorded as a
// CollectorError naming the interpreter, so empty fields are never silent.
func runProbe(timeout int, python, collector, script string, errs *[]types.CollectorError) (string, bool) {
	r := util.RunCommand(timeout, python, "-I", "-c", script)
	out := strings.TrimSpace(r.Stdout)
	if r.Err == nil && out != "" {
		return out, true
	}
	msg := "probe did not run using interpreter " + python
	if r.Err != nil {
		msg += ": " + r.Err.Error()
	}
	if s := lastLine(r.Stderr); s != "" {
		msg += " (" + s + ")"
	}
	*errs = append(*errs, types.CollectorError{Collector: collector, Error: msg})
	return "", false
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
        "device_name": ""
    }
    if torch.cuda.is_available() and torch.cuda.device_count() > 0:
        try:
            result["device_name"] = torch.cuda.get_device_name(0)
        except Exception:
            pass
    print(json.dumps(result))
except ImportError:
    print(json.dumps({"error": "not_installed"}))
except Exception as e:
    print(json.dumps({"error": str(e)}))
`

	stdout, ok := runProbe(timeout, python, "ai.pytorch", script, errs)
	if !ok {
		return
	}
	ptInfo := &types.PyTorchInfo{}
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

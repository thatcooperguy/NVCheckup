// Package ai provides collectors for AI/CUDA framework diagnostics.
package ai

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectAIInfo gathers AI framework and CUDA environment information.
func CollectAIInfo(timeout int) (types.AIInfo, []types.CollectorError) {
	var info types.AIInfo
	var errs []types.CollectorError

	collectCUDAToolkit(&info, &errs, timeout)
	collectCuDNN(&info, &errs, timeout)
	collectPythonEnvs(&info, &errs, timeout)
	collectConda(&info, &errs, timeout)
	collectPyTorch(&info, &errs, timeout)
	collectTensorFlow(&info, &errs, timeout)
	collectKeyPackages(&info, &errs, timeout)

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

func collectCuDNN(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	if runtime.GOOS == "linux" {
		// Check for cuDNN header
		for _, path := range []string{
			"/usr/include/cudnn_version.h",
			"/usr/local/cuda/include/cudnn_version.h",
			"/usr/include/cudnn.h",
			"/usr/local/cuda/include/cudnn.h",
		} {
			r := util.RunCommand(timeout, "sh", "-c", `grep -E "CUDNN_MAJOR|CUDNN_MINOR|CUDNN_PATCHLEVEL" `+path+` 2>/dev/null | head -3`)
			if r.Err == nil && r.Stdout != "" {
				major, minor, patch := "", "", ""
				for _, line := range strings.Split(r.Stdout, "\n") {
					if strings.Contains(line, "CUDNN_MAJOR") && !strings.Contains(line, "MINOR") && !strings.Contains(line, "PATCH") {
						parts := strings.Fields(line)
						if len(parts) >= 3 {
							major = parts[len(parts)-1]
						}
					} else if strings.Contains(line, "CUDNN_MINOR") {
						parts := strings.Fields(line)
						if len(parts) >= 3 {
							minor = parts[len(parts)-1]
						}
					} else if strings.Contains(line, "CUDNN_PATCHLEVEL") {
						parts := strings.Fields(line)
						if len(parts) >= 3 {
							patch = parts[len(parts)-1]
						}
					}
				}
				if major != "" {
					info.CuDNNVersion = major
					if minor != "" {
						info.CuDNNVersion += "." + minor
					}
					if patch != "" {
						info.CuDNNVersion += "." + patch
					}
				}
				break
			}
		}
	}

	if runtime.GOOS == "windows" {
		cudaPath := os.Getenv("CUDA_PATH")
		if cudaPath != "" {
			headerPath := filepath.Join(cudaPath, "include", "cudnn_version.h")
			r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command",
				`Select-String -Path "`+headerPath+`" -Pattern "CUDNN_MAJOR|CUDNN_MINOR|CUDNN_PATCHLEVEL" -ErrorAction SilentlyContinue | ForEach-Object { $_.Line }`)
			if r.Err == nil && r.Stdout != "" {
				major, minor, patch := "", "", ""
				for _, line := range strings.Split(r.Stdout, "\n") {
					parts := strings.Fields(line)
					if len(parts) < 3 {
						continue
					}
					lastVal := parts[len(parts)-1]
					if strings.Contains(line, "CUDNN_MAJOR") && !strings.Contains(line, "MINOR") {
						major = lastVal
					} else if strings.Contains(line, "CUDNN_MINOR") {
						minor = lastVal
					} else if strings.Contains(line, "CUDNN_PATCHLEVEL") {
						patch = lastVal
					}
				}
				if major != "" {
					info.CuDNNVersion = major
					if minor != "" {
						info.CuDNNVersion += "." + minor
					}
					if patch != "" {
						info.CuDNNVersion += "." + patch
					}
				}
			}
		}
	}
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

func collectConda(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	info.CondaPresent = util.CommandExists("conda")
}

func collectPyTorch(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	// Find a working python
	pythonCmd := findPython(timeout)
	if pythonCmd == "" {
		return
	}

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

	r := util.RunCommand(timeout, pythonCmd, "-c", script)
	if r.Err == nil && r.Stdout != "" {
		ptInfo := &types.PyTorchInfo{}
		stdout := strings.TrimSpace(r.Stdout)
		// Simple JSON parsing without encoding/json import dependency
		// Actually, let's just parse it properly
		if strings.Contains(stdout, `"error"`) {
			if strings.Contains(stdout, "not_installed") {
				// PyTorch not installed, skip
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
}

func collectTensorFlow(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	pythonCmd := findPython(timeout)
	if pythonCmd == "" {
		return
	}

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

	r := util.RunCommand(timeout+10, pythonCmd, "-c", script) // TF import can be slow
	if r.Err == nil && r.Stdout != "" {
		tfInfo := &types.TFInfo{}
		stdout := strings.TrimSpace(r.Stdout)
		if strings.Contains(stdout, `"error"`) {
			if strings.Contains(stdout, "not_installed") {
				return
			}
			tfInfo.Error = extractJSONValue(stdout, "error")
		} else {
			tfInfo.Version = extractJSONValue(stdout, "version")
			// Parse GPUs list
			gpuRe := regexp.MustCompile(`"gpus":\s*\[([^\]]*)\]`)
			if m := gpuRe.FindStringSubmatch(stdout); m != nil {
				gpuStr := m[1]
				itemRe := regexp.MustCompile(`"([^"]+)"`)
				for _, gm := range itemRe.FindAllStringSubmatch(gpuStr, -1) {
					tfInfo.GPUs = append(tfInfo.GPUs, gm[1])
				}
			}
		}
		info.TensorFlowInfo = tfInfo
	}
}

func collectKeyPackages(info *types.AIInfo, errs *[]types.CollectorError, timeout int) {
	pythonCmd := findPython(timeout)
	if pythonCmd == "" {
		return
	}

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

	r := util.RunCommand(timeout, pythonCmd, "-c", script)
	if r.Err == nil && r.Stdout != "" {
		// Parse key=value pairs from JSON
		stdout := strings.TrimSpace(r.Stdout)
		// Simple extraction
		pairRe := regexp.MustCompile(`"(\w+)":\s*"([^"]*)"`)
		for _, m := range pairRe.FindAllStringSubmatch(stdout, -1) {
			info.KeyPackages = append(info.KeyPackages, types.PackageInfo{
				Name:    m[1],
				Version: m[2],
			})
		}
	}
}

func findPython(timeout int) string {
	candidates := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		candidates = []string{"python", "python3", "py"}
	}
	for _, cmd := range candidates {
		if util.CommandExists(cmd) {
			return cmd
		}
	}
	return ""
}

func extractJSONValue(jsonStr, key string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `":\s*"([^"]*)"`)
	if m := re.FindStringSubmatch(jsonStr); m != nil {
		return m[1]
	}
	return ""
}

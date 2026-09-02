package ai

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// torchCapabilityStderr is how Python prints the spec 3.2 warning
// "Found GPU0 NVIDIA GB10 which is of cuda capability 12.1. Minimum and
// Maximum cuda capability supported by this version of PyTorch is (8.0) - (12.0)"
// (multi-line UserWarning with the warnings.warn( trailer).
const torchCapabilityStderr = `/usr/lib/python3/dist-packages/torch/cuda/__init__.py:262: UserWarning:
    Found GPU0 NVIDIA GB10 which is of cuda capability 12.1.
    Minimum and Maximum cuda capability supported by this version of PyTorch is
    (8.0) - (12.0)

  warnings.warn(
`

func TestParseTorchWarnings(t *testing.T) {
	got := parseTorchWarnings(torchCapabilityStderr)
	want := []string{"Found GPU0 NVIDIA GB10 which is of cuda capability 12.1. Minimum and Maximum cuda capability supported by this version of PyTorch is (8.0) - (12.0)"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseTorchWarnings = %q, want %q", got, want)
	}

	single := "C:\\Python312\\Lib\\site-packages\\torch\\__init__.py:10: DeprecationWarning: pkg_resources is deprecated\n  import pkg_resources\nunrelated noise line\n"
	if got := parseTorchWarnings(single); !reflect.DeepEqual(got, []string{"pkg_resources is deprecated import pkg_resources"}) {
		t.Errorf("single-line warning = %q", got)
	}
	if got := parseTorchWarnings("just noise\nmore noise\n"); got != nil {
		t.Errorf("noise must give nil, got %q", got)
	}
	// Duplicates collapse.
	if got := parseTorchWarnings(torchCapabilityStderr + torchCapabilityStderr); len(got) != 1 {
		t.Errorf("duplicate warnings must be de-duplicated, got %d", len(got))
	}
}

func TestExtractJSONStringList(t *testing.T) {
	out := `{"version": "2.9.0+cu130", "arch_list": ["sm_80", "sm_90", "sm_100", "sm_120", "compute_120"], "cuda_available": true}`
	got := extractJSONStringList(out, "arch_list")
	want := []string{"sm_80", "sm_90", "sm_100", "sm_120", "compute_120"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("arch list = %v, want %v", got, want)
	}
	if got := extractJSONStringList(`{"arch_list": []}`, "arch_list"); got != nil {
		t.Errorf("empty list must be nil, got %v", got)
	}
	if got := extractJSONStringList(`{"version": "x"}`, "arch_list"); got != nil {
		t.Errorf("missing key must be nil, got %v", got)
	}
}

func TestApplyEcosystemProbe(t *testing.T) {
	stdout := `{"torch": {"version": "2.9.0+cu130", "cuda": "13.0", "arch_list": ["sm_80", "sm_120"]}, "triton": {"version": "3.5.0", "ptxas": "/usr/lib/python3/dist-packages/triton/backends/nvidia/bin/ptxas"}, "flash_attn": "2.8.3", "ort": "1.20.0", "ort_gpu": "1.20.0", "ort_providers": ["AzureExecutionProvider", "CPUExecutionProvider"], "ort_version": "1.20.0", "libcudart": ["/usr/lib/python3/dist-packages/nvidia/cuda_runtime/lib/libcudart.so.12"]}`
	p, err := parseEcosystemProbe("stray site hook print\n" + stdout)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var info types.EcosystemInfo
	info.LibcudartVersions = []string{"libcudart.so.13"}
	applyEcosystemProbe(&info, p, torchCapabilityStderr)

	if !reflect.DeepEqual(info.TorchArchList, []string{"sm_80", "sm_120"}) {
		t.Errorf("TorchArchList = %v", info.TorchArchList)
	}
	if len(info.TorchWarnings) != 1 {
		t.Errorf("TorchWarnings = %v", info.TorchWarnings)
	}
	if info.FlashAttnVersion != "2.8.3" {
		t.Errorf("FlashAttnVersion = %q", info.FlashAttnVersion)
	}
	if info.ORTVersion != "1.20.0" || !info.ORTGPUShadowed {
		t.Errorf("ORT = %q shadowed=%v, want 1.20.0 true", info.ORTVersion, info.ORTGPUShadowed)
	}
	if want := []string{"libcudart.so.12", "libcudart.so.13"}; !reflect.DeepEqual(info.LibcudartVersions, want) {
		t.Errorf("LibcudartVersions = %v, want %v", info.LibcudartVersions, want)
	}

	// CUDA provider present: not shadowed even with both distributions.
	p.ORTProviders = []string{"CUDAExecutionProvider", "CPUExecutionProvider"}
	var info2 types.EcosystemInfo
	applyEcosystemProbe(&info2, p, "")
	if info2.ORTGPUShadowed {
		t.Error("CUDAExecutionProvider present must not be shadowed")
	}
	if info2.TorchWarnings != nil {
		t.Errorf("no stderr must give no warnings, got %v", info2.TorchWarnings)
	}
}

func TestParseDockerDaemonJSON(t *testing.T) {
	runtimes, cdi, err := parseDockerDaemonJSON(`{"runtimes": {"nvidia": {"path": "nvidia-container-runtime", "args": []}}, "features": {"cdi": true}}`)
	if err != nil || !reflect.DeepEqual(runtimes, []string{"nvidia"}) || !cdi {
		t.Errorf("daemon.json = %v %v %v", runtimes, cdi, err)
	}
	runtimes, cdi, err = parseDockerDaemonJSON(`{"log-driver": "json-file"}`)
	if err != nil || runtimes != nil || cdi {
		t.Errorf("minimal daemon.json = %v %v %v", runtimes, cdi, err)
	}
	if _, _, err := parseDockerDaemonJSON(`{not json`); err == nil {
		t.Error("invalid JSON must error")
	}
}

func TestDockerImageParsing(t *testing.T) {
	refs := parseDockerImageList("nvcr.io/nvidia/pytorch:25.09-py3\n<none>:<none>\nlmsysorg/sglang:latest\n\n")
	if want := []string{"nvcr.io/nvidia/pytorch:25.09-py3", "lmsysorg/sglang:latest"}; !reflect.DeepEqual(refs, want) {
		t.Errorf("image refs = %v, want %v", refs, want)
	}
	images := pairImageArchitectures(refs, "amd64\narm64\n")
	want := []types.ContainerImage{{Ref: "nvcr.io/nvidia/pytorch:25.09-py3", Arch: "amd64"}, {Ref: "lmsysorg/sglang:latest", Arch: "arm64"}}
	if !reflect.DeepEqual(images, want) {
		t.Errorf("images = %+v, want %+v", images, want)
	}
	// A short inspect answer leaves architectures empty rather than misaligned.
	images = pairImageArchitectures(refs, "amd64\n")
	if images[0].Arch != "" || images[1].Arch != "" {
		t.Errorf("misaligned inspect output must not assign architectures: %+v", images)
	}
}

func TestFilterPortsAndMergeLibcudart(t *testing.T) {
	if got := filterPorts([]int{22, 8000, 11434, 631}, inferencePorts); !reflect.DeepEqual(got, []int{8000, 11434}) {
		t.Errorf("filterPorts = %v", got)
	}
	got := mergeLibcudart(nil, []string{"/usr/local/cuda-13.0/lib64/libcudart.so.13.0.48", "/usr/lib/aarch64-linux-gnu/libcudart.so.12", "/x/libcuda.so.1"})
	if want := []string{"libcudart.so.12", "libcudart.so.13"}; !reflect.DeepEqual(got, want) {
		t.Errorf("mergeLibcudart = %v, want %v", got, want)
	}
}

func TestCollectEcosystemFromSimRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NVC_SIM_ROOT", root)
	t.Setenv("TRITON_PTXAS_PATH", "/usr/local/cuda/bin/ptxas")
	write := func(rel, content string) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("etc/docker/daemon.json", `{"runtimes": {"nvidia": {"path": "nvidia-container-runtime"}}, "features": {"cdi": true}}`)
	write("etc/cdi/nvidia.yaml", "cdiVersion: 0.6.0\n")
	write("snap/docker/current", "")
	// vLLM on 8000 (0x1F40) and Ollama on 11434 (0x2CAA) listening; 22 ignored.
	write("proc/net/tcp", "  sl  local_address rem_address   st\n"+
		"   0: 00000000:1F40 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 1 1 0000000000000000 100 0 0 10 0\n"+
		"   1: 0100007F:2CAA 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 2 1 0000000000000000 100 0 0 10 0\n"+
		"   2: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0 0 3 1 0000000000000000 100 0 0 10 0\n")
	write("usr/local/cuda-13.0/lib64/libcudart.so.13.0.48", "")

	// Keep the Python probe out of this test: an empty PATH makes every
	// interpreter and docker/snap lookup fail, so only the file-derived
	// fields are exercised.
	t.Setenv("PATH", "")
	info, errs := CollectEcosystem(5)
	if len(errs) != 0 {
		t.Errorf("unexpected collector errors: %+v", errs)
	}
	if !reflect.DeepEqual(info.DockerRuntimes, []string{"nvidia"}) || !info.DockerCDI || !info.CDISpecPresent || !info.SnapDocker {
		t.Errorf("docker facts = runtimes %v cdi %v spec %v snap %v", info.DockerRuntimes, info.DockerCDI, info.CDISpecPresent, info.SnapDocker)
	}
	if want := []int{8000, 11434}; !reflect.DeepEqual(info.ListeningPorts, want) {
		t.Errorf("ListeningPorts = %v, want %v", info.ListeningPorts, want)
	}
	if !reflect.DeepEqual(info.LibcudartVersions, []string{"libcudart.so.13"}) {
		t.Errorf("LibcudartVersions = %v", info.LibcudartVersions)
	}
	if info.TritonPtxasPath != "/usr/local/cuda/bin/ptxas" {
		t.Errorf("TritonPtxasPath = %q", info.TritonPtxasPath)
	}
}

package llmplan

import (
	"strings"
	"testing"

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

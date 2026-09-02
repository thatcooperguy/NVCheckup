package llmplan

import "testing"

// Every worked example of spec 7.5 is reproduced here: GiB figures to
// +/-0.1 GiB, the ceilings the spec states with a decimal (17 / 13.4,
// 6.9 / 3.3 / 5.4) to +/-0.1 tok/s and the integer-quoted ones (34 / 22,
// 61 / 31) to +/-0.5 tok/s.

func TestCeil05AndClamp(t *testing.T) {
	cases := map[float64]float64{0.3592: 0.40, 0.742: 0.75, 0.575: 0.60, 0.65: 0.65, 0.6499: 0.65, 0.01: 0.05, 0.9: 0.90}
	for in, want := range cases {
		if got := ceil05(in); got != want {
			t.Errorf("ceil05(%v) = %v, want %v", in, got, want)
		}
	}
	if clamp(0.1, UtilizationMin, UtilizationMax) != 0.30 || clamp(0.95, UtilizationMin, UtilizationMax) != 0.85 {
		t.Error("clamp to 0.30..0.85 failed")
	}
}

func TestBytesPerParam_Spec74(t *testing.T) {
	want := map[Quant]float64{QuantBF16: 2.0, QuantFP16: 2.0, QuantFP8: 1.0, QuantQ8_0: 1.06, QuantNVFP4: 0.56, QuantMXFP4: 0.53, QuantQ4KM: 0.60}
	for q, b := range want {
		if q.BytesPerParam() != b {
			t.Errorf("%s bytes/param = %v, want %v", q, q.BytesPerParam(), b)
		}
	}
	for _, kv := range []struct {
		k KVDtype
		b float64
	}{{KVF16, 2}, {KVFP8, 1}, {KVQ8_0, 1}, {KVQ4_0, 0.5}} {
		if kv.k.Bytes() != kv.b {
			t.Errorf("kv %s bytes = %v, want %v", kv.k, kv.k.Bytes(), kv.b)
		}
	}
	for _, a := range []string{"int8", "fp4", "q4", "F16", "bfloat16", " q8 "} {
		if _, ok := ParseQuant(a); !ok {
			t.Errorf("alias %q not accepted", a)
		}
	}
	if _, ok := ParseQuant("nf4"); ok {
		t.Error("nf4 has no factor in the spec and must be rejected")
	}
}

func TestReserves_Spec74(t *testing.T) {
	want := map[Runtime]float64{RuntimeVLLM: 12, RuntimeSGLang: 10, RuntimeTRTLLM: 10, RuntimeLlamaCpp: 3, RuntimeOllama: 3}
	for r, g := range want {
		if r.ReserveGiB() != g {
			t.Errorf("%s reserve = %v, want %v", r, r.ReserveGiB(), g)
		}
	}
}

// Row 1: Llama 3.1 8B BF16, agent 4 x 32K, f16 KV, vLLM.
func TestWorkedExample_Llama8B(t *testing.T) {
	m := mustModel(t, "llama-3.1-8b-instruct")
	in := Inputs{Model: m, Quant: QuantBF16, KV: KVF16, Context: 32768, Concurrency: 4, Runtime: RuntimeVLLM, Nodes: 1,
		PoolBytes: poolGB10Bytes, FloorBytes: floorLinux, BandwidthBytesPerSec: GB10BandwidthBytesPerSec}
	s := Compute(in)
	near(t, "W bf16", GiBf(s.WeightsBytes), 15.0, 0.1)
	near(t, "KV 4x32K f16", GiBf(s.KVBytes), 16.0, 0.1)
	near(t, "R vllm", GiBf(s.ReserveBytes), 12.0, 0.01)
	near(t, "W+KV+R", GiBf(s.NowBytes), 43.0, 0.1)
	if !s.FitsTotal {
		t.Error("8B BF16 agent must fit the 128 GB unit")
	}
	if s.Utilization != 0.40 {
		t.Errorf("u = %.2f, want 0.40", s.Utilization)
	}
	near(t, "ceiling weights-only", s.CeilingWeightsOnlyTPS, 17.0, 0.1)
	near(t, "ceiling at 32K", s.CeilingAtContextTPS, 13.4, 0.1)
	near(t, "band low", s.BandLowTPS, 13.4*0.5, 0.1)
	near(t, "band high", s.BandHighTPS, 13.4*0.8, 0.1)

	in.Quant = QuantFP8
	s = Compute(in)
	near(t, "W fp8", GiBf(s.WeightsBytes), 7.5, 0.1)
	near(t, "fp8 ceiling weights-only", s.CeilingWeightsOnlyTPS, 34, 0.5)
	near(t, "fp8 ceiling at 32K", s.CeilingAtContextTPS, 22, 0.5)

	in.Quant = QuantNVFP4
	s = Compute(in)
	near(t, "W nvfp4", GiBf(s.WeightsBytes), 4.2, 0.1)
	near(t, "nvfp4 ceiling weights-only", s.CeilingWeightsOnlyTPS, 61, 0.5)
	near(t, "nvfp4 ceiling at 32K", s.CeilingAtContextTPS, 31, 0.5)

	// 64 GB column: llama.cpp, R = 3, F = 10, pool 57.7 GiB.
	in64 := Inputs{Model: m, Quant: QuantBF16, KV: KVF16, Context: 32768, Concurrency: 4, Runtime: RuntimeLlamaCpp, Nodes: 1,
		PoolBytes: pool64Bytes, FloorBytes: floorWindows}
	s = Compute(in64)
	near(t, "64GB 8B BF16 W+KV+R", GiBf(s.NowBytes), 34.0, 0.1)
	if !s.FitsTotal {
		t.Error("15 + 16 + 3 = 34 GiB must fit the 47.7 GiB budget")
	}
	in64.Quant, in64.Context, in64.Concurrency = QuantQ4KM, 131072, 1
	s = Compute(in64)
	near(t, "64GB 8B Q4_K_M W", GiBf(s.WeightsBytes), 4.5, 0.1)
	if !s.FitsTotal {
		t.Error("Q4_K_M 4.5 GiB must fit at 128K on the 64 GB unit")
	}
}

// Row 2: Llama 3.3 70B NVFP4, 128K, vLLM.
func TestWorkedExample_Llama70B(t *testing.T) {
	m := mustModel(t, "llama-3.3-70b-instruct")
	in := Inputs{Model: m, Quant: QuantNVFP4, KV: KVF16, Context: 131072, Concurrency: 1, Runtime: RuntimeVLLM, Nodes: 1,
		PoolBytes: poolGB10Bytes, FloorBytes: floorLinux, BandwidthBytesPerSec: GB10BandwidthBytesPerSec}
	s := Compute(in)
	near(t, "W nvfp4", GiBf(s.WeightsBytes), 36.8, 0.1)
	near(t, "KV 128K f16", GiBf(s.KVBytes), 40.0, 0.1)
	near(t, "W+KV+R", GiBf(s.NowBytes), 88.8, 0.1)
	if !s.FitsTotal || s.Utilization != 0.75 {
		t.Errorf("70B NVFP4 128K: fits=%v u=%.2f, want fits u=0.75", s.FitsTotal, s.Utilization)
	}
	near(t, "ceiling weights-only", s.CeilingWeightsOnlyTPS, 6.9, 0.1)
	near(t, "ceiling at 128K", s.CeilingAtContextTPS, 3.3, 0.1)

	in.KV = KVFP8
	s = Compute(in)
	near(t, "KV 128K fp8", GiBf(s.KVBytes), 20.0, 0.1)
	near(t, "W+KV+R fp8 KV", GiBf(s.NowBytes), 68.8, 0.1)
	if s.Utilization != 0.60 {
		t.Errorf("u with fp8 KV = %.2f, want 0.60", s.Utilization)
	}

	in.KV, in.Context, in.Concurrency = KVF16, 32768, 4
	s = Compute(in)
	near(t, "ceiling at 32K", s.CeilingAtContextTPS, 5.4, 0.1)
	if !s.FitsTotal {
		t.Error("70B NVFP4 4 x 32K must fit (same KV as 1 x 128K)")
	}

	in.Quant, in.Context, in.Concurrency = QuantBF16, 131072, 1
	s = Compute(in)
	near(t, "W bf16", GiBf(s.WeightsBytes), 131.5, 0.1)
	if s.FitsTotal {
		t.Error("70B BF16 must not fit")
	}
	in.Quant = QuantFP8
	s = Compute(in)
	near(t, "W fp8", GiBf(s.WeightsBytes), 65.7, 0.1)
	if s.FitsTotal {
		t.Error("70B FP8 + 128K (65.7 + 40 + 12 + F) must not fit")
	}

	// 64 GB column: Q4_K_M 39.4 + 3 = 42.4 GiB leaves 5.3 GiB KV: f16 ~17K tokens or q8_0 32K (5.0 GiB).
	in64 := Inputs{Model: m, Quant: QuantQ4KM, KV: KVF16, Context: 16384, Concurrency: 1, Runtime: RuntimeLlamaCpp, Nodes: 1,
		PoolBytes: pool64Bytes, FloorBytes: floorWindows}
	s = Compute(in64)
	near(t, "64GB 70B Q4_K_M W", GiBf(s.WeightsBytes), 39.4, 0.1)
	if s.MaxContextTokens < 16500 || s.MaxContextTokens > 17500 {
		t.Errorf("max f16 context on the 64 GB unit = %d, want ~17K", s.MaxContextTokens)
	}
	in64.KV, in64.Context = KVQ8_0, 32768
	s = Compute(in64)
	near(t, "64GB 70B q8_0 32K KV", GiBf(s.KVBytes), 5.0, 0.1)
	if !s.FitsTotal {
		t.Errorf("70B Q4_K_M with q8_0 32K KV must fit the 64 GB unit (total %.1f GiB)", GiBf(s.TotalBytes))
	}
	in64.Runtime = RuntimeVLLM
	s = Compute(in64)
	if s.FitsTotal {
		t.Error("vLLM (39.4 + 12) must not fit the 47.7 GiB budget")
	}
}

// Row 3: gpt-oss-120b MXFP4, agent 4 x 32K, vLLM.
func TestWorkedExample_GPTOSS(t *testing.T) {
	m := mustModel(t, "gpt-oss-120b")
	in := Inputs{Model: m, Quant: QuantMXFP4, KV: KVF16, Context: 32768, Concurrency: 4, Runtime: RuntimeVLLM, Nodes: 1,
		PoolBytes: poolGB10Bytes, FloorBytes: floorLinux, BandwidthBytesPerSec: GB10BandwidthBytesPerSec}
	s := Compute(in)
	near(t, "W measured", GiBf(s.WeightsBytes), 56.8, 0.1)
	if !s.WeightsMeasured {
		t.Error("gpt-oss-120b must use the measured checkpoint size")
	}
	near(t, "KV upper bound", GiBf(s.KVBytes), 9.0, 0.1)
	near(t, "W+KV+R", GiBf(s.NowBytes), 77.8, 0.1)
	if !s.FitsTotal || s.Utilization != 0.65 {
		t.Errorf("gpt-oss-120b: fits=%v u=%.2f, want fits u=0.65", s.FitsTotal, s.Utilization)
	}
	if s.CeilingWeightsOnlyTPS != 0 || s.CeilingAtContextTPS != 0 || s.CeilingNote == "" {
		t.Error("no formula ceiling may be printed for a mixed bf16/MXFP4 MoE checkpoint (spec 7.5)")
	}

	// 64 GB column: does not fit (56.8 > 47.7); gpt-oss-20b 12.1 + 3 + 6.0 (128K) = 21.1 GiB.
	in64 := in
	in64.Runtime, in64.PoolBytes, in64.FloorBytes = RuntimeLlamaCpp, pool64Bytes, floorWindows
	if Compute(in64).FitsTotal {
		t.Error("gpt-oss-120b must not fit the 64 GB unit")
	}
	in64.Model, in64.Context, in64.Concurrency = mustModel(t, "gpt-oss-20b"), 131072, 1
	s = Compute(in64)
	near(t, "20b W", GiBf(s.WeightsBytes), 12.1, 0.1)
	near(t, "20b KV 128K", GiBf(s.KVBytes), 6.0, 0.1)
	near(t, "20b W+KV+R", GiBf(s.NowBytes), 21.1, 0.1)
	if !s.FitsTotal {
		t.Error("gpt-oss-20b must fit the 64 GB unit at 128K")
	}
}

// Qwen3-235B-A22B NVFP4 = 235e9 x 0.56 = 122.6 GiB > 119.7 GiB.
func TestWorkedExample_Qwen235B(t *testing.T) {
	m := mustModel(t, "qwen3-235b-a22b")
	in := Inputs{Model: m, Quant: QuantNVFP4, KV: KVF16, Context: 32768, Concurrency: 1, Runtime: RuntimeVLLM, Nodes: 1,
		PoolBytes: poolGB10Bytes, FloorBytes: floorLinux}
	s := Compute(in)
	near(t, "W nvfp4", GiBf(s.WeightsBytes), 122.6, 0.1)
	if s.FitsTotal || s.MaxContextTokens != 0 {
		t.Error("Qwen3-235B NVFP4 must not fit one Spark")
	}
	in.Nodes = 2
	s = Compute(in)
	if !s.FitsTotal {
		t.Errorf("Qwen3-235B NVFP4 should fit per node across two Sparks (per node %.1f GiB)", GiBf(s.TotalBytes))
	}
	if s.CeilingAtContextTPS != 0 || s.CeilingNote == "" {
		t.Error("two-node ceilings are not modelled")
	}
}

func TestKVPerToken_Spec73(t *testing.T) {
	want := map[string]float64{
		"llama-3.1-8b-instruct":            131072,
		"llama-3.3-70b-instruct":           327680,
		"qwen3-32b":                        262144,
		"qwen3-235b-a22b":                  192512,
		"gpt-oss-120b":                     73728,
		"gpt-oss-20b":                      49152,
		"nemotron-3-super-120b-a12b-nvfp4": 8192,
	}
	for id, k := range want {
		if got := mustModel(t, id).KVBytesPerToken(2); got != k {
			t.Errorf("%s KV B/token f16 = %.0f, want %.0f", id, got, k)
		}
	}
}

func TestMambaState_Nemotron(t *testing.T) {
	m := mustModel(t, "nemotron-3-super-120b-a12b-nvfp4")
	// 7 GB measured per slot at 262K minus the 8 KiB/token attention KV it contains.
	want := 7e9 - 8192*262144
	if got := MambaStateBytesPerSlot(m); got != want {
		t.Errorf("state per slot = %.0f, want %.0f", got, want)
	}
	if MambaStateBytesPerSlot(mustModel(t, "llama-3.1-8b-instruct")) != 0 {
		t.Error("pure transformers have no state term")
	}
	s := Compute(Inputs{Model: m, Quant: QuantNVFP4, KV: KVF16, Context: 262144, Concurrency: 1, Runtime: RuntimeOllama, Nodes: 1, PoolBytes: poolGB10Bytes, FloorBytes: floorLinux})
	near(t, "KV + state per slot at 262K", GiBf(s.KVBytes+s.StateBytes), GiBf(7e9), 0.05)
}

func TestFitsNow(t *testing.T) {
	m := mustModel(t, "llama-3.1-8b-instruct")
	in := Inputs{Model: m, Quant: QuantBF16, KV: KVF16, Context: 32768, Concurrency: 4, Runtime: RuntimeVLLM, Nodes: 1,
		PoolBytes: poolGB10Bytes, AvailableBytes: 30 * GiB, FloorBytes: floorLinux}
	s := Compute(in)
	if !s.FitsTotal || !s.FitsNowKnown || s.FitsNow {
		t.Errorf("43 GiB needed with 30 GiB available: fitsTotal=%v known=%v now=%v", s.FitsTotal, s.FitsNowKnown, s.FitsNow)
	}
	in.AvailableBytes = 0
	if s = Compute(in); s.FitsNowKnown {
		t.Error("unknown MemAvailable must not claim a now-fit")
	}
}

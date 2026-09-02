package llmplan

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// GiB is the unit every memory figure in the plan is printed in.
const GiB = 1 << 30

// Quant is a weight format the wizard knows a bytes-per-parameter factor for.
type Quant string

// Quant names as accepted by --quant (spec 7.1).
const (
	QuantBF16  Quant = "bf16"
	QuantFP16  Quant = "fp16"
	QuantFP8   Quant = "fp8"
	QuantQ8_0  Quant = "q8_0"
	QuantNVFP4 Quant = "nvfp4"
	QuantMXFP4 Quant = "mxfp4"
	QuantQ4KM  Quant = "q4_k_m"
)

// bytesPerParam is spec 7.4 "b": bf16/fp16 2.00, fp8 1.00, q8_0 1.06,
// nvfp4 0.5625 -> 0.56 (4-bit values + one FP8 scale per 16-weight block),
// mxfp4 0.53 for expert weights, q4_k_m 0.60 (0.62 measured). S2 S82 S83.
var bytesPerParam = map[Quant]float64{
	QuantBF16:  2.00,
	QuantFP16:  2.00,
	QuantFP8:   1.00,
	QuantQ8_0:  1.06,
	QuantNVFP4: 0.56,
	QuantMXFP4: 0.53,
	QuantQ4KM:  0.60,
}

// quantAliases maps common spellings onto the spec names. int8 has the same
// storage as fp8 (1 byte/param); q8 -> q8_0; fp4 -> nvfp4; q4/q4km -> q4_k_m.
// nf4/int4 (bitsandbytes) have no factor in the spec and are rejected.
var quantAliases = map[string]Quant{
	"bf16": QuantBF16, "bfloat16": QuantBF16,
	"fp16": QuantFP16, "f16": QuantFP16, "float16": QuantFP16,
	"fp8": QuantFP8, "int8": QuantFP8, "fp8_e4m3": QuantFP8,
	"q8_0": QuantQ8_0, "q8": QuantQ8_0, "q8-0": QuantQ8_0,
	"nvfp4": QuantNVFP4, "fp4": QuantNVFP4, "modelopt_fp4": QuantNVFP4,
	"mxfp4":  QuantMXFP4,
	"q4_k_m": QuantQ4KM, "q4km": QuantQ4KM, "q4-k-m": QuantQ4KM, "q4": QuantQ4KM,
}

// QuantNames lists the accepted --quant values for help text.
func QuantNames() string { return "bf16|fp16|fp8|q8_0|nvfp4|mxfp4|q4_k_m" }

// ParseQuant resolves a --quant value.
func ParseQuant(s string) (Quant, bool) {
	q, ok := quantAliases[strings.ToLower(strings.TrimSpace(s))]
	return q, ok
}

// BytesPerParam returns spec 7.4 "b".
func (q Quant) BytesPerParam() float64 { return bytesPerParam[q] }

// IsGGUF reports whether the quant is a llama.cpp/Ollama GGUF type.
func (q Quant) IsGGUF() bool { return q == QuantQ8_0 || q == QuantQ4KM }

// Rank orders quants from least to most lossy (used for "smallest quant that
// fits" advice). Formats of the same bit width share a rank: the spec gives
// no quality ordering between the 8-bit formats or between the 4-bit block
// formats, so the wizard never recommends switching within a rank.
func (q Quant) Rank() int {
	switch q {
	case QuantBF16, QuantFP16:
		return 0
	case QuantQ8_0, QuantFP8:
		return 1
	case QuantQ4KM, QuantNVFP4, QuantMXFP4:
		return 2
	}
	return 9
}

// KVDtype is the KV-cache element type (spec 7.1 --kv-dtype auto|f16|fp8|q8_0,
// plus q4_0 which the spec prices in 7.4).
type KVDtype string

// KV dtype names.
const (
	KVAuto KVDtype = "auto"
	KVF16  KVDtype = "f16"
	KVFP8  KVDtype = "fp8"
	KVQ8_0 KVDtype = "q8_0"
	KVQ4_0 KVDtype = "q4_0"
)

// kvBytes is spec 7.4 bytes_kv: f16 2, fp8/q8_0 1, q4_0 0.5.
var kvBytes = map[KVDtype]float64{KVF16: 2, KVFP8: 1, KVQ8_0: 1, KVQ4_0: 0.5}

// ParseKVDtype resolves a --kv-dtype value.
func ParseKVDtype(s string) (KVDtype, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return KVAuto, true
	case "f16", "fp16", "bf16", "half":
		return KVF16, true
	case "fp8", "fp8_e4m3", "fp8_e5m2":
		return KVFP8, true
	case "q8_0", "q8":
		return KVQ8_0, true
	case "q4_0", "q4":
		return KVQ4_0, true
	}
	return "", false
}

// Bytes returns bytes per KV element.
func (k KVDtype) Bytes() float64 { return kvBytes[k] }

// OS floor F (spec 7.4): 8 GiB headless DGX OS, 10 GiB with GNOME or on
// Windows. Marked "inference, no primary source" in the spec.
const (
	OSFloorHeadlessGiB = 8.0
	OSFloorDesktopGiB  = 10.0
)

// GB10BandwidthBytesPerSec is the 273 GB/s LPDDR5X figure of spec 2.1 used
// for the decode ceilings (spec 7.4). N1X: press ~300 GB/s is unconfirmed,
// the spec says to use 273 (spec 2.2, 7.2).
const GB10BandwidthBytesPerSec = 273e9

// vLLM --gpu-memory-utilization clamp (spec 7.4): 0.30..0.85.
const (
	UtilizationMin = 0.30
	UtilizationMax = 0.85
)

// Realism band applied to the at-context ceiling (spec 7.4: 50-80%).
const (
	BandLowFraction  = 0.50
	BandHighFraction = 0.80
)

// PrefillReferenceTPS is the measured prefill range the spec tells the wizard
// to quote (spec 7.4: "Prefill: quote measured 2,000-8,000 tok/s").
const PrefillReferenceTPS = "2,000-8,000 tok/s (measured by others on GB10, S88-S90; not measured here)"

// mambaSlotContextTokens is the context at which spec 7.3 quotes the Nemotron
// per-slot measurement (~7 GB per Ollama slot at 262K, S81, single source).
const mambaSlotContextTokens = 262144

// Inputs is everything Compute needs. Bytes fields are absolute; a zero
// AvailableBytes or BandwidthBytesPerSec means "unknown".
type Inputs struct {
	Model       ModelShape
	Quant       Quant
	KV          KVDtype // resolved, never KVAuto
	Context     int     // tokens per stream
	Concurrency int     // streams (agent: 1 + subagents, spec 7.2)
	Runtime     Runtime
	Nodes       int // 1 or 2 (spec 7.1 target: single node / cluster of two)

	PoolBytes            float64 // measured MemTotal (or VRAM total on discrete GPUs); never "128 GB"
	AvailableBytes       float64 // MemAvailable now; 0 = unknown
	FloorBytes           float64 // F
	BandwidthBytesPerSec float64 // 0 = unknown, no ceiling printed
}

// Sizing is the result of the spec 7.4 arithmetic. All byte figures are the
// aggregate for the whole deployment; when Nodes == 2 the Total/Now figures
// are per node (weights, KV and state split evenly; R and F per node).
type Sizing struct {
	WeightsBytes       float64
	WeightsMeasured    bool // W came from a measured checkpoint size, not P x b
	ActiveWeightsBytes float64
	KVPerTokenBytes    float64
	KVBytes            float64
	StateBytes         float64 // hybrid Mamba per-slot state x concurrency (0 for pure transformers)
	ReserveBytes       float64 // R
	FloorBytes         float64 // F
	Nodes              int

	TotalBytes float64 // (W + KV + state)/nodes + R + F, compared against PoolBytes
	NowBytes   float64 // (W + KV + state)/nodes + R, compared against AvailableBytes
	PoolBytes  float64
	AvailBytes float64

	FitsTotal    bool
	FitsNowKnown bool
	FitsNow      bool
	MarginBytes  float64 // PoolBytes - TotalBytes (negative when it does not fit)

	Utilization float64 // vLLM u (also TRT-LLM free_gpu_memory_fraction, SGLang --mem-fraction-static)

	// Decode ceilings for one stream (spec 7.4). Zero means "not printed";
	// CeilingNote says why.
	CeilingWeightsOnlyTPS float64
	CeilingAtContextTPS   float64
	BandLowTPS            float64
	BandHighTPS           float64
	CeilingNote           string

	// MaxContextTokens is the largest per-stream context (at this quant,
	// concurrency and KV dtype) whose design total still fits the pool.
	MaxContextTokens int
}

// ceil05 rounds up to the next 0.05 (spec 7.4 "ceil05").
func ceil05(x float64) float64 {
	return math.Ceil(x*20-1e-9) / 20
}

// clamp bounds x to [lo, hi].
func clamp(x, lo, hi float64) float64 {
	return math.Max(lo, math.Min(hi, x))
}

// WeightsBytes is spec 7.4 W = P_total x b, unless the spec quotes a measured
// checkpoint size for this quant (preferred, spec 7.4 mxfp4/nvfp4 notes).
func WeightsBytes(m ModelShape, q Quant) (bytes float64, measured bool) {
	if gib, ok := m.MeasuredCheckpointGiB[string(q)]; ok && gib > 0 {
		return gib * GiB, true
	}
	return m.ParamsB * 1e9 * q.BytesPerParam(), false
}

// MambaStateBytesPerSlot derives the hybrid-Mamba per-slot state term from
// the spec 7.3 Nemotron measurement (~7 GB per Ollama slot at 262K, S81) by
// removing the attention KV that measurement already contains. Single source,
// unconfirmed; zero for models without a measurement.
func MambaStateBytesPerSlot(m ModelShape) float64 {
	if m.MeasuredSlotGBAt262K <= 0 {
		return 0
	}
	state := m.MeasuredSlotGBAt262K*1e9 - m.KVBytesPerToken(2)*mambaSlotContextTokens
	if state < 0 {
		return 0
	}
	return state
}

// Compute applies the spec 7.4 formulas.
func Compute(in Inputs) Sizing {
	nodes := in.Nodes
	if nodes < 1 {
		nodes = 1
	}
	s := Sizing{Nodes: nodes, PoolBytes: in.PoolBytes, AvailBytes: in.AvailableBytes, FloorBytes: in.FloorBytes}

	s.WeightsBytes, s.WeightsMeasured = WeightsBytes(in.Model, in.Quant)
	s.ActiveWeightsBytes = in.Model.ActiveParamsB * 1e9 * in.Quant.BytesPerParam()

	// KV per token k = 2 x L x H_kv x d_head x bytes_kv; KV = k x context x concurrency.
	s.KVPerTokenBytes = in.Model.KVBytesPerToken(in.KV.Bytes())
	s.KVBytes = s.KVPerTokenBytes * float64(in.Context) * float64(in.Concurrency)
	s.StateBytes = MambaStateBytesPerSlot(in.Model) * float64(in.Concurrency)

	s.ReserveBytes = in.Runtime.ReserveGiB() * GiB

	shard := (s.WeightsBytes + s.KVBytes + s.StateBytes) / float64(nodes)
	s.NowBytes = shard + s.ReserveBytes
	s.TotalBytes = s.NowBytes + s.FloorBytes

	// Fit: W + KV + R + F <= MemTotal (design) and W + KV + R <= MemAvailable_now.
	s.FitsTotal = in.PoolBytes > 0 && s.TotalBytes <= in.PoolBytes
	s.MarginBytes = in.PoolBytes - s.TotalBytes
	if in.AvailableBytes > 0 {
		s.FitsNowKnown = true
		s.FitsNow = s.NowBytes <= in.AvailableBytes
	}

	// u = ceil05((W + KV + R) / MemTotal) clamped 0.30..0.85.
	if in.PoolBytes > 0 {
		s.Utilization = clamp(ceil05(s.NowBytes/in.PoolBytes), UtilizationMin, UtilizationMax)
	}

	// Largest per-stream context whose design total fits.
	if in.PoolBytes > 0 && in.Concurrency > 0 && s.KVPerTokenBytes > 0 {
		free := (in.PoolBytes-s.ReserveBytes-s.FloorBytes)*float64(nodes) - s.WeightsBytes - s.StateBytes
		if free > 0 {
			s.MaxContextTokens = int(free / (s.KVPerTokenBytes * float64(in.Concurrency)))
		}
	}

	s.computeCeilings(in)
	return s
}

// computeCeilings fills the two decode ceilings of spec 7.4:
// weights-only tok/s <= BW / bytes_active_weights and
// at-context tok/s <= BW / (bytes_active_weights + k x context), one stream,
// plus the 50-80% realism band on the at-context value.
func (s *Sizing) computeCeilings(in Inputs) {
	switch {
	case in.BandwidthBytesPerSec <= 0:
		s.CeilingNote = "memory bandwidth of this platform is not known to the wizard; no decode ceiling is printed"
		return
	case in.Model.NoFormulaCeiling || (in.Quant == QuantMXFP4 && in.Model.IsMoE()):
		// spec 7.5: active-weight bytes of a mixed bf16/MXFP4 MoE checkpoint are not known to +/-10%.
		s.CeilingNote = "no formula ceiling: the active-weight bytes of a mixed bf16/MXFP4 MoE checkpoint are not known to +/-10% (spec 7.5)"
		if in.Model.MeasuredDecodeTPS != "" {
			s.CeilingNote += "; " + in.Model.MeasuredDecodeTPS
		}
		return
	case s.Nodes > 1:
		s.CeilingNote = "two-node decode is bounded by the ConnectX-7 fabric as well as memory bandwidth; the single-node ceiling formula is not applied"
		return
	case s.ActiveWeightsBytes <= 0:
		s.CeilingNote = "active parameter count unknown"
		return
	}
	s.CeilingWeightsOnlyTPS = in.BandwidthBytesPerSec / s.ActiveWeightsBytes
	s.CeilingAtContextTPS = in.BandwidthBytesPerSec / (s.ActiveWeightsBytes + s.KVPerTokenBytes*float64(in.Context))
	s.BandLowTPS = s.CeilingAtContextTPS * BandLowFraction
	s.BandHighTPS = s.CeilingAtContextTPS * BandHighFraction
}

// GiBf converts bytes to GiB.
func GiBf(b float64) float64 { return b / GiB }

// fmtGiB prints a byte count as "12.3 GiB", rounded exactly like the JSON
// figures (round1) so text and plan.json never disagree in the last digit.
func fmtGiB(b float64) string { return fmt.Sprintf("%.1f GiB", round1(GiBf(b))) }

// smallTPS is the value below which a tok/s ceiling would round to 0.0 at one
// decimal; such ceilings (very long --context) keep three significant digits
// instead so a valid figure is never printed or stored as 0.
const smallTPS = 0.05

// smallTPSString prints 0 < x < smallTPS in plain decimal notation with three
// significant digits (0.00273, 0.0000208), never in exponent form.
func smallTPSString(x float64) string {
	digits := 2 - int(math.Floor(math.Log10(x)))
	return strconv.FormatFloat(x, 'f', digits, 64)
}

// roundTPS rounds a tok/s ceiling for plan.json: round1 like every other
// figure, except that positive values below smallTPS keep three significant
// digits (parsed back from the decimal string, so no float noise).
func roundTPS(x float64) float64 {
	if x <= 0 || x >= smallTPS {
		return round1(x)
	}
	v, _ := strconv.ParseFloat(smallTPSString(x), 64)
	return v
}

// fmtTPS prints a tok/s ceiling with exactly the precision roundTPS keeps, so
// text, markdown and plan.json agree.
func fmtTPS(x float64) string {
	if x <= 0 || x >= smallTPS {
		return fmt.Sprintf("%.1f", x)
	}
	return smallTPSString(x)
}

// fmtTokens prints a token count as 32K / 131072 style.
func fmtTokens(n int) string {
	if n >= 1024 && n%1024 == 0 {
		return fmt.Sprintf("%dK", n/1024)
	}
	return fmt.Sprintf("%d", n)
}

package common

import (
	"strings"
	"testing"
)

func TestFormatPCIeGen(t *testing.T) {
	tests := []struct {
		gen      int
		expected string
	}{
		{1, "Gen1"},
		{3, "Gen3"},
		{4, "Gen4"},
		{5, "Gen5"},
		{6, "Gen6"},
		{7, "Gen7"},
	}

	for _, tt := range tests {
		result := formatPCIeGen(tt.gen)
		if result != tt.expected {
			t.Errorf("formatPCIeGen(%d) = %s, want %s", tt.gen, result, tt.expected)
		}
	}
}

func TestFormatPCIeWidth(t *testing.T) {
	tests := []struct {
		width    int
		expected string
	}{
		{1, "x1"},
		{4, "x4"},
		{8, "x8"},
		{16, "x16"},
		{32, "x32"},
	}

	for _, tt := range tests {
		result := formatPCIeWidth(tt.width)
		if result != tt.expected {
			t.Errorf("formatPCIeWidth(%d) = %s, want %s", tt.width, result, tt.expected)
		}
	}
}

// Rows follow PCIeQueryFields: index, gen current, gen max, width current,
// width max, pstate, utilization.
func TestParsePCIeCSV(t *testing.T) {
	tests := []struct {
		name            string
		line            string
		wantDownshifted bool
		wantIdle        bool
		wantPState      string
		wantUtil        int
		wantCurSpeed    string
		wantCurWidth    string
	}{
		// Idle at P8 with a Gen1 link: power saving, not a fault.
		{"idle gen1", "0, 1, 4, 16, 16, P8, 28", false, true, "P8", 28, "Gen1", "x16"},
		// Busy at P0 but still Gen1: a real downshift.
		{"busy gen1", "0, 1, 4, 16, 16, P0, 95", true, false, "P0", 95, "Gen1", "x16"},
		// Width below max is a fault regardless of load.
		{"width x8", "0, 4, 4, 8, 16, P0, 95", true, false, "P0", 95, "Gen4", "x8"},
		// Width below max even when idle.
		{"width x8 idle", "0, 1, 4, 8, 16, P8, 3", true, true, "P8", 3, "Gen1", "x8"},
		// Low utilization at P0 counts as idle, so a gen drop is tolerated.
		{"p0 low util", "0, 1, 4, 16, 16, P0, 5", false, true, "P0", 5, "Gen1", "x16"},
		{"full speed", "0, 4, 4, 16, 16, P0, 99", false, false, "P0", 99, "Gen4", "x16"},
		// Unknown load state (pstate and utilization both [N/A]): a Gen1 link is
		// neither a confirmed downshift nor confirmed idle. The analyzer needs
		// positive load evidence before warning, so the collector must agree.
		{"gen1 unknown load", "0, 1, 4, 16, 16, [N/A], [N/A]", false, false, "", 0, "Gen1", "x16"},
		// P0 alone is positive load evidence even without a utilization figure.
		{"gen1 p0 no util", "0, 1, 4, 16, 16, P0, [N/A]", true, false, "P0", 0, "Gen1", "x16"},
		// Width below max is a fault even with unknown load.
		{"width x8 unknown load", "0, 4, 4, 8, 16, [N/A], [N/A]", true, false, "", 0, "Gen4", "x8"},
		// P13-P15 are valid deep-idle states, not unknown values.
		{"p15 deep idle", "0, 1, 5, 16, 16, P15, 0", false, true, "P15", 0, "Gen1", "x16"},
	}
	for _, tt := range tests {
		info, errs := parsePCIeCSV(tt.line)
		if len(errs) != 0 {
			t.Errorf("%s: unexpected errors %+v", tt.name, errs)
		}
		if info.Downshifted != tt.wantDownshifted {
			t.Errorf("%s: Downshifted = %v, want %v", tt.name, info.Downshifted, tt.wantDownshifted)
		}
		if info.IdleLikely != tt.wantIdle {
			t.Errorf("%s: IdleLikely = %v, want %v", tt.name, info.IdleLikely, tt.wantIdle)
		}
		if info.PowerState != tt.wantPState || info.UtilizationPct != tt.wantUtil {
			t.Errorf("%s: PowerState/Util = %s/%d, want %s/%d", tt.name, info.PowerState, info.UtilizationPct, tt.wantPState, tt.wantUtil)
		}
		if info.CurrentSpeed != tt.wantCurSpeed || info.CurrentWidth != tt.wantCurWidth {
			t.Errorf("%s: speed/width = %s/%s, want %s/%s", tt.name, info.CurrentSpeed, info.CurrentWidth, tt.wantCurSpeed, tt.wantCurWidth)
		}
	}
}

// TestParsePCIeCSV_GPUClasses covers link states from consumer, laptop,
// legacy, data-center and workstation parts. The two properties that matter
// across all of them: the maximum is the card's own maximum (a laptop wired
// x8 of x8 is at full width), and generation is compared without a ceiling
// so Gen5 and Gen6 links parse like any other.
func TestParsePCIeCSV_GPUClasses(t *testing.T) {
	tests := []struct {
		name            string
		line            string
		wantCur         string // "GenN xM"
		wantMax         string
		wantDownshifted bool
		wantIdle        bool
	}{
		{"GeForce RTX 5090 at Gen5 x16 under load", "0, 5, 5, 16, 16, P0, 97", "Gen5 x16", "Gen5 x16", false, false},
		{"GeForce RTX 4060 Laptop GPU idle, wired x8 of x8", "0, 1, 4, 8, 8, P8, 0", "Gen1 x8", "Gen4 x8", false, true},
		{"GeForce RTX 4060 Laptop GPU busy at its own maximum", "0, 4, 4, 8, 8, P0, 91", "Gen4 x8", "Gen4 x8", false, false},
		{"GeForce GTX 1060 6GB Gen3 board at Gen3", "0, 3, 3, 16, 16, P2, 73", "Gen3 x16", "Gen3 x16", false, false},
		{"NVIDIA A100-SXM4-80GB Gen4 x16 at 100%", "0, 4, 4, 16, 16, P0, 100", "Gen4 x16", "Gen4 x16", false, false},
		{"NVIDIA H100 80GB HBM3 with MIG (utilization [N/A])", "0, 5, 5, 16, 16, P0, [N/A]", "Gen5 x16", "Gen5 x16", false, false},
		{"Tesla T4 passive idle at Gen1", "0, 1, 3, 16, 16, P8, 0", "Gen1 x16", "Gen3 x16", false, true},
		{"Quadro RTX 8000 rendering at Gen3", "0, 3, 3, 16, 16, P0, 88", "Gen3 x16", "Gen3 x16", false, false},
		{"future Gen6 x16 link parses", "0, 6, 6, 16, 16, P0, 50", "Gen6 x16", "Gen6 x16", false, false},
		{"Gen6 board negotiated Gen4 under load is a downshift", "0, 4, 6, 16, 16, P0, 90", "Gen4 x16", "Gen6 x16", true, false},
		{"riser-limited x4 on an x16 card", "0, 4, 4, 4, 16, P8, 0", "Gen4 x4", "Gen4 x16", true, true},
	}
	for _, tt := range tests {
		info, errs := parsePCIeCSV(tt.line)
		if len(errs) != 0 {
			t.Errorf("%s: unexpected errors %+v", tt.name, errs)
		}
		cur := info.CurrentSpeed + " " + info.CurrentWidth
		max := info.MaxSpeed + " " + info.MaxWidth
		if cur != tt.wantCur || max != tt.wantMax {
			t.Errorf("%s: current %q max %q, want %q %q", tt.name, cur, max, tt.wantCur, tt.wantMax)
		}
		if info.Downshifted != tt.wantDownshifted {
			t.Errorf("%s: Downshifted = %v, want %v", tt.name, info.Downshifted, tt.wantDownshifted)
		}
		if info.IdleLikely != tt.wantIdle {
			t.Errorf("%s: IdleLikely = %v, want %v", tt.name, info.IdleLikely, tt.wantIdle)
		}
	}
}

func TestParsePCIeCSV_NotAvailable(t *testing.T) {
	// Some virtualised / MIG configurations report [N/A] for link state.
	info, errs := parsePCIeCSV("0, [N/A], [N/A], [N/A], [N/A], [N/A], [N/A]")
	if len(errs) != 0 {
		t.Errorf("[N/A] must not produce parse errors, got %+v", errs)
	}
	if info.CurrentSpeed != "" || info.MaxSpeed != "" || info.CurrentWidth != "" || info.MaxWidth != "" || info.PowerState != "" {
		t.Errorf("[N/A] fields should stay empty, got %+v", info)
	}
	if info.Downshifted || info.IdleLikely {
		t.Errorf("unknown data must not be reported as downshifted or idle: %+v", info)
	}
}

// A 3-GPU rig (RTX 3090 + 2x RTX 4090): GPU 2 is busy at Gen1 and must be
// flagged, GPU 0 is idle at Gen1 and must not, GPU 1 is at full speed.
const pcieThreeGPU = "0, 1, 4, 16, 16, P8, 2\n1, 4, 4, 16, 16, P0, 98\n2, 1, 4, 16, 16, P0, 99\n"

func TestParsePCIeRows_ThreeGPUs(t *testing.T) {
	rows, _ := csvRows(pcieThreeGPU)
	infos, errs := parsePCIeRows(rows)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if len(infos) != 3 {
		t.Fatalf("got %d entries, want 3", len(infos))
	}
	if infos[0].GPUIndex != 0 || infos[0].Downshifted || !infos[0].IdleLikely {
		t.Errorf("GPU 0 idle at Gen1 must not be a downshift: %+v", infos[0])
	}
	if infos[1].GPUIndex != 1 || infos[1].Downshifted || infos[1].CurrentSpeed != "Gen4" {
		t.Errorf("GPU 1 at Gen4 is healthy: %+v", infos[1])
	}
	if infos[2].GPUIndex != 2 || !infos[2].Downshifted || infos[2].IdleLikely {
		t.Errorf("GPU 2 busy at Gen1 must be flagged: %+v", infos[2])
	}
}

func TestParsePCIeRows_ZeroAndShortRows(t *testing.T) {
	infos, errs := parsePCIeRows(nil)
	if len(infos) != 0 || len(errs) != 0 {
		t.Errorf("nil rows: got %d infos, %d errs", len(infos), len(errs))
	}
	for _, short := range []string{"0", "0,", "0, 4", "0, 4, 4, 16"} {
		info, errs := parsePCIeRow(short, 0)
		if len(errs) != 0 {
			t.Errorf("%q: unexpected errors %+v", short, errs)
		}
		if info.Downshifted || info.IdleLikely {
			t.Errorf("%q: truncated row must not assert anything: %+v", short, info)
		}
		if strings.HasPrefix(short, "0, 4") && info.CurrentSpeed != "Gen4" {
			t.Errorf("%q: current gen lost: %+v", short, info)
		}
	}
}

func TestParsePCIeCSV_Malformed(t *testing.T) {
	_, errs := parsePCIeCSV("0, x, 4, 16, 16, P0, 99")
	if len(errs) != 1 || errs[0].Collector != "pcie.current_gen" {
		t.Errorf("expected one current_gen error, got %+v", errs)
	}
}

func TestIsActivePState(t *testing.T) {
	active := []string{"P0", "P1", "P2", "P3", "P4", "p0"}
	notActive := []string{"P5", "P8", "P12", "P13", "P15", "", "[N/A]", "N/A", "P16", "Px"}
	for _, p := range active {
		if !isActivePState(p) {
			t.Errorf("isActivePState(%q) = false, want true", p)
		}
	}
	for _, p := range notActive {
		if isActivePState(p) {
			t.Errorf("isActivePState(%q) = true, want false", p)
		}
	}
}

func TestIsIdlePState(t *testing.T) {
	// NVML defines P0..P15; everything from P5 up is a power-saving state.
	idle := []string{"P5", "P8", "P12", "P13", "P15", "p8"}
	busy := []string{"P0", "P1", "P2", "P3", "P4", "", "N/A", "P16", "P-1"}
	for _, p := range idle {
		if !isIdlePState(p) {
			t.Errorf("isIdlePState(%q) = false, want true", p)
		}
	}
	for _, p := range busy {
		if isIdlePState(p) {
			t.Errorf("isIdlePState(%q) = true, want false", p)
		}
	}
}

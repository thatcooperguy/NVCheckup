package common

import (
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
	}

	for _, tt := range tests {
		result := formatPCIeWidth(tt.width)
		if result != tt.expected {
			t.Errorf("formatPCIeWidth(%d) = %s, want %s", tt.width, result, tt.expected)
		}
	}
}

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
		{"idle gen1", "1, 4, 16, 16, P8, 28", false, true, "P8", 28, "Gen1", "x16"},
		// Busy at P0 but still Gen1: a real downshift.
		{"busy gen1", "1, 4, 16, 16, P0, 95", true, false, "P0", 95, "Gen1", "x16"},
		// Width below max is a fault regardless of load.
		{"width x8", "4, 4, 8, 16, P0, 95", true, false, "P0", 95, "Gen4", "x8"},
		// Width below max even when idle.
		{"width x8 idle", "1, 4, 8, 16, P8, 3", true, true, "P8", 3, "Gen1", "x8"},
		// Low utilization at P0 counts as idle, so a gen drop is tolerated.
		{"p0 low util", "1, 4, 16, 16, P0, 5", false, true, "P0", 5, "Gen1", "x16"},
		{"full speed", "4, 4, 16, 16, P0, 99", false, false, "P0", 99, "Gen4", "x16"},
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

func TestParsePCIeCSV_NotAvailable(t *testing.T) {
	// Some virtualised / MIG configurations report [N/A] for link state.
	info, errs := parsePCIeCSV("[N/A], [N/A], [N/A], [N/A], [N/A], [N/A]")
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

func TestParsePCIeCSV_MultiGPUFirstLine(t *testing.T) {
	out := "4, 4, 16, 16, P0, 99\n1, 4, 16, 16, P8, 0\n"
	info, _ := parsePCIeCSV(firstLine(out))
	if info.CurrentSpeed != "Gen4" || info.PowerState != "P0" || info.UtilizationPct != 99 {
		t.Errorf("expected first GPU row, got %+v", info)
	}
}

func TestParsePCIeCSV_Malformed(t *testing.T) {
	_, errs := parsePCIeCSV("x, 4, 16, 16, P0, 99")
	if len(errs) != 1 || errs[0].Collector != "pcie.current_gen" {
		t.Errorf("expected one current_gen error, got %+v", errs)
	}
}

func TestIsIdlePState(t *testing.T) {
	idle := []string{"P5", "P8", "P12", "p8"}
	busy := []string{"P0", "P1", "P2", "P3", "P4", "", "N/A", "P13"}
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

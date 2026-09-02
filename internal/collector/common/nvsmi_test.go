package common

import (
	"errors"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/util"
)

// The three nvidia-smi failure modes that happen on machines whose driver is
// fine: an Optimus laptop with the dGPU powered off, no visible device, and a
// container started without the GPU. Each must become one clear error that
// quotes nvidia-smi and explains the likely cause; none may be parsed as a GPU.
func TestNvidiaSmiFailureModes(t *testing.T) {
	exitErr := errors.New("exit status 255")
	cases := []struct {
		name     string
		result   util.CommandResult
		wantText string
		wantHint string
	}{
		{
			name:     "Optimus dGPU powered off",
			result:   util.CommandResult{Stdout: "Unable to determine the device handle for GPU 0000:01:00.0: Unknown Error", ExitCode: 255, Err: exitErr},
			wantText: `"Unable to determine the device handle for GPU 0000:01:00.0: Unknown Error"`,
			wantHint: "Optimus",
		},
		{
			name:     "no devices visible",
			result:   util.CommandResult{Stdout: "No devices were found", ExitCode: 6, Err: errors.New("exit status 6")},
			wantText: `"No devices were found"`,
			wantHint: "no NVIDIA GPU is visible",
		},
		{
			name:     "container without GPU mapped",
			result:   util.CommandResult{Stderr: "Failed to initialize NVML: Unknown Error", ExitCode: 255, Err: exitErr},
			wantText: `"Failed to initialize NVML: Unknown Error"`,
			wantHint: "container",
		},
		{
			name:     "driver not loaded",
			result:   util.CommandResult{Stdout: "NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver. Make sure that the latest NVIDIA driver is installed and running.", ExitCode: 9, Err: errors.New("exit status 9")},
			wantText: `couldn't communicate with the NVIDIA driver`,
			wantHint: "not loaded",
		},
	}
	for _, c := range cases {
		e := nvidiaSmiQueryError("thermal.query", "thermal query", c.result)
		if e.Collector != "thermal.query" || !e.Fatal {
			t.Errorf("%s: collector/fatal wrong: %+v", c.name, e)
		}
		if !strings.Contains(e.Error, c.wantText) {
			t.Errorf("%s: error should quote nvidia-smi text %s, got %q", c.name, c.wantText, e.Error)
		}
		if !strings.Contains(e.Error, c.wantHint) {
			t.Errorf("%s: error should explain the cause (%q), got %q", c.name, c.wantHint, e.Error)
		}
		// The failure text is not CSV and must never be parsed as a GPU row.
		rows, other := csvRows(c.result.Stdout)
		if len(rows) != 0 && !strings.Contains(c.result.Stdout, ",") {
			t.Errorf("%s: failure text parsed as %d CSV rows", c.name, len(rows))
		}
		if quoted, _, known := describeNvidiaSmiFailure(c.result.Stdout + "\n" + c.result.Stderr); !known || quoted == "" {
			t.Errorf("%s: failure text not recognised (rows=%v other=%v)", c.name, rows, other)
		}
	}
}

// A failure that is not one of the recognised messages still yields a
// one-line error built from stderr/stdout/exit code rather than nothing.
func TestNvidiaSmiQueryError_Unrecognised(t *testing.T) {
	e := nvidiaSmiQueryError("pcie.query", "PCIe query", util.CommandResult{Stdout: "Field \"foo\" is not a valid field to query.", ExitCode: 2, Err: errors.New("exit status 2")})
	if !strings.Contains(e.Error, "not a valid field to query") || !strings.HasPrefix(e.Error, "nvidia-smi PCIe query failed: ") {
		t.Errorf("unexpected error text %q", e.Error)
	}
	if _, _, known := describeNvidiaSmiFailure("0, 43, P8"); known {
		t.Error("a healthy CSV row must not be classified as a failure")
	}
}

func TestCsvRows(t *testing.T) {
	rows, other := csvRows("\n0, 43, P8\n\nWARNING: infoROM is corrupted at gpu 0000:41:00.0\n1, 50, P0\n")
	if len(rows) != 2 || rows[0] != "0, 43, P8" || rows[1] != "1, 50, P0" {
		t.Errorf("rows = %q", rows)
	}
	if len(other) != 1 || !strings.HasPrefix(other[0], "WARNING") {
		t.Errorf("other = %q", other)
	}
	if rows, other := csvRows(""); len(rows) != 0 || len(other) != 0 {
		t.Errorf("empty output: rows=%q other=%q", rows, other)
	}
}

func TestParseRowIndex(t *testing.T) {
	idx, rest := parseRowIndex(splitCSV("2, 4, 4"), 7)
	if idx != 2 || len(rest) != 2 {
		t.Errorf("explicit index: got %d %v", idx, rest)
	}
	idx, rest = parseRowIndex(splitCSV("[N/A], 4"), 7)
	if idx != 7 || len(rest) != 1 {
		t.Errorf("[N/A] index should fall back to ordinal: got %d %v", idx, rest)
	}
	idx, rest = parseRowIndex(nil, 3)
	if idx != 3 || rest != nil {
		t.Errorf("empty fields: got %d %v", idx, rest)
	}
	if n, ok := parseSmallInt("15"); !ok || n != 15 {
		t.Errorf("parseSmallInt(15) = %d %v", n, ok)
	}
	for _, bad := range []string{"", "-1", "1.5", "P8", "99999999999"} {
		if _, ok := parseSmallInt(bad); ok {
			t.Errorf("parseSmallInt(%q) should fail", bad)
		}
	}
}

// A host without nvidia-smi is a Fatal collector error on a desktop or server,
// but nothing at all on Jetson, where JetPack simply does not ship nvidia-smi
// and the analyzer's jetson-detected finding explains the missing data.
func TestMissingNvidiaSmiError(t *testing.T) {
	for _, collector := range []string{"thermal", "pcie"} {
		e := missingNvidiaSmiError(collector, false)
		if e == nil || e.Collector != collector || !e.Fatal || !strings.Contains(e.Error, "nvidia-smi not found in PATH") {
			t.Errorf("%s on a non-Jetson host: got %+v, want a Fatal 'nvidia-smi not found in PATH' error", collector, e)
		}
		if e := missingNvidiaSmiError(collector, true); e != nil {
			t.Errorf("%s on Jetson must not report a collector error, got %+v", collector, e)
		}
	}
}

// A multi-GPU rig where nvidia-smi exits 0, prints rows for the healthy GPUs
// and a failure line for one dead GPU must keep the healthy rows and surface
// the failure as a non-fatal note rather than discarding everything.
func TestCsvRowsKeepHealthyRowsNextToFailureLine(t *testing.T) {
	out := `0, 44, P8, 210, 2100, 350.00, 31.99, 0, 11
Unable to determine the device handle for GPU 0000:41:00.0: Unknown Error
2, 61, P0, 1980, 2100, 450.00, 402.10, [N/A], 100
`
	rows, other := csvRows(out)
	if len(rows) != 2 {
		t.Fatalf("expected 2 CSV rows, got %d: %v", len(rows), rows)
	}
	if len(other) != 1 {
		t.Fatalf("expected the failure line to be reported separately, got %v", other)
	}
	if _, _, known := describeNvidiaSmiFailure(other[0]); !known {
		t.Fatalf("expected %q to be a recognised nvidia-smi failure", other[0])
	}
}

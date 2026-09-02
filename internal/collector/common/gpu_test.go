package common

import (
	"strings"
	"testing"
)

// Captured from nvidia-smi 591.86 on an RTX 3090 (process names altered).
const nvidiaSmiTableWithProcesses = `Tue Sep  1 19:10:20 2026       
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 591.86                 Driver Version: 591.86         CUDA Version: 13.1     |
+-----------------------------------------+------------------------+----------------------+
| GPU  Name                  Driver-Model | Bus-Id          Disp.A | Volatile Uncorr. ECC |
| Fan  Temp   Perf          Pwr:Usage/Cap |           Memory-Usage | GPU-Util  Compute M. |
|                                         |                        |               MIG M. |
|=========================================+========================+======================|
|   0  NVIDIA GeForce RTX 3090      WDDM  |   00000000:41:00.0  On |                  N/A |
|  0%   36C    P8             30W /  350W |    3545MiB /  24576MiB |     15%      Default |
|                                         |                        |                  N/A |
+-----------------------------------------+------------------------+----------------------+

+-----------------------------------------------------------------------------------------+
| Processes:                                                                              |
|  GPU   GI   CI              PID   Type   Process name                        GPU Memory |
|        ID   ID                                                               Usage      |
|=========================================================================================|
|    0   N/A  N/A           12664    C+G   ...indows\System32\ShellHost.exe      N/A      |
|    0   N/A  N/A           20488    C+G   ...4__8wekyb3d8bbwe\ms-teams.exe      N/A      |
|    0   N/A  N/A           32304    C+G   ...secret-project\app\private.exe      N/A      |
+-----------------------------------------------------------------------------------------+
`

func TestStripProcessSection(t *testing.T) {
	got := stripProcessSection(nvidiaSmiTableWithProcesses)

	for _, private := range []string{"Processes:", "ShellHost.exe", "ms-teams.exe", "private.exe", "PID"} {
		if strings.Contains(got, private) {
			t.Errorf("stored output still contains %q", private)
		}
	}
	for _, keep := range []string{"Driver Version: 591.86", "CUDA Version: 13.1", "NVIDIA GeForce RTX 3090", "3545MiB /  24576MiB"} {
		if !strings.Contains(got, keep) {
			t.Errorf("stored output lost GPU table content %q", keep)
		}
	}
	if !strings.Contains(got, processSectionOmittedNote) {
		t.Errorf("expected omission note in output")
	}

	// The GPU table's own closing border stays; the Processes box border goes.
	lines := strings.Split(got, "\n")
	if len(lines) < 3 {
		t.Fatalf("too few lines kept: %d", len(lines))
	}
	if lines[len(lines)-1] != processSectionOmittedNote {
		t.Errorf("last line = %q, want omission note", lines[len(lines)-1])
	}
	if !isTableBorder(lines[len(lines)-2]) {
		t.Errorf("expected GPU table closing border before note, got %q", lines[len(lines)-2])
	}
	if strings.TrimSpace(lines[len(lines)-3]) == "" {
		t.Errorf("dangling Processes box border was not removed")
	}
}

func TestStripProcessSection_NoProcesses(t *testing.T) {
	in := "+---+\n| NVIDIA-SMI 535.00 |\n+---+\n"
	if got := stripProcessSection(in); got != in {
		t.Errorf("output without Processes section should be unchanged, got %q", got)
	}
}

func TestIsTableBorder(t *testing.T) {
	yes := []string{"+---+", "+-----------------------------------------+------------------------+", "|=====|"}
	// "|=====|" is a header separator, not a border, so it must be false.
	if !isTableBorder(yes[0]) || !isTableBorder(yes[1]) {
		t.Error("expected borders to be recognised")
	}
	if isTableBorder(yes[2]) || isTableBorder("| GPU  Name |") || isTableBorder("") {
		t.Error("non-border lines recognised as borders")
	}
}

func TestGPUQueryFields(t *testing.T) {
	if !strings.Contains(GPUQueryFields, "driver_version") || !strings.Contains(GPUQueryFields, "pci.bus_id") {
		t.Fatalf("GPUQueryFields missing expected fields: %s", GPUQueryFields)
	}
}

// Package selftest verifies environment, dependencies, and permissions.
package selftest

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// CheckResult holds a single self-test check result
type CheckResult struct {
	Name   string
	Status string // "OK", "INFO", "WARN", "FAIL"
	Detail string
}

// queryCheck pairs a collector name with the exact nvidia-smi field list it uses,
// so self-test exercises the same query strings the collectors will run.
type queryCheck struct {
	name   string
	fields string
}

// Run executes all self-test checks and returns an exit code.
func Run() int {
	fmt.Println("NVCheckup Self-Test")
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()

	var results []CheckResult

	// Check 1: OS detection
	results = append(results, checkOS())

	// Check 2: Architecture
	results = append(results, checkArch())

	// Check 3: nvidia-smi presence, then the collectors' actual query strings.
	// A working "nvidia-smi -L" says nothing about whether the driver accepts
	// every field the collectors ask for (an invalid field made thermal
	// collection fail silently for a long time), so each list is run verbatim.
	smi := checkNvidiaSmi()
	results = append(results, smi)
	if smi.Status == "OK" {
		results = append(results, checkNvidiaSmiQueries()...)
	}

	// Check 4: Write permissions
	results = append(results, checkWritePermissions())

	// Check 5: Python (for AI mode)
	results = append(results, checkPython())

	// Check 6: Elevation (some collectors degrade without it)
	results = append(results, checkElevation())

	// Platform-specific checks
	if runtime.GOOS == "windows" {
		results = append(results, checkPowerShell())
	}
	if runtime.GOOS == "linux" {
		results = append(results, checkLspci())
		results = append(results, checkModinfo())
	}

	// Print results. INFO rows are context only: they are rendered but do not
	// count towards the exit code, so a healthy machine that simply is not
	// running as Administrator still exits 0.
	okCount, infoCount, warnCount, failCount := 0, 0, 0, 0
	toolMissing := false
	for _, r := range results {
		icon := "  "
		switch r.Status {
		case "OK":
			icon = "OK  "
			okCount++
		case "INFO":
			icon = "INFO"
			infoCount++
		case "WARN":
			icon = "WARN"
			warnCount++
			if concernsTool(r) {
				toolMissing = true
			}
		case "FAIL":
			icon = "FAIL"
			failCount++
			if concernsTool(r) {
				toolMissing = true
			}
		}
		fmt.Printf("  [%s] %-30s %s\n", icon, r.Name, r.Detail)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("  Results: %d OK, %d INFO, %d WARN, %d FAIL\n", okCount, infoCount, warnCount, failCount)
	fmt.Println()

	switch {
	case failCount > 0:
		fmt.Println("  Some checks failed. NVCheckup will still run but may produce")
		fmt.Println("  incomplete results. See details above.")
		return types.ExitCritical
	case warnCount > 0 && toolMissing:
		fmt.Println("  Some optional tools are missing or not working. NVCheckup will")
		fmt.Println("  run but the checks that depend on them will be skipped.")
		return types.ExitWarnings
	case warnCount > 0:
		fmt.Println("  Some checks reported warnings. NVCheckup will run but the")
		fmt.Println("  affected data may be incomplete. See details above.")
		return types.ExitWarnings
	}
	fmt.Println("  All checks passed. NVCheckup is ready to run.")
	return types.ExitOK
}

// toolChecks names the checks that verify an external tool is present and
// usable, so the footer only talks about "missing tools" when one of them
// actually warned or failed.
var toolChecks = map[string]bool{
	"nvidia-smi": true,
	"Python":     true,
	"PowerShell": true,
	"lspci":      true,
	"modinfo":    true,
}

// concernsTool reports whether a check result is about an external tool.
func concernsTool(r CheckResult) bool {
	return toolChecks[r.Name]
}

func checkOS() CheckResult {
	return CheckResult{
		Name:   "Operating System",
		Status: "OK",
		Detail: fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

func checkArch() CheckResult {
	arch := runtime.GOARCH
	if arch == "amd64" || arch == "arm64" {
		return CheckResult{
			Name:   "Architecture",
			Status: "OK",
			Detail: arch,
		}
	}
	return CheckResult{
		Name:   "Architecture",
		Status: "WARN",
		Detail: fmt.Sprintf("%s (untested architecture)", arch),
	}
}

func checkNvidiaSmi() CheckResult {
	if !util.CommandExists("nvidia-smi") {
		return CheckResult{
			Name:   "nvidia-smi",
			Status: "WARN",
			Detail: "Not found in PATH (NVIDIA driver may not be installed)",
		}
	}
	r := util.RunCommand(5, "nvidia-smi", "-L")
	if r.Err != nil {
		return CheckResult{
			Name:   "nvidia-smi",
			Status: "WARN",
			Detail: fmt.Sprintf("Found but failed: %s", r.Err.Error()),
		}
	}
	lines := strings.Split(strings.TrimSpace(r.Stdout), "\n")
	return CheckResult{
		Name:   "nvidia-smi",
		Status: "OK",
		Detail: fmt.Sprintf("Found, %d GPU(s) detected", len(lines)),
	}
}

// checkNvidiaSmiQueries runs every --query-gpu field list the collectors use
// and reports the driver's reason when one is rejected.
func checkNvidiaSmiQueries() []CheckResult {
	checks := []queryCheck{
		{"nvidia-smi gpu query", common.GPUQueryFields},
		{"nvidia-smi thermal query", common.ThermalQueryFields},
		{"nvidia-smi pcie query", common.PCIeQueryFields},
	}
	var results []CheckResult
	for _, c := range checks {
		results = append(results, runQueryCheck(c.name, c.fields))
	}
	results = append(results, checkClockEventQuery())
	return results
}

// runQueryCheck executes one nvidia-smi --query-gpu list exactly as a collector would.
func runQueryCheck(name, fields string) CheckResult {
	r := util.RunCommand(10, "nvidia-smi", "--query-gpu="+fields, "--format=csv,noheader,nounits")
	if r.Err != nil {
		return CheckResult{
			Name:   name,
			Status: "WARN",
			Detail: fmt.Sprintf("Rejected (exit %d): %s", r.ExitCode, failureDetail(r)),
		}
	}
	return CheckResult{
		Name:   name,
		Status: "OK",
		Detail: fmt.Sprintf("%d field(s) accepted", strings.Count(fields, ",")+1),
	}
}

// checkClockEventQuery verifies the clock event reasons field, accepting the
// legacy spelling on older drivers (the thermal collector falls back the same way).
func checkClockEventQuery() CheckResult {
	const name = "nvidia-smi clock events"
	r := util.RunCommand(10, "nvidia-smi", "--query-gpu="+common.ThermalEventQueryFields, "--format=csv,noheader")
	if r.Err == nil {
		return CheckResult{Name: name, Status: "OK", Detail: common.ThermalEventQueryFields + " accepted"}
	}
	modernErr := failureDetail(r)
	r = util.RunCommand(10, "nvidia-smi", "--query-gpu="+common.ThermalEventQueryFieldsLegacy, "--format=csv,noheader")
	if r.Err == nil {
		return CheckResult{
			Name:   name,
			Status: "INFO",
			Detail: fmt.Sprintf("Legacy field %s in use (%s rejected: %s)", common.ThermalEventQueryFieldsLegacy, common.ThermalEventQueryFields, modernErr),
		}
	}
	return CheckResult{
		Name:   name,
		Status: "WARN",
		Detail: fmt.Sprintf("Both field names rejected (exit %d): %s", r.ExitCode, failureDetail(r)),
	}
}

// failureDetail returns a one-line reason for a failed command: the first
// non-empty of trimmed stderr, the first line of stdout, and the Go error.
// nvidia-smi prints 'Field "x" is not a valid field to query.' to STDOUT with
// exit 2 and an empty stderr, so looking at stderr alone loses the reason.
func failureDetail(r util.CommandResult) string {
	if s := firstLine(strings.TrimSpace(r.Stderr)); s != "" {
		return s
	}
	if s := firstLine(strings.TrimSpace(r.Stdout)); s != "" {
		return s
	}
	if r.Err != nil {
		return r.Err.Error()
	}
	return fmt.Sprintf("exit %d", r.ExitCode)
}

// checkElevation reports whether the process is elevated and which checks
// degrade otherwise. Both outcomes are INFO: running unelevated is the normal
// state for most users and nothing is broken, it just means a few collectors
// return partial data. Reporting it as WARN made self-test exit 1 on every
// healthy non-admin machine.
func checkElevation() CheckResult {
	const name = "Elevation"
	if isElevated() {
		return CheckResult{Name: name, Status: "INFO", Detail: "Running elevated; all checks available"}
	}
	var degraded string
	if runtime.GOOS == "windows" {
		degraded = "Windows Event Log (4101 driver resets, nvlddmkm, WHEA) and Confirm-SecureBootUEFI may be incomplete"
	} else {
		degraded = "dmesg/Xid history and some sysfs/DKMS state may be incomplete"
	}
	return CheckResult{
		Name:   name,
		Status: "INFO",
		Detail: "Not elevated: " + degraded + " (re-run as Administrator/root for full coverage)",
	}
}

// isElevated reports whether the current process has administrative rights.
// On Windows "net session" succeeds only from an elevated token; on Linux
// root has effective uid 0.
func isElevated() bool {
	if runtime.GOOS == "windows" {
		r := util.RunCommand(5, "net", "session")
		return r.Err == nil && r.ExitCode == 0
	}
	return os.Geteuid() == 0
}

// firstLine returns the first line of s, for compact single-line details.
func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// checkWritePermissions probes whether the current directory is writable by
// creating a uniquely named temporary file. os.CreateTemp never overwrites an
// existing file (it uses O_EXCL with a random suffix), so a user file that
// happens to share the prefix is safe. The probe is removed afterwards.
func checkWritePermissions() CheckResult {
	f, err := os.CreateTemp(".", ".nvcheckup-selftest-*")
	if err != nil {
		return CheckResult{
			Name:   "Write Permissions",
			Status: "FAIL",
			Detail: "Cannot write to current directory: " + err.Error(),
		}
	}
	name := f.Name()
	_, writeErr := f.Write([]byte("test"))
	closeErr := f.Close()
	os.Remove(name)
	if writeErr != nil || closeErr != nil {
		return CheckResult{
			Name:   "Write Permissions",
			Status: "FAIL",
			Detail: "Cannot write to current directory",
		}
	}
	return CheckResult{
		Name:   "Write Permissions",
		Status: "OK",
		Detail: "Can write to current directory",
	}
}

func checkPython() CheckResult {
	for _, cmd := range []string{"python3", "python", "py"} {
		if util.CommandExists(cmd) {
			r := util.RunCommand(5, cmd, "--version")
			if r.Err == nil {
				return CheckResult{
					Name:   "Python",
					Status: "OK",
					Detail: strings.TrimSpace(r.Stdout + r.Stderr),
				}
			}
		}
	}
	return CheckResult{
		Name:   "Python",
		Status: "WARN",
		Detail: "Not found (AI mode checks will be limited)",
	}
}

func checkPowerShell() CheckResult {
	if !util.CommandExists("powershell") {
		return CheckResult{
			Name:   "PowerShell",
			Status: "FAIL",
			Detail: "Not found (required for Windows diagnostics)",
		}
	}
	return CheckResult{
		Name:   "PowerShell",
		Status: "OK",
		Detail: "Available",
	}
}

func checkLspci() CheckResult {
	if !util.CommandExists("lspci") {
		return CheckResult{
			Name:   "lspci",
			Status: "WARN",
			Detail: "Not found (install pciutils for GPU enumeration)",
		}
	}
	return CheckResult{
		Name:   "lspci",
		Status: "OK",
		Detail: "Available",
	}
}

func checkModinfo() CheckResult {
	if !util.CommandExists("modinfo") {
		return CheckResult{
			Name:   "modinfo",
			Status: "WARN",
			Detail: "Not found (needed for kernel module checks)",
		}
	}
	return CheckResult{
		Name:   "modinfo",
		Status: "OK",
		Detail: "Available",
	}
}

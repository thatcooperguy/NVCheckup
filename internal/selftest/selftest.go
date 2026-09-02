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

	// Print results
	okCount, warnCount, failCount := 0, 0, 0
	for _, r := range results {
		icon := "  "
		switch r.Status {
		case "OK":
			icon = "OK  "
			okCount++
		case "INFO":
			icon = "INFO"
		case "WARN":
			icon = "WARN"
			warnCount++
		case "FAIL":
			icon = "FAIL"
			failCount++
		}
		fmt.Printf("  [%s] %-30s %s\n", icon, r.Name, r.Detail)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("  Results: %d OK, %d WARN, %d FAIL\n", okCount, warnCount, failCount)
	fmt.Println()

	if failCount > 0 {
		fmt.Println("  Some checks failed. NVCheckup will still run but may produce")
		fmt.Println("  incomplete results. See details above.")
		return types.ExitCritical
	}
	if warnCount > 0 {
		fmt.Println("  Some optional tools are missing. NVCheckup will work but some")
		fmt.Println("  checks may be skipped.")
		return types.ExitWarnings
	}
	fmt.Println("  All checks passed. NVCheckup is ready to run.")
	return types.ExitOK
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
// and reports the driver stderr when one is rejected.
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
		detail := strings.TrimSpace(r.Stderr)
		if detail == "" {
			detail = r.Err.Error()
		}
		return CheckResult{
			Name:   name,
			Status: "WARN",
			Detail: fmt.Sprintf("Rejected (exit %d): %s", r.ExitCode, firstLine(detail)),
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
	modernErr := firstLine(strings.TrimSpace(r.Stderr))
	r = util.RunCommand(10, "nvidia-smi", "--query-gpu="+common.ThermalEventQueryFieldsLegacy, "--format=csv,noheader")
	if r.Err == nil {
		return CheckResult{
			Name:   name,
			Status: "INFO",
			Detail: fmt.Sprintf("Legacy field %s in use (%s rejected: %s)", common.ThermalEventQueryFieldsLegacy, common.ThermalEventQueryFields, modernErr),
		}
	}
	detail := strings.TrimSpace(r.Stderr)
	if detail == "" {
		detail = r.Err.Error()
	}
	return CheckResult{
		Name:   name,
		Status: "WARN",
		Detail: fmt.Sprintf("Both field names rejected (exit %d): %s", r.ExitCode, firstLine(detail)),
	}
}

// checkElevation reports whether the process is elevated and which checks
// degrade otherwise. Elevated is INFO (nothing to act on); not elevated is WARN
// because several Windows collectors return partial data without it.
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
		Status: "WARN",
		Detail: "Not elevated: " + degraded,
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

func checkWritePermissions() CheckResult {
	testFile := ".nvcheckup-selftest-write"
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		return CheckResult{
			Name:   "Write Permissions",
			Status: "FAIL",
			Detail: "Cannot write to current directory",
		}
	}
	os.Remove(testFile)
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

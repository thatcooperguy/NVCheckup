// Package selftest verifies environment, dependencies, and permissions.
package selftest

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CheckResult holds a single self-test check result
type CheckResult struct {
	Name   string
	Status string // "OK", "WARN", "FAIL"
	Detail string
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

	// Check 3: nvidia-smi
	results = append(results, checkNvidiaSmi())

	// Check 4: Write permissions
	results = append(results, checkWritePermissions())

	// Check 5: Python (for AI mode)
	results = append(results, checkPython())

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

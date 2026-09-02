//go:build linux

package linux

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// TestParseLdconfigLibcuda_ShimFormat feeds the exact rows the CI ldconfig
// shim prints (.github/fieldtest/shims/ldconfig: header line, then one
// tab-indented cache row per scenario entry, AArch64 on the GB10 job).
func TestParseLdconfigLibcuda_ShimFormat(t *testing.T) {
	out := "3 libs found in cache `/etc/ld.so.cache'\n" +
		"\tlibnvidia-ml.so.1 (libc6,AArch64) => /usr/lib/aarch64-linux-gnu/libnvidia-ml.so.1\n" +
		"\tlibcuda.so.1 (libc6,AArch64) => /usr/lib/aarch64-linux-gnu/libcuda.so.1\n" +
		"\tlibcuda.so (libc6,AArch64) => /usr/lib/aarch64-linux-gnu/libcuda.so\n"
	if got := parseLdconfigLibcuda(out); got != "/usr/lib/aarch64-linux-gnu/libcuda.so.1" {
		t.Errorf("parseLdconfigLibcuda(shim) = %q", got)
	}
	// rig3 has no ldconfig_libs: the shim prints an empty cache.
	if got := parseLdconfigLibcuda("0 libs found in cache `/etc/ld.so.cache'\n"); got != "" {
		t.Errorf("empty cache must give empty, got %q", got)
	}
}

// TestDevNodesAndLibcuda_SimRoot: /dev/nvidia* and the libcuda fallback are
// read through NVC_SIM_ROOT and recorded as logical paths.
func TestDevNodesAndLibcuda_SimRoot(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"dev/nvidia0", "dev/nvidiactl", "dev/nvidia-uvm", "usr/lib/aarch64-linux-gnu/libcuda.so.1"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(common.SimRootEnv, root)
	t.Setenv("PATH", t.TempDir()) // no ldconfig: exercise the file fallback

	var info types.LinuxInfo
	var errs []types.CollectorError
	collectDevNodes(&info, &errs, 5)
	if len(info.DevNvidiaNodes) != 3 || info.DevNvidiaNodes[1] != "/dev/nvidia0" {
		t.Errorf("DevNvidiaNodes = %v", info.DevNvidiaNodes)
	}
	collectLibCuda(&info, &errs, 5)
	if info.LibCudaPath != "/usr/lib/aarch64-linux-gnu/libcuda.so.1" {
		t.Errorf("LibCudaPath = %q", info.LibCudaPath)
	}
	collectSecureBoot(&info, &errs, 5)
	if info.SecureBootState != "N/A (Legacy BIOS)" {
		t.Errorf("without sys/firmware/efi in the fixture: %q", info.SecureBootState)
	}
}

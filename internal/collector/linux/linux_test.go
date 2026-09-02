//go:build linux

package linux

import (
	"strings"
	"testing"
)

func TestParseLsmodGPUModules(t *testing.T) {
	out := "Module                  Size  Used by\n" +
		"nvidia_uvm           1798144  0\n" +
		"nvidia_drm             94208  3\n" +
		"nvidia_modeset       1306624  5 nvidia_drm\n" +
		"nvidia              56827904  86 nvidia_uvm,nvidia_modeset\n" +
		"video                  73728  1 nvidia_modeset\n" +
		"i915                 3620864  12\n"
	got := parseLsmodGPUModules(out)
	want := []string{"nvidia_uvm", "nvidia_drm", "nvidia_modeset", "nvidia"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("parseLsmodGPUModules = %v, want %v", got, want)
	}
	// Only the first column counts: "video ... nvidia_modeset" is not a GPU module.
	for _, m := range got {
		if m == "video" {
			t.Error("dependency column must not be matched")
		}
	}
	if got := parseLsmodGPUModules("Module Size Used by\ni915 3620864 12\n"); len(got) != 0 {
		t.Errorf("no GPU modules loaded must give an empty list, got %v", got)
	}
	if got := parseLsmodGPUModules("Module Size Used by\nnouveau 2662400 5\n"); len(got) != 1 || got[0] != "nouveau" {
		t.Errorf("nouveau should be listed, got %v", got)
	}
}

func TestParseNvidiaPackageList(t *testing.T) {
	dpkg := "Desired=Unknown/Install/Remove/Purge/Hold\n" +
		"||/ Name                         Version            Architecture Description\n" +
		"+++-============================-==================-============-=================================\n" +
		"ii  libnvidia-compute-550:amd64  550.107.02-0ubuntu1 amd64        NVIDIA libcompute package\n" +
		"ii  nvidia-driver-550            550.107.02-0ubuntu1 amd64        NVIDIA driver metapackage\n" +
		"ii  vim                          2:9.1.0016-1ubuntu7 amd64        Vi IMproved - enhanced vi editor\n"
	got := parseNvidiaPackageList("apt", dpkg)
	want := []string{"libnvidia-compute-550:amd64 550.107.02-0ubuntu1", "nvidia-driver-550 550.107.02-0ubuntu1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("apt: got %v, want %v", got, want)
	}

	rpm := "kernel-6.9.7-200.fc40.x86_64\nakmod-nvidia-555.58.02-1.fc40.x86_64\nxorg-x11-drv-nvidia-cuda-555.58.02-1.fc40.x86_64\n"
	got = parseNvidiaPackageList("dnf", rpm)
	if len(got) != 2 || got[0] != "akmod-nvidia-555.58.02-1.fc40.x86_64" {
		t.Errorf("dnf: got %v", got)
	}

	if got := parseNvidiaPackageList("pacman", "linux 6.9.7.arch1-1\nmesa 1:24.1.3-1\n"); len(got) != 0 {
		t.Errorf("no NVIDIA packages must give an empty list, got %v", got)
	}
	if got := parseNvidiaPackageList("pacman", "nvidia 555.58.02-1\nNVIDIA-utils 555.58.02-1\n"); len(got) != 2 {
		t.Errorf("match must be case-insensitive, got %v", got)
	}
}

func TestParseLdconfigLibcuda(t *testing.T) {
	out := "1234 libs found in cache `/etc/ld.so.cache'\n" +
		"\tlibcudart.so.12 (libc6,x86-64) => /usr/local/cuda/lib64/libcudart.so.12\n" +
		"\tlibcuda.so.1 (libc6,x86-64) => /lib/x86_64-linux-gnu/libcuda.so.1\n" +
		"\tlibcuda.so (libc6,x86-64) => /lib/x86_64-linux-gnu/libcuda.so\n"
	if got := parseLdconfigLibcuda(out); got != "/lib/x86_64-linux-gnu/libcuda.so.1" {
		t.Errorf("parseLdconfigLibcuda = %q", got)
	}
	if got := parseLdconfigLibcuda("10 libs found in cache\n\tlibc.so.6 (libc6,x86-64) => /lib/x86_64-linux-gnu/libc.so.6\n"); got != "" {
		t.Errorf("no libcuda must give empty, got %q", got)
	}
}

func TestFilterDmesgSnippets(t *testing.T) {
	var b strings.Builder
	b.WriteString("[ 0.0] Linux version 6.8.0\n")
	for i := 0; i < 60; i++ {
		b.WriteString("[ 1.0] NVRM: line ")
		b.WriteString(strings.Repeat("x", i%5))
		b.WriteString("\n")
	}
	b.WriteString("[ 2.0] usb 1-1: new device\n")
	b.WriteString("[ 3.0] nouveau 0000:01:00.0: DRM: fault\n")
	got := filterDmesgSnippets(b.String())
	lines := strings.Split(got, "\n")
	if len(lines) != maxDmesgSnippetLines {
		t.Errorf("expected %d lines, got %d", maxDmesgSnippetLines, len(lines))
	}
	if !strings.HasSuffix(got, "nouveau 0000:01:00.0: DRM: fault") {
		t.Error("the most recent matching line must be kept")
	}
	if strings.Contains(got, "usb 1-1") || strings.Contains(got, "Linux version") {
		t.Error("non-GPU lines must be dropped")
	}
	if got := filterDmesgSnippets("[ 0.0] Linux version 6.8.0\n"); got != "" {
		t.Errorf("no GPU lines must give empty, got %q", got)
	}
}

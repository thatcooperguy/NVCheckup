package common

import (
	"os"
	"path/filepath"
	"testing"
)

// The release line JetPack 6.1 writes to /etc/nv_tegra_release.
const tegraReleaseSample = "# R36 (release), REVISION: 4.3, GCID: 38968081, BOARD: generic, EABI: aarch64, DATE: Wed Jan  8 01:49:37 UTC 2025"

func TestParseTegraRelease(t *testing.T) {
	if got := parseTegraRelease(tegraReleaseSample + "\n"); got != tegraReleaseSample {
		t.Errorf("parseTegraRelease = %q", got)
	}
	// Leading blank lines and a NUL terminator are tolerated.
	if got := parseTegraRelease("\n\n" + tegraReleaseSample + "\x00\n# second line\n"); got != tegraReleaseSample {
		t.Errorf("parseTegraRelease with padding = %q", got)
	}
	if got := parseTegraRelease(""); got != "" {
		t.Errorf("empty file should give empty release, got %q", got)
	}
}

func TestIsJetsonModel(t *testing.T) {
	for _, m := range []string{"NVIDIA Jetson AGX Orin Developer Kit\x00", "NVIDIA Jetson Orin Nano Developer Kit", "NVIDIA Jetson Xavier NX Developer Kit\x00\x00"} {
		if !isJetsonModel([]byte(m)) {
			t.Errorf("isJetsonModel(%q) = false", m)
		}
	}
	for _, m := range []string{"Raspberry Pi 4 Model B Rev 1.4\x00", "", "\x00"} {
		if isJetsonModel([]byte(m)) {
			t.Errorf("isJetsonModel(%q) = true", m)
		}
	}
}

func TestDetectJetsonFrom(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "nv_tegra_release")
	model := filepath.Join(dir, "model")
	missing := filepath.Join(dir, "does-not-exist")

	// Neither file: a desktop or server.
	if is, rel := detectJetsonFrom(missing, missing); is || rel != "" {
		t.Errorf("no files: got (%v, %q)", is, rel)
	}

	// Full JetPack install: both files present.
	if err := os.WriteFile(release, []byte(tegraReleaseSample+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(model, []byte("NVIDIA Jetson AGX Orin Developer Kit\x00"), 0644); err != nil {
		t.Fatal(err)
	}
	if is, rel := detectJetsonFrom(release, model); !is || rel != tegraReleaseSample {
		t.Errorf("both files: got (%v, %q)", is, rel)
	}

	// Only the release file (container image built from L4T without device tree).
	if is, rel := detectJetsonFrom(release, missing); !is || rel != tegraReleaseSample {
		t.Errorf("release only: got (%v, %q)", is, rel)
	}

	// Only the device tree (release file removed by a minimal rootfs).
	if is, rel := detectJetsonFrom(missing, model); !is || rel != "" {
		t.Errorf("model only: got (%v, %q)", is, rel)
	}

	// A device tree that names another board is not a Jetson.
	if err := os.WriteFile(model, []byte("Raspberry Pi 4 Model B\x00"), 0644); err != nil {
		t.Fatal(err)
	}
	if is, _ := detectJetsonFrom(missing, model); is {
		t.Error("Raspberry Pi device tree detected as Jetson")
	}
}

// On the development machine (and CI) neither file exists, so the exported
// detector must report a non-Jetson host.
func TestDetectJetson_HostIsNotJetsonUnlessFilesExist(t *testing.T) {
	if _, err := os.Stat(tegraReleasePath); err == nil {
		t.Skip("running on a Jetson")
	}
	if _, err := os.Stat(deviceTreeModelPath); err == nil {
		t.Skip("running on a device-tree system")
	}
	if is, rel := DetectJetson(); is || rel != "" {
		t.Errorf("DetectJetson on a non-Jetson host = (%v, %q)", is, rel)
	}
}

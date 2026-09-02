package common

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestSimGlob_LogicalPaths: under NVC_SIM_ROOT the glob runs inside the
// fixture tree but the matches come back as the logical device paths a
// report on real hardware would show (spec section 10 contract).
func TestSimGlob_LogicalPaths(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"dev/nvidia0", "dev/nvidiactl", "dev/nvidia-uvm", "dev/null"} {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(SimRootEnv, root)
	got, err := SimGlob("/dev/nvidia*")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/dev/nvidia-uvm", "/dev/nvidia0", "/dev/nvidiactl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SimGlob = %v, want %v", got, want)
	}
	if got, _ := SimGlob("/dev/dxg*"); len(got) != 0 {
		t.Errorf("no match must give an empty list, got %v", got)
	}

	// Without a root the pattern is globbed where it points.
	t.Setenv(SimRootEnv, "")
	rel := filepath.Join(root, "dev", "nvidia*")
	got, err = SimGlob(rel)
	if err != nil || len(got) != 3 || got[0] != filepath.Join(root, "dev", "nvidia-uvm") {
		t.Errorf("SimGlob without root = %v, %v", got, err)
	}
}

func TestSimFileExists_AndReadSimFile(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "sys", "firmware", "efi")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc_version"), []byte("Linux version 6.17.0-1026-nvidia\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(SimRootEnv, root)
	if !SimFileExists("/sys/firmware/efi") {
		t.Error("mapped directory must exist")
	}
	if SimFileExists("/sys/firmware/absent") {
		t.Error("unmapped path must not exist")
	}
	data, err := ReadSimFile("/proc_version")
	if err != nil || string(data) != "Linux version 6.17.0-1026-nvidia\n" {
		t.Errorf("ReadSimFile = %q, %v", data, err)
	}
	// Relative paths are never mapped.
	if SimPath("report.json") != "report.json" {
		t.Error("relative path mapped")
	}
}

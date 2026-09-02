package bundle

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCreateZip_TwoFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "report.txt")
	b := filepath.Join(dir, "report.json")
	if err := os.WriteFile(a, []byte("hello text"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`{"ok":true}`), 0644); err != nil {
		t.Fatal(err)
	}

	zipPath, err := CreateZip(dir, []string{a, b})
	if err != nil {
		t.Fatalf("CreateZip: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(zipPath), "nvcheckup-bundle-") || !strings.HasSuffix(zipPath, ".zip") {
		t.Errorf("unexpected zip name: %s", zipPath)
	}

	rc, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("zip is not readable: %v", err)
	}
	defer rc.Close()

	var names []string
	contents := map[string]string{}
	for _, f := range rc.File {
		names = append(names, f.Name)
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(r)
		r.Close()
		contents[f.Name] = string(data)
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "report.json" || names[1] != "report.txt" {
		t.Errorf("entries = %v, want [report.json report.txt]", names)
	}
	if contents["report.txt"] != "hello text" {
		t.Errorf("report.txt content = %q", contents["report.txt"])
	}
	if contents["report.json"] != `{"ok":true}` {
		t.Errorf("report.json content = %q", contents["report.json"])
	}
}

func TestCreateZip_SkipsMissingFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "report.txt")
	if err := os.WriteFile(a, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	zipPath, err := CreateZip(dir, []string{a, filepath.Join(dir, "missing.log")})
	if err != nil {
		t.Fatalf("CreateZip should tolerate a missing file: %v", err)
	}
	rc, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatalf("zip is not readable: %v", err)
	}
	defer rc.Close()
	if len(rc.File) != 1 {
		t.Errorf("expected 1 entry, got %d", len(rc.File))
	}
}

func TestCreateZip_BadOutDir(t *testing.T) {
	_, err := CreateZip(filepath.Join(t.TempDir(), "does", "not", "exist"), nil)
	if err == nil {
		t.Error("expected error for nonexistent output directory")
	}
}

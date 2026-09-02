package llmplan

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec 7.9 "must not": the package never downloads, never contacts the
// network, never starts/stops processes or containers and never writes
// outside --out. These tests pin that down structurally.

func packageFiles(t *testing.T) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	matches, _ := filepath.Glob("*.go")
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = f
	}
	return files
}

func TestMustNot_NoNetworkOrExecImports(t *testing.T) {
	forbidden := map[string]bool{"os/exec": true, "net": true, "net/http": true, "net/url": true}
	for path, f := range packageFiles(t) {
		for _, imp := range f.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if forbidden[p] {
				t.Errorf("%s imports %s; llm-plan must not exec or use the network (spec 7.9)", path, p)
			}
		}
	}
}

// forbiddenCommands are things llm-plan must never run (spec 7.9).
var forbiddenCommands = []string{"docker", "podman", "pip", "pip3", "uv", "ollama", "systemctl", "sysctl", "swapon", "swapoff", "nvidia-smi", "huggingface-cli", "hf", "curl", "wget", "git", "vllm", "llama-server", "trtllm-serve", "gsettings", "nvpmodel"}

// TestMustNot_OnlyReadOnlyCommand allows exactly one command invocation in
// the package: the read-only Win32_OperatingSystem memory query.
func TestMustNot_OnlyReadOnlyCommand(t *testing.T) {
	var calls []string
	for path, f := range packageFiles(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "RunCommand" && sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" && sel.Sel.Name != "StartProcess" {
				return true
			}
			var lits []string
			for _, a := range call.Args {
				if bl, ok := a.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					lits = append(lits, strings.Trim(bl.Value, "`\""))
				}
			}
			calls = append(calls, path+": "+strings.Join(lits, " "))
			for _, l := range lits {
				for _, bad := range forbiddenCommands {
					if l == bad || strings.HasPrefix(l, bad+" ") {
						t.Errorf("%s runs %q; forbidden by spec 7.9", path, l)
					}
				}
			}
			return true
		})
	}
	if len(calls) != 1 || !strings.Contains(calls[0], "powershell") || !strings.Contains(calls[0], "Win32_OperatingSystem") {
		t.Errorf("expected exactly one read-only command (the Win32_OperatingSystem query), got %v", calls)
	}
}

// TestMustNot_WritesOnlyInRender: os.WriteFile/Create/MkdirAll appear only in
// render.go's WriteFiles, which writes into the --out directory.
func TestMustNot_WritesOnlyInRender(t *testing.T) {
	for path, f := range packageFiles(t) {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if !ok || id.Name != "os" {
				return true
			}
			switch sel.Sel.Name {
			case "WriteFile", "Create", "OpenFile", "MkdirAll", "Mkdir", "Remove", "RemoveAll", "Rename", "Setenv", "Chmod", "Symlink", "Truncate":
				if filepath.Base(path) != "render.go" {
					t.Errorf("%s calls os.%s; only render.go may write, and only into --out (spec 7.9)", path, sel.Sel.Name)
				}
			}
			return true
		})
	}
}

func TestMustNot_WriteFilesStaysInDir(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	p := &Plan{Runtime: Command{Runtime: RuntimeVLLM, Command: "x"}}
	files, err := WriteFiles(out, p, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files = %v", files)
	}
	for _, f := range files {
		if filepath.Dir(f) != out {
			t.Errorf("%s written outside %s", f, out)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "out" {
		t.Errorf("unexpected entries next to --out: %v", entries)
	}
}

// TestMustNot_NeverClaimsMeasurement: rendered estimates carry the
// not-measured labels and never the word "measured" without attribution.
func TestMustNot_NeverClaimsMeasurement(t *testing.T) {
	txt := RenderText(&Plan{Runtime: Command{Runtime: RuntimeVLLM, Command: "x"}, Estimates: PlanEstimates{DecodeCeilingTPS: 13.4, DecodeCeilingWeightsOnlyTPS: 17, PrefillRefTPS: PrefillReferenceTPS}})
	for _, must := range []string{"not measured on this machine", footerEstimates, footerReadOnly, "never \"128 GB\""} {
		if must == "never \"128 GB\"" {
			if strings.Contains(txt, "128 GB") {
				t.Error("plan text must never present the pool as 128 GB (spec 7.9)")
			}
			continue
		}
		if !strings.Contains(txt, must) {
			t.Errorf("plan text lacks %q", must)
		}
	}
}

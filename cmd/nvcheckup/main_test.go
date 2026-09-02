package main

import (
	"errors"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// journalAfterFailedUndo simulates the sequence the engine produces: a fix is
// applied, an undo attempt fails (Engine.Undo stamps UndoneAt on every
// attempt), and the user retries.
func journalAfterFailedUndo(id string) []types.ChangeJournalEntry {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return []types.ChangeJournalEntry{
		{ActionID: id, Title: "first apply, undone fine", AppliedAt: t0, Success: true,
			UndoneAt: t0.Add(time.Minute), UndoSuccess: true},
		{ActionID: id, Title: "second apply, undo failed", AppliedAt: t0.Add(2 * time.Minute), Success: true,
			UndoneAt: t0.Add(3 * time.Minute), UndoSuccess: false, UndoOutput: "Access is denied."},
	}
}

func TestNewestUndoable_RetriesFailedUndo(t *testing.T) {
	entries := journalAfterFailedUndo("set-high-performance")

	got := newestUndoable(entries, "set-high-performance")
	if got == nil {
		t.Fatal("entry whose undo FAILED must remain undoable")
	}
	if got.Title != "second apply, undo failed" {
		t.Errorf("selected %q, want the newest entry with a failed undo", got.Title)
	}

	// Once the retry succeeds, nothing is left to undo.
	entries[1].UndoSuccess = true
	if got := newestUndoable(entries, "set-high-performance"); got != nil {
		t.Errorf("successfully undone entry must not be selected again, got %q", got.Title)
	}
}

func TestNewestUndoable_PrefersNewestAndSkipsFailedApply(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	entries := []types.ChangeJournalEntry{
		{ActionID: "a", Title: "old", AppliedAt: t0, Success: true},
		{ActionID: "a", Title: "new", AppliedAt: t0.Add(time.Hour), Success: true},
		{ActionID: "a", Title: "failed apply", AppliedAt: t0.Add(2 * time.Hour), Success: false},
		{ActionID: "b", Title: "other action", AppliedAt: t0.Add(3 * time.Hour), Success: true},
	}
	got := newestUndoable(entries, "a")
	if got == nil || got.Title != "new" {
		t.Fatalf("want newest successful entry for a, got %+v", got)
	}
	if newestUndoable(entries, "zzz") != nil {
		t.Error("unknown id must return nil")
	}
}

func TestIsElevationError_NarrowMatch(t *testing.T) {
	yes := []string{
		"this action requires elevated privileges (Administrator/root); nothing was changed",
		"ERROR: Access is denied.",
		"access denied",
		"open /etc/modprobe.d/x.conf: permission denied",
		"This operation requires Administrator rights",
		"please run as root",
	}
	for _, m := range yes {
		if !isElevationError(m) {
			t.Errorf("%q should be an elevation error", m)
		}
	}
	no := []string{
		"cannot create journal directory /root/.config/nvcheckup: read-only file system",
		"user Administrator has no journal at C:\\Users\\Administrator\\AppData\\Roaming\\nvcheckup",
		"reg.exe exited with status 1",
		"",
	}
	for _, m := range no {
		if isElevationError(m) {
			t.Errorf("%q must not be treated as an elevation error", m)
		}
	}
}

func TestElevationHint_PerPlatform(t *testing.T) {
	if got := elevationHint("windows", "fix --id x"); !strings.Contains(got, "Administrator") || strings.Contains(got, "sudo") {
		t.Errorf("windows hint = %q", got)
	}
	if got := elevationHint("linux", "fix --id set-high-performance"); got != "Hint: Re-run with sudo (e.g. sudo nvcheckup fix --id set-high-performance)." {
		t.Errorf("linux hint = %q", got)
	}
	if got := elevationHint("darwin", "undo --id x"); !strings.Contains(got, "sudo nvcheckup undo --id x") {
		t.Errorf("darwin hint = %q", got)
	}
}

func TestSudoUserJournalDir(t *testing.T) {
	lookup := func(name string) (*user.User, error) {
		if name == "alice" {
			return &user.User{Username: "alice", HomeDir: "/home/alice"}, nil
		}
		return nil, errors.New("unknown user")
	}
	want := filepath.Join("/home/alice", ".config", "nvcheckup")

	if dir, ok := sudoUserJournalDir("linux", 0, "alice", lookup); !ok || dir != want {
		t.Errorf("sudo on linux: got (%q, %v), want (%q, true)", dir, ok, want)
	}
	if dir, ok := sudoUserJournalDir("darwin", 0, "alice", lookup); !ok || dir != filepath.Join("/home/alice", "Library", "Application Support", "nvcheckup") {
		t.Errorf("sudo on darwin: got (%q, %v)", dir, ok)
	}
	cases := []struct {
		name string
		goos string
		euid int
		sudo string
	}{
		{"windows is unchanged", "windows", 0, "alice"},
		{"not root", "linux", 1000, "alice"},
		{"plain root without SUDO_USER", "linux", 0, ""},
		{"sudo from root itself", "linux", 0, "root"},
		{"unknown sudo user", "linux", 0, "ghost"},
	}
	for _, c := range cases {
		if dir, ok := sudoUserJournalDir(c.goos, c.euid, c.sudo, lookup); ok {
			t.Errorf("%s: expected fallback to UserConfigDir, got %q", c.name, dir)
		}
	}
}

func TestCompareWriteDir(t *testing.T) {
	if got := compareWriteDir(".", false, false); got != "" {
		t.Errorf("no --md and no --out must be console only, got %q", got)
	}
	if got := compareWriteDir(".", true, false); got != "." {
		t.Errorf("--md alone writes into the current directory, got %q", got)
	}
	if got := compareWriteDir("out", false, true); got != "out" {
		t.Errorf("explicit --out writes comparison.txt, got %q", got)
	}
	if got := compareWriteDir("", true, true); got != "." {
		t.Errorf("empty --out with --md falls back to current directory, got %q", got)
	}
}

func TestNetworkVerdictLines(t *testing.T) {
	join := func(n *types.NetworkInfo) string { return strings.Join(networkVerdictLines(n), "\n") }

	healthy := join(&types.NetworkInfo{LatencyMs: 12, JitterMs: 1, DNSTimeMs: 20})
	if !strings.Contains(healthy, "Network appears healthy.") {
		t.Errorf("real ping samples should be healthy, got %q", healthy)
	}

	hopsOnly := join(&types.NetworkInfo{JitterMs: 40, Hops: []types.HopInfo{{Number: 1, LatencyMs: 2}}})
	if strings.Contains(hopsOnly, "healthy") || strings.Contains(hopsOnly, "High jitter") {
		t.Errorf("hops without ping must not produce a latency verdict, got %q", hopsOnly)
	}
	if !strings.Contains(hopsOnly, "Ping produced no samples; latency, jitter and loss could not be measured.") {
		t.Errorf("hops without ping should explain the missing samples, got %q", hopsOnly)
	}

	nothing := join(&types.NetworkInfo{})
	if nothing != "" {
		t.Errorf("no probe data should print no verdict, got %q", nothing)
	}

	lossy := join(&types.NetworkInfo{PacketLossPct: 100})
	if !strings.Contains(lossy, "CRITICAL: High packet loss") || strings.Contains(lossy, "healthy") {
		t.Errorf("total loss is ping evidence, got %q", lossy)
	}
}

package remediate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestJournal_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	entry := types.ChangeJournalEntry{
		ActionID:  "test-action",
		Title:     "Test Action",
		AppliedAt: time.Now(),
		Success:   true,
		Output:    "test output",
		UndoInfo:  "undo info",
	}

	err := j.Append(entry)
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	entries, err := j.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	if entries[0].ActionID != "test-action" {
		t.Errorf("Expected action ID 'test-action', got '%s'", entries[0].ActionID)
	}
	if entries[0].Title != "Test Action" {
		t.Errorf("Expected title 'Test Action', got '%s'", entries[0].Title)
	}
}

func TestJournal_MultipleAppends(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	for i := 0; i < 3; i++ {
		entry := types.ChangeJournalEntry{
			ActionID:  "action-" + string(rune('a'+i)),
			Title:     "Action",
			AppliedAt: time.Now(),
			Success:   true,
		}
		if err := j.Append(entry); err != nil {
			t.Fatalf("Append %d failed: %v", i, err)
		}
	}

	entries, err := j.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if len(entries) != 3 {
		t.Fatalf("Expected 3 entries, got %d", len(entries))
	}
}

func TestJournal_ReadEmpty(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	entries, err := j.Read()
	if err != nil {
		t.Fatalf("Read empty should not error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func TestJournal_Path(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)

	expected := filepath.Join(dir, journalFilename)
	if j.Path() != expected {
		t.Errorf("Expected path %s, got %s", expected, j.Path())
	}
}

func TestJournal_WritePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	j := NewJournal(dir)
	if err := j.Append(types.ChangeJournalEntry{ActionID: "a", AppliedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(j.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("journal permissions = %o, want 0600", perm)
	}
}

func TestJournal_Read_RejectsMissingActionID(t *testing.T) {
	dir := t.TempDir()
	raw := `[{"action_id":"","title":"x","applied_at":"2026-09-01T10:00:00Z","success":true}]`
	if err := os.WriteFile(filepath.Join(dir, journalFilename), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := NewJournal(dir).Read()
	if err == nil || !strings.Contains(err.Error(), "action_id") {
		t.Fatalf("expected missing action_id error, got %v", err)
	}
}

func TestJournal_Read_RejectsBadAppliedAt(t *testing.T) {
	dir := t.TempDir()
	for _, raw := range []string{
		`[{"action_id":"a","applied_at":"yesterday","success":true}]`,
		`[{"action_id":"a","success":true}]`,
		`[{"action_id":"a","applied_at":"0001-01-01T00:00:00Z","success":true}]`,
	} {
		if err := os.WriteFile(filepath.Join(dir, journalFilename), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewJournal(dir).Read(); err == nil {
			t.Errorf("Read should reject %s", raw)
		}
	}
}

func TestJournal_LatestUndoable(t *testing.T) {
	dir := t.TempDir()
	j := NewJournal(dir)
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }

	// Deliberately out of chronological order so the test proves selection is
	// by AppliedAt rather than by file position.
	entries := []types.ChangeJournalEntry{
		{ActionID: "disable-hags", AppliedAt: at(3), Success: true, UndoInfo: "2", UndoneAt: at(4), UndoSuccess: true}, // already undone
		{ActionID: "disable-hags", AppliedAt: at(1), Success: true, UndoInfo: "2"},
		{ActionID: "disable-hags", AppliedAt: at(5), Success: false},                          // failed apply
		{ActionID: "disable-game-mode", AppliedAt: at(6), Success: true, UndoInfo: "1"},       // other action
		{ActionID: "disable-hags", AppliedAt: at(2), Success: true, UndoInfo: absentSentinel}, // newest undoable
	}
	if err := j.Write(entries); err != nil {
		t.Fatal(err)
	}

	got, ok := j.LatestUndoable("disable-hags")
	if !ok {
		t.Fatal("expected an undoable entry")
	}
	if !got.AppliedAt.Equal(at(2)) || got.UndoInfo != absentSentinel {
		t.Errorf("LatestUndoable picked %+v, want the at(2) entry", got)
	}

	// An entry whose undo attempt failed is still undoable (retry).
	entries = append(entries, types.ChangeJournalEntry{ActionID: "disable-hags", AppliedAt: at(7), Success: true, UndoInfo: "2", UndoneAt: at(8), UndoSuccess: false, UndoOutput: "boom"})
	if err := j.Write(entries); err != nil {
		t.Fatal(err)
	}
	got, ok = j.LatestUndoable("disable-hags")
	if !ok || !got.AppliedAt.Equal(at(7)) {
		t.Errorf("failed-undo entry should be retryable, got %+v ok=%v", got, ok)
	}

	if _, ok := j.LatestUndoable("set-high-performance"); ok {
		t.Error("no entry expected for an action never applied")
	}
	if _, ok := NewJournal(t.TempDir()).LatestUndoable("disable-hags"); ok {
		t.Error("no entry expected for a missing journal")
	}
}

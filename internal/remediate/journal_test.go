package remediate

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
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

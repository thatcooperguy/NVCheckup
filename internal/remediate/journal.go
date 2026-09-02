package remediate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// journalFilename is the name of the change journal file stored in the journal directory.
const journalFilename = "nvcheckup-changes.json"

// Journal manages the change log for remediation actions. Each applied action
// is recorded as a ChangeJournalEntry, enabling auditing and undo operations.
// The journal is stored as a JSON array in a single file.
//
// The file is written with 0600 permissions: it records previous registry
// values and file contents, and (more importantly) it is read back by Undo,
// so nobody else on the machine should be able to plant entries in it.
type Journal struct {
	path string
}

// NewJournal creates a new Journal that stores entries in the given directory.
// The directory is created on first write.
func NewJournal(dir string) *Journal {
	return &Journal{
		path: filepath.Join(dir, journalFilename),
	}
}

// Append adds a new entry to the journal. If the journal file does not exist,
// it is created. Existing entries are preserved.
func (j *Journal) Append(entry types.ChangeJournalEntry) error {
	entries, err := j.Read()
	if err != nil {
		return fmt.Errorf("failed to read existing journal: %w", err)
	}

	entries = append(entries, entry)
	return j.Write(entries)
}

// Read returns all journal entries from the journal file. If the file does not
// exist, an empty slice is returned with no error (a missing journal is not an
// error condition -- it simply means no actions have been applied yet).
//
// Every entry is validated: it must carry an action id and a parseable,
// non-zero AppliedAt timestamp. A journal that fails validation is rejected as
// a whole because Undo keys entries by (ActionID, AppliedAt).
func (j *Journal) Read() ([]types.ChangeJournalEntry, error) {
	data, err := os.ReadFile(j.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []types.ChangeJournalEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read journal file %s: %w", j.path, err)
	}

	// Handle empty file gracefully
	if len(data) == 0 {
		return []types.ChangeJournalEntry{}, nil
	}

	var entries []types.ChangeJournalEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		// json.Unmarshal surfaces an unparseable applied_at as a time.Time error.
		return nil, fmt.Errorf("failed to parse journal file %s: %w", j.path, err)
	}

	for i, e := range entries {
		if err := validateJournalEntry(e); err != nil {
			return nil, fmt.Errorf("journal file %s: entry %d is invalid: %w", j.path, i+1, err)
		}
	}

	if entries == nil {
		entries = []types.ChangeJournalEntry{}
	}
	return entries, nil
}

// validateJournalEntry checks the fields Undo relies on to identify an entry.
func validateJournalEntry(e types.ChangeJournalEntry) error {
	if e.ActionID == "" {
		return fmt.Errorf("missing action_id")
	}
	if e.AppliedAt.IsZero() {
		return fmt.Errorf("missing or unparseable applied_at")
	}
	return nil
}

// Write replaces the entire journal file with the given entries. This is used
// by Append (to add entries) and by the Engine's Undo method (to update entries
// with undo status). The parent directory is created if it does not exist.
func (j *Journal) Write(entries []types.ChangeJournalEntry) error {
	// Ensure the parent directory exists. 0700 so the journal stays private
	// even if the directory is freshly created under a shared location.
	dir := filepath.Dir(j.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create journal directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal journal entries: %w", err)
	}

	if err := os.WriteFile(j.path, data, 0600); err != nil {
		return fmt.Errorf("failed to write journal file %s: %w", j.path, err)
	}
	// os.WriteFile only applies the mode when creating the file; tighten an
	// existing file that may have been created by an older version with 0644.
	if err := os.Chmod(j.path, 0600); err != nil {
		return fmt.Errorf("failed to set journal permissions on %s: %w", j.path, err)
	}

	return nil
}

// LatestUndoable returns the newest journal entry for actionID that was
// applied successfully and has not been successfully undone. An entry whose
// undo attempt failed is still returned, so the user can retry; all undo
// operations are idempotent. ok is false when no such entry exists or the
// journal cannot be read.
func (j *Journal) LatestUndoable(actionID string) (*types.ChangeJournalEntry, bool) {
	entries, err := j.Read()
	if err != nil {
		return nil, false
	}

	var best *types.ChangeJournalEntry
	for i := range entries {
		e := &entries[i]
		if e.ActionID != actionID || !e.Success {
			continue
		}
		if !e.UndoneAt.IsZero() && e.UndoSuccess {
			continue
		}
		if best == nil || e.AppliedAt.After(best.AppliedAt) {
			best = e
		}
	}
	if best == nil {
		return nil, false
	}
	// Return a copy so callers cannot mutate the slice we just read.
	out := *best
	return &out, true
}

// Path returns the absolute path to the journal file.
func (j *Journal) Path() string {
	return j.path
}

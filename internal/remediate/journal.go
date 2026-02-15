package remediate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// journalFilename is the name of the change journal file stored in the journal directory.
const journalFilename = "nvcheckup-changes.json"

// Journal manages the change log for remediation actions. Each applied action
// is recorded as a ChangeJournalEntry, enabling auditing and undo operations.
// The journal is stored as a JSON array in a single file.
type Journal struct {
	path string
}

// NewJournal creates a new Journal that stores entries in the given directory.
// The directory is created if it does not exist.
func NewJournal(dir string) *Journal {
	return &Journal{
		path: filepath.Join(dir, journalFilename),
	}
}

// Append adds a new entry to the journal. If the journal file does not exist,
// it is created. Existing entries are preserved.
func (j *Journal) Append(entry types.ChangeJournalEntry) error {
	entries, err := j.Read()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read existing journal: %w", err)
	}

	entries = append(entries, entry)
	return j.Write(entries)
}

// Read returns all journal entries from the journal file. If the file does not
// exist, an empty slice is returned with no error (a missing journal is not an
// error condition -- it simply means no actions have been applied yet).
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
		return nil, fmt.Errorf("failed to parse journal file %s: %w", j.path, err)
	}

	return entries, nil
}

// Write replaces the entire journal file with the given entries. This is used
// by Append (to add entries) and by the Engine's Undo method (to update entries
// with undo status). The parent directory is created if it does not exist.
func (j *Journal) Write(entries []types.ChangeJournalEntry) error {
	// Ensure the parent directory exists
	dir := filepath.Dir(j.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create journal directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal journal entries: %w", err)
	}

	if err := os.WriteFile(j.path, data, 0644); err != nil {
		return fmt.Errorf("failed to write journal file %s: %w", j.path, err)
	}

	return nil
}

// Path returns the absolute path to the journal file.
func (j *Journal) Path() string {
	return j.path
}

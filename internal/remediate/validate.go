package remediate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// absentSentinel is recorded as UndoInfo when the value or file an action
// creates did not exist beforehand. Undo then removes the value/file instead
// of writing a guessed default the machine never had.
const absentSentinel = "__ABSENT__"

// maxUndoInfoBytes caps the size of undo data that Undo is willing to act on.
// Real values are a GUID, a DWORD, or a three-line modprobe file; anything
// larger indicates a corrupted or tampered journal.
const maxUndoInfoBytes = 4096

var (
	// guidPattern matches a Windows power-scheme GUID (8-4-4-4-12 hex digits).
	guidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	// dwordPattern matches a decimal or 0x-prefixed hexadecimal DWORD literal.
	// Range is checked separately because the regexp cannot express 2^32-1.
	dwordPattern = regexp.MustCompile(`^(0[xX][0-9a-fA-F]{1,8}|[0-9]{1,10})$`)

	// nouveauLinePattern matches the only lines nvcheckup is willing to write
	// to /etc/modprobe.d: comments and the two nouveau directives.
	nouveauLinePattern = regexp.MustCompile(`^(#.*|blacklist nouveau|options nouveau modeset=0)$`)
)

// validateUndoInfo checks that undo data read from the journal has the shape
// the named action expects. It is the gate between untrusted journal contents
// and privileged writes (registry, /etc), so every action has an explicit
// allow-list and unknown actions are rejected.
func validateUndoInfo(actionID, undoInfo string) error {
	if len(undoInfo) > maxUndoInfoBytes {
		return fmt.Errorf("undo information is too large (%d bytes)", len(undoInfo))
	}

	switch actionID {
	case "set-high-performance":
		if !guidPattern.MatchString(undoInfo) {
			return fmt.Errorf("undo information %q is not a power scheme GUID", undoInfo)
		}
		return nil

	case "disable-hags", "disable-game-mode":
		if undoInfo == absentSentinel {
			return nil
		}
		if !isDword(undoInfo) {
			return fmt.Errorf("undo information %q is not a DWORD value or %s", undoInfo, absentSentinel)
		}
		return nil

	case "blacklist-nouveau":
		if undoInfo == absentSentinel {
			return nil
		}
		if err := validateNouveauContent(undoInfo); err != nil {
			return fmt.Errorf("undo information is not a safe blacklist-nouveau file: %w", err)
		}
		return nil

	case "update-ldconfig":
		if undoInfo != "" {
			return fmt.Errorf("undo information must be empty for update-ldconfig, got %q", undoInfo)
		}
		return nil

	case "":
		return fmt.Errorf("missing action id")

	default:
		return fmt.Errorf("unknown remediation action %q", actionID)
	}
}

// isDword reports whether s is a decimal or hex literal that fits in 32 bits.
func isDword(s string) bool {
	if !dwordPattern.MatchString(s) {
		return false
	}
	var (
		v   uint64
		err error
	)
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		v, err = strconv.ParseUint(s[2:], 16, 64)
	} else {
		v, err = strconv.ParseUint(s, 10, 64)
	}
	return err == nil && v <= 0xFFFFFFFF
}

// validateNouveauContent accepts file content only if every line is a comment,
// "blacklist nouveau", or "options nouveau modeset=0". A single trailing
// newline is allowed; blank lines and anything else are rejected so that undo
// can never write arbitrary directives into /etc/modprobe.d.
func validateNouveauContent(content string) error {
	if content == "" {
		return fmt.Errorf("content is empty")
	}
	if strings.Contains(content, "\r") {
		return fmt.Errorf("content contains carriage returns")
	}
	body := strings.TrimSuffix(content, "\n")
	for i, line := range strings.Split(body, "\n") {
		if !nouveauLinePattern.MatchString(line) {
			return fmt.Errorf("line %d %q is not an allowed modprobe directive", i+1, line)
		}
	}
	return nil
}

package remediate

import (
	"strings"
	"testing"
)

func TestValidateUndoInfo_Accepts(t *testing.T) {
	ok := []struct{ id, info string }{
		{"set-high-performance", "381b4222-f694-41f0-9685-ff5bb260df2e"},
		{"set-high-performance", "8C5E7FDA-E8BF-4A96-9A85-A6E23A8C635C"},
		{"disable-hags", absentSentinel},
		{"disable-hags", "2"},
		{"disable-hags", "0x2"},
		{"disable-hags", "4294967295"},
		{"disable-hags", absentKeySentinel},
		{"disable-game-mode", absentKeySentinel},
		{"disable-game-mode", absentSentinel},
		{"disable-game-mode", "1"},
		{"blacklist-nouveau", absentSentinel},
		{"blacklist-nouveau", nouveauBlacklistPath}, // legacy v0.2.0 marker for "we created the file"
		{"blacklist-nouveau", nouveauBlacklistContent},
		{"blacklist-nouveau", "blacklist nouveau"},
		{"blacklist-nouveau", "# just a comment\nblacklist nouveau\noptions nouveau modeset=0"},
		{"update-ldconfig", ""},
	}
	for _, c := range ok {
		if err := validateUndoInfo(c.id, c.info); err != nil {
			t.Errorf("validateUndoInfo(%q, %q) = %v, want nil", c.id, c.info, err)
		}
	}
}

func TestValidateUndoInfo_Rejects(t *testing.T) {
	bad := []struct{ id, info, wantSubstr string }{
		{"set-high-performance", "", "GUID"},
		{"set-high-performance", "381b4222-f694-41f0-9685", "GUID"},
		{"set-high-performance", "381b4222-f694-41f0-9685-ff5bb260df2e /h", "GUID"},
		{"set-high-performance", absentSentinel, "GUID"},
		{"set-high-performance", absentKeySentinel, "GUID"},
		{"disable-hags", "", "DWORD"},
		{"disable-hags", "2 /f", "DWORD"},
		{"disable-hags", "4294967296", "DWORD"},
		{"disable-hags", "0x100000000", "DWORD"},
		{"disable-hags", "-1", "DWORD"},
		{"disable-hags", "two", "DWORD"},
		{"disable-game-mode", "1\n", "DWORD"},
		{"blacklist-nouveau", "", "empty"},
		{"blacklist-nouveau", "install nouveau /bin/true\n", "not an allowed"},
		{"blacklist-nouveau", "blacklist nouveau\n\nblacklist i915\n", "not an allowed"},
		{"blacklist-nouveau", "blacklist nouveau\r\n", "carriage"},
		{"blacklist-nouveau", "blacklist nouveau; rm -rf /\n", "not an allowed"},
		{"blacklist-nouveau", "options nouveau modeset=1\n", "not an allowed"},
		{"blacklist-nouveau", "/etc/modprobe.d/other.conf", "not an allowed"},
		{"blacklist-nouveau", nouveauBlacklistPath + "\n", "not an allowed"},
		{"blacklist-nouveau", " " + nouveauBlacklistPath, "not an allowed"},
		{"update-ldconfig", absentSentinel, "must be empty"},
		{"update-ldconfig", "anything", "must be empty"},
		{"", "2", "missing action id"},
		{"rm-rf", "x", "unknown"},
		{"disable-hags", strings.Repeat("1", maxUndoInfoBytes+1), "too large"},
	}
	for _, c := range bad {
		err := validateUndoInfo(c.id, c.info)
		if err == nil {
			t.Errorf("validateUndoInfo(%q, %q) should fail", c.id, c.info)
			continue
		}
		if !strings.Contains(err.Error(), c.wantSubstr) {
			t.Errorf("validateUndoInfo(%q, %q) error %q should mention %q", c.id, c.info, err, c.wantSubstr)
		}
	}
}

func TestIsDword(t *testing.T) {
	for _, s := range []string{"0", "1", "0x0", "0xFFFFFFFF", "4294967295"} {
		if !isDword(s) {
			t.Errorf("isDword(%q) should be true", s)
		}
	}
	for _, s := range []string{"", "x", "0x", "0xFFFFFFFFF", "4294967296", "1.0", " 1"} {
		if isDword(s) {
			t.Errorf("isDword(%q) should be false", s)
		}
	}
}

// Only the exact legacy marker is translated; near misses stay untouched so
// validation rejects them.
func TestNormalizeNouveauUndoInfo(t *testing.T) {
	if got := normalizeNouveauUndoInfo(nouveauBlacklistPath); got != absentSentinel {
		t.Errorf("legacy path marker should map to %s, got %q", absentSentinel, got)
	}
	for _, s := range []string{absentSentinel, nouveauBlacklistContent, "/etc/modprobe.d/other.conf", nouveauBlacklistPath + "\n", ""} {
		if got := normalizeNouveauUndoInfo(s); got != s {
			t.Errorf("normalizeNouveauUndoInfo(%q) = %q, want unchanged", s, got)
		}
	}
}

package wsl

import (
	"testing"
)

func TestOSReleaseName(t *testing.T) {
	content := "PRETTY_NAME=\"Ubuntu 24.04.3 LTS\"\nNAME=\"Ubuntu\"\nVERSION_ID=\"24.04\"\nVERSION_CODENAME=noble\n"
	if got := osReleaseName(content); got != "Ubuntu" {
		t.Errorf("osReleaseName = %q", got)
	}
	if got := osReleaseName("VERSION_ID=\"24.04\"\n"); got != "" {
		t.Errorf("missing NAME must give empty, got %q", got)
	}
	if got := osReleaseName("NAME=Debian GNU/Linux\n"); got != "Debian GNU/Linux" {
		t.Errorf("unquoted NAME = %q", got)
	}
}

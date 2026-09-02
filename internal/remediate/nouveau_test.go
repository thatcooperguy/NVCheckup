package remediate

import (
	"errors"
	"testing"
)

// The content we write must itself pass the undo validator, otherwise a file
// we created could never be restored from the journal.
func TestNouveauBlacklistContent_IsRestorable(t *testing.T) {
	if err := validateNouveauContent(nouveauBlacklistContent); err != nil {
		t.Fatalf("nouveauBlacklistContent fails validation: %v", err)
	}
	if err := validateUndoInfo("blacklist-nouveau", nouveauBlacklistContent); err != nil {
		t.Fatalf("validateUndoInfo rejects our own content: %v", err)
	}
}

func TestDetectInitramfsTool(t *testing.T) {
	only := func(name string) func(string) (string, error) {
		return func(n string) (string, error) {
			if n == name {
				return "/usr/sbin/" + n, nil
			}
			return "", errors.New("not found")
		}
	}
	cases := []struct {
		installed string
		wantName  string
		wantArgs  string
	}{
		{"update-initramfs", "update-initramfs", "-u"},
		{"dracut", "dracut", "-f"},
		{"mkinitcpio", "mkinitcpio", "-P"},
	}
	for _, c := range cases {
		tool, ok := detectInitramfsTool(only(c.installed))
		if !ok || tool.Name != c.wantName || len(tool.Args) != 1 || tool.Args[0] != c.wantArgs {
			t.Errorf("detectInitramfsTool(%s) = %+v ok=%v", c.installed, tool, ok)
		}
	}
	if _, ok := detectInitramfsTool(only("nothing")); ok {
		t.Error("should report no tool when none is installed")
	}
	// Preference order: Debian's tool wins when several are present (e.g. a
	// system that has both update-initramfs and dracut installed).
	all := func(string) (string, error) { return "/x", nil }
	if tool, _ := detectInitramfsTool(all); tool.Name != "update-initramfs" {
		t.Errorf("expected update-initramfs to be preferred, got %s", tool.Name)
	}
}

func TestPackageListHasNvidiaDriver(t *testing.T) {
	dpkgInstalled := `Desired=Unknown/Install/Remove/Purge/Hold
| Status=Not/Inst/Conf-files/Unpacked/halF-conf/Half-inst/trig-aWait/Trig-pend
||/ Name                           Version                     Architecture Description
+++-==============================-===========================-============-=================================
ii  libnvidia-compute-550:amd64    550.54.14-0ubuntu0.22.04.1  amd64        NVIDIA libcompute package
ii  nvidia-driver-550              550.54.14-0ubuntu0.22.04.1  amd64        NVIDIA driver metapackage
ii  nvidia-settings                510.47.03-0ubuntu1          amd64        Tool for configuring the NVIDIA graphics driver
`
	dpkgRemoved := `rc  nvidia-driver-535              535.104.05-0ubuntu0.22.04.1 amd64        NVIDIA driver metapackage
ii  nvidia-settings                510.47.03-0ubuntu1          amd64        Tool for configuring the NVIDIA graphics driver
ii  libnvidia-egl-wayland1:amd64   1:1.1.9-1.1                 amd64        Wayland EGL External Platform library
`
	rpmInstalled := "kernel-6.8.5-301.fc40.x86_64\nakmod-nvidia-550.76-1.fc40.x86_64\nxorg-x11-drv-nvidia-cuda-550.76-1.fc40.x86_64\n"
	rpmUtilsOnly := "kernel-6.8.5-301.fc40.x86_64\nnvidia-settings-550.76-1.fc40.x86_64\nlibnvidia-container1-1.15.0-1.x86_64\n"
	pacmanInstalled := "linux 6.8.9.arch1-1\nnvidia 550.78-6\nnvidia-utils 550.78-1\n"
	pacmanOpen := "linux 6.8.9.arch1-1\nnvidia-open-dkms 550.78-1\n"
	pacmanUtilsOnly := "linux 6.8.9.arch1-1\nnvidia-utils 550.78-1\nbc 1.07.1-4\n"

	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"dpkg installed", dpkgInstalled, true},
		{"dpkg removed (rc) only", dpkgRemoved, false},
		{"rpm akmod", rpmInstalled, true},
		{"rpm utils only", rpmUtilsOnly, false},
		{"pacman nvidia", pacmanInstalled, true},
		{"pacman nvidia-open-dkms", pacmanOpen, true},
		{"pacman utils only", pacmanUtilsOnly, false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := packageListHasNvidiaDriver(c.in); got != c.want {
			t.Errorf("%s: packageListHasNvidiaDriver = %v, want %v", c.name, got, c.want)
		}
	}
}

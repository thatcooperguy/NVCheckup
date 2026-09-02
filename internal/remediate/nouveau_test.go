package remediate

import (
	"errors"
	"strings"
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
	// Library/common/source split packages that can be present without any
	// kernel module (Debian/Ubuntu and negativo17 naming).
	dpkgLibsOnly := `ii  nvidia-driver-libs:i386        550.54.14-1                 i386         NVIDIA metapackage (OpenGL/GLX/EGL/GLES libraries)
ii  nvidia-driver-libs:amd64       550.54.14-1                 amd64        NVIDIA metapackage (OpenGL/GLX/EGL/GLES libraries)
ii  nvidia-driver-bin              550.54.14-1                 amd64        NVIDIA driver support binaries
ii  nvidia-kernel-common           550.54.14-1                 amd64        NVIDIA binary kernel module support files
ii  nvidia-kernel-common-550       550.54.14-0ubuntu0.22.04.1  amd64        Shared files used with the kernel module
ii  nvidia-kernel-source-550       550.54.14-0ubuntu0.22.04.1  amd64        NVIDIA kernel source package
ii  nvidia-kernel-support          550.54.14-1                 amd64        NVIDIA binary kernel module support files
`
	dpkgDkms := "ii  nvidia-dkms-550:amd64          550.54.14-0ubuntu0.22.04.1  amd64        NVIDIA DKMS package\n"
	dpkgDebianKernelDkms := "ii  nvidia-kernel-dkms             550.54.14-1                 amd64        NVIDIA binary kernel module DKMS source\n"
	dpkgOpenMeta := "ii  nvidia-driver-550-open         550.54.14-0ubuntu0.22.04.1  amd64        NVIDIA driver (open kernel) metapackage\n"
	dpkgLinuxModules := "ii  linux-modules-nvidia-550-generic 6.8.0-40.40                amd64        Linux kernel nvidia modules\n"
	rpmNegativoLibsOnly := "nvidia-driver-libs-550.76-1.fc40.x86_64\nnvidia-driver-cuda-550.76-1.fc40.x86_64\nnvidia-kmod-common-550.76-1.fc40.noarch\nnvidia-settings-550.76-1.fc40.x86_64\n"
	rpmNegativoDkms := "nvidia-driver-libs-550.76-1.fc40.x86_64\ndkms-nvidia-550.76-1.fc40.x86_64\n"
	rpmFusionLibsOnly := "xorg-x11-drv-nvidia-libs-550.76-1.fc40.x86_64\nxorg-x11-drv-nvidia-cuda-550.76-1.fc40.x86_64\nxorg-x11-drv-nvidia-kmodsrc-550.76-1.fc40.x86_64\n"
	rpmFusionDriver := "xorg-x11-drv-nvidia-550.76-1.fc40.x86_64\n"
	rpmKmod := "kmod-nvidia-6.8.5-301.fc40.x86_64-550.76-1.fc40.x86_64\n"

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
		{"dpkg libs/common/source only", dpkgLibsOnly, false},
		{"dpkg nvidia-dkms", dpkgDkms, true},
		{"dpkg debian nvidia-kernel-dkms", dpkgDebianKernelDkms, true},
		{"dpkg open meta", dpkgOpenMeta, true},
		{"dpkg linux-modules-nvidia", dpkgLinuxModules, true},
		{"rpm negativo17 libs only", rpmNegativoLibsOnly, false},
		{"rpm negativo17 dkms-nvidia", rpmNegativoDkms, true},
		{"rpm fusion libs only", rpmFusionLibsOnly, false},
		{"rpm fusion driver", rpmFusionDriver, true},
		{"rpm kmod-nvidia", rpmKmod, true},
	}
	for _, c := range cases {
		if got := packageListHasNvidiaDriver(c.in); got != c.want {
			t.Errorf("%s: packageListHasNvidiaDriver = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestStripPackageVersion(t *testing.T) {
	cases := map[string]string{
		"nvidia-driver-550":                 "nvidia-driver",
		"nvidia-driver-550-open":            "nvidia-driver",
		"akmod-nvidia-550.76-1.fc40.x86_64": "akmod-nvidia",
		"kmod-nvidia-6.8.5-301.fc40.x86_64": "kmod-nvidia",
		"xorg-x11-drv-nvidia":               "xorg-x11-drv-nvidia",
		"nvidia":                            "nvidia",
		"nvidia-legacy-390xx-driver":        "nvidia-legacy",
		"linux-modules-nvidia-550-generic":  "linux-modules-nvidia",
		"nvidia-kernel-source-550":          "nvidia-kernel-source",
	}
	for in, want := range cases {
		if got := stripPackageVersion(in); got != want {
			t.Errorf("stripPackageVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsNvidiaDriverPackage(t *testing.T) {
	yes := []string{
		"nvidia", "nvidia-open", "nvidia-open-dkms", "nvidia-lts", "nvidia-dkms",
		"nvidia-dkms-550", "nvidia-driver", "nvidia-driver-550", "nvidia-driver-550-server",
		"nvidia-kernel-dkms", "nvidia-kernel-open-dkms", "nvidia-legacy-390xx-driver",
		"akmod-nvidia-550.76-1.fc40.x86_64", "kmod-nvidia-550.76-1.fc40.x86_64",
		"dkms-nvidia-550.76-1.fc40.x86_64", "xorg-x11-drv-nvidia-550.76-1.fc40.x86_64",
		"linux-modules-nvidia-550-generic",
	}
	no := []string{
		"", "nvidia-utils", "nvidia-settings", "libnvidia-compute-550", "nvidia-driver-libs",
		"nvidia-driver-libs-nonglvnd", "nvidia-driver-bin", "nvidia-driver-cuda",
		"nvidia-kernel-common", "nvidia-kernel-common-550", "nvidia-kernel-source",
		"nvidia-kernel-source-550", "nvidia-kernel-support", "nvidia-kmod-common",
		"xorg-x11-drv-nvidia-libs-550.76-1.fc40.x86_64", "xorg-x11-drv-nvidia-cuda",
		"xorg-x11-drv-nvidia-kmodsrc", "nvidia-legacy-390xx-driver-libs",
		"nvidia-driver-libs-550.76-1.fc40.x86_64", "nvidia-prime", "nvidia-persistenced",
	}
	for _, n := range yes {
		if !isNvidiaDriverPackage(n) {
			t.Errorf("isNvidiaDriverPackage(%q) should be true", n)
		}
	}
	for _, n := range no {
		if isNvidiaDriverPackage(n) {
			t.Errorf("isNvidiaDriverPackage(%q) should be false", n)
		}
	}
}

func TestCheckNouveauRestorable(t *testing.T) {
	if err := checkNouveauRestorable(nouveauBlacklistContent); err != nil {
		t.Errorf("our own content must be restorable, got %v", err)
	}
	if err := checkNouveauRestorable("blacklist nouveau\n"); err != nil {
		t.Errorf("a plain blacklist line must be restorable, got %v", err)
	}
	if err := checkNouveauRestorable("install nouveau /bin/true\n"); err == nil {
		t.Error("foreign directives must not be restorable")
	}
	if err := checkNouveauRestorable(""); err == nil {
		t.Error("empty content must not be restorable")
	}

	// Valid line by line, but too large for the journal's undo limit.
	var b strings.Builder
	for b.Len() <= maxUndoInfoBytes {
		b.WriteString("# padding comment line that is perfectly valid on its own\n")
	}
	err := checkNouveauRestorable(b.String())
	if err == nil {
		t.Fatalf("%d bytes of content must exceed the %d-byte undo limit", b.Len(), maxUndoInfoBytes)
	}
	if !strings.Contains(err.Error(), "undo limit") {
		t.Errorf("error should name the undo limit, got %v", err)
	}
	// The same content must be rejected by the journal gate, so apply can
	// never record something undo would later refuse.
	if validateUndoInfo("blacklist-nouveau", b.String()) == nil {
		t.Error("validateUndoInfo must reject oversized content too")
	}
}

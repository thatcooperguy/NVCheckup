//go:build linux

package remediate

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// packageManagers lists the read-only package queries used to detect an
// installed NVIDIA driver when the kernel module is not (yet) available to
// modinfo, e.g. right after installing the package but before rebooting.
var packageManagers = []struct {
	Name string
	Args []string
}{
	{"dpkg", []string{"-l"}},
	{"rpm", []string{"-qa"}},
	{"pacman", []string{"-Q"}},
}

// nvidiaDriverInstalled reports whether a proprietary/open NVIDIA kernel
// driver is present, returning a short evidence string. Blacklisting nouveau
// without a replacement driver leaves the machine with no accelerated display
// driver at all (black screen on many desktops), so this is a hard gate.
func (e *Engine) nvidiaDriverInstalled() (evidence string, ok bool) {
	if _, err := e.executor.Run("modinfo", "nvidia"); err == nil {
		return "kernel module 'nvidia' is available (modinfo nvidia)", true
	}
	for _, pm := range packageManagers {
		out, err := e.executor.Run(pm.Name, pm.Args...)
		if err != nil {
			continue
		}
		if packageListHasNvidiaDriver(out) {
			return fmt.Sprintf("NVIDIA driver package installed (%s)", cmdString(pm.Name, pm.Args...)), true
		}
	}
	return "", false
}

// readNouveauFile returns the current blacklist file content, absent=true when
// the file does not exist, or an error for any other failure.
func readNouveauFile() (content string, absent bool, err error) {
	data, err := os.ReadFile(nouveauBlacklistPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", true, nil
		}
		return "", false, fmt.Errorf("could not read %s: %w", nouveauBlacklistPath, err)
	}
	return string(data), false, nil
}

// restoreNouveauFile puts the blacklist file back to a captured state: removes
// it for absentSentinel (or the legacy v0.2.0 marker, see
// normalizeNouveauUndoInfo), otherwise writes the captured content. The
// content must already have passed validateNouveauContent.
func restoreNouveauFile(undoInfo string) error {
	if normalizeNouveauUndoInfo(undoInfo) == absentSentinel {
		if err := os.Remove(nouveauBlacklistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("could not remove %s: %w", nouveauBlacklistPath, err)
		}
		return nil
	}
	if err := os.WriteFile(nouveauBlacklistPath, []byte(undoInfo), 0644); err != nil {
		return fmt.Errorf("could not restore %s: %w", nouveauBlacklistPath, err)
	}
	return nil
}

// rebuildInitramfs runs the distro's initramfs tool through the executor.
func (e *Engine) rebuildInitramfs(tool initramfsTool) (string, error) {
	out, err := e.executor.Run(tool.Name, tool.Args...)
	if err != nil {
		return out, fmt.Errorf("%s failed: %w: %s", cmdString(tool.Name, tool.Args...), err, strings.TrimSpace(out))
	}
	return out, nil
}

// actionBlacklistNouveau writes the modprobe drop-in that stops nouveau from
// loading and rebuilds the initramfs so the change is honored at early boot.
//
// It refuses when no NVIDIA driver is installed, when the current file cannot
// be read, or when an existing file has content nvcheckup could not safely
// restore on undo. On initramfs failure the file is rolled back so the system
// is left exactly as found.
func (e *Engine) actionBlacklistNouveau() (output, undoInfo string, err error) {
	evidence, ok := e.nvidiaDriverInstalled()
	if !ok {
		return "", "", fmt.Errorf("refusing to blacklist nouveau: no NVIDIA driver detected " +
			"(modinfo nvidia failed and no nvidia driver package is installed). " +
			"Blacklisting nouveau without a replacement driver can leave the system without a working display. " +
			"Install the NVIDIA driver first, then re-run this fix")
	}

	existing, absent, err := readNouveauFile()
	if err != nil {
		return "", "", err
	}

	undoInfo = absentSentinel
	wrote := false
	switch {
	case absent:
		// Nothing to preserve; undo will remove the file.
	case existing == nouveauBlacklistContent:
		// Already in place (perhaps from an earlier run whose initramfs rebuild
		// did not happen). Keep the file, still rebuild below.
		undoInfo = existing
	default:
		if verr := checkNouveauRestorable(existing); verr != nil {
			return "", "", fmt.Errorf("refusing to overwrite %s: it already exists with content nvcheckup could not safely restore on undo (%v). Edit it manually", nouveauBlacklistPath, verr)
		}
		undoInfo = existing
	}

	if absent || existing != nouveauBlacklistContent {
		if werr := os.WriteFile(nouveauBlacklistPath, []byte(nouveauBlacklistContent), 0644); werr != nil {
			return "", "", fmt.Errorf("failed to write %s: %w", nouveauBlacklistPath, werr)
		}
		wrote = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Driver check: %s. ", evidence)
	if wrote {
		fmt.Fprintf(&b, "Wrote %s. ", nouveauBlacklistPath)
	} else {
		fmt.Fprintf(&b, "%s already had the expected content. ", nouveauBlacklistPath)
	}

	tool, found := detectInitramfsTool(lookPath)
	if !found {
		fmt.Fprintf(&b, "WARNING: no initramfs tool found (update-initramfs, dracut, mkinitcpio); "+
			"rebuild the initramfs manually before rebooting or nouveau may still load.")
		return b.String(), undoInfo, nil
	}

	if _, rerr := e.rebuildInitramfs(tool); rerr != nil {
		rollback := "restored previous state"
		if wrote {
			if rb := restoreNouveauFile(undoInfo); rb != nil {
				rollback = "rollback also failed: " + rb.Error()
			}
		}
		return b.String(), "", fmt.Errorf("initramfs rebuild failed (%v); %s", rerr, rollback)
	}
	fmt.Fprintf(&b, "Rebuilt initramfs with '%s' successfully. Reboot required.", cmdString(tool.Name, tool.Args...))
	return b.String(), undoInfo, nil
}

// undoBlacklistNouveau restores the file and rebuilds the initramfs again so
// the early-boot image no longer carries the blacklist.
func (e *Engine) undoBlacklistNouveau(undoInfo string) error {
	if err := restoreNouveauFile(undoInfo); err != nil {
		return err
	}
	tool, found := detectInitramfsTool(lookPath)
	if !found {
		return fmt.Errorf("%s restored, but no initramfs tool was found; rebuild the initramfs manually so nouveau is no longer blacklisted at boot", nouveauBlacklistPath)
	}
	_, err := e.rebuildInitramfs(tool)
	return err
}

// actionUpdateLdconfig runs ldconfig to refresh the shared library cache so
// libcuda.so and libnvidia-ml.so are registered after a driver install.
func (e *Engine) actionUpdateLdconfig() (output, undoInfo string, err error) {
	ldOutput, err := e.executor.Run("ldconfig")
	if err != nil {
		return ldOutput, "", fmt.Errorf("ldconfig failed: %w: %s", err, strings.TrimSpace(ldOutput))
	}
	msg := "ldconfig completed successfully"
	if strings.TrimSpace(ldOutput) != "" {
		msg += ": " + strings.TrimSpace(ldOutput)
	}
	// ldconfig is idempotent and only regenerates the cache; there is no prior
	// state to restore, so undo info is intentionally empty.
	return msg, "", nil
}

// applyAction dispatches a remediation action by ID to the appropriate
// Linux-specific implementation.
func (e *Engine) applyAction(id string) (output string, undoInfo string, err error) {
	switch id {
	case "blacklist-nouveau":
		return e.actionBlacklistNouveau()
	case "update-ldconfig":
		return e.actionUpdateLdconfig()
	default:
		return "", "", fmt.Errorf("unknown remediation action: %q", id)
	}
}

// undoAction reverses a previously applied Linux remediation action using the
// stored undo information. Callers must run validateUndoInfo first; nothing
// here writes journal-supplied content that has not passed that check.
func (e *Engine) undoAction(id string, undoInfo string) error {
	switch id {
	case "blacklist-nouveau":
		return e.undoBlacklistNouveau(undoInfo)
	case "update-ldconfig":
		// ldconfig is idempotent; running it again is the closest thing to undo.
		_, err := e.executor.Run("ldconfig")
		return err
	default:
		return fmt.Errorf("unknown action for undo: %q", id)
	}
}

// inspectAction performs the read-only capture for an action and describes the
// exact commands Apply and Undo would run.
func (e *Engine) inspectAction(id string) (inspection, error) {
	switch id {
	case "blacklist-nouveau":
		return e.inspectBlacklistNouveau()
	case "update-ldconfig":
		return e.inspectUpdateLdconfig()
	default:
		return inspection{}, fmt.Errorf("unknown remediation action: %q", id)
	}
}

func (e *Engine) inspectBlacklistNouveau() (inspection, error) {
	existing, absent, err := readNouveauFile()
	if err != nil {
		return inspection{}, err
	}
	evidence, driverOK := e.nvidiaDriverInstalled()
	if !driverOK {
		evidence = "NO NVIDIA driver detected; apply will be refused"
	}

	insp := inspection{UndoInfo: absentSentinel}
	restorable := true
	switch {
	case absent:
		insp.Current = fmt.Sprintf("%s does not exist; %s", nouveauBlacklistPath, evidence)
		insp.UndoCommands = []string{"rm " + nouveauBlacklistPath}
	case existing == nouveauBlacklistContent || checkNouveauRestorable(existing) == nil:
		insp.UndoInfo = existing
		insp.Current = fmt.Sprintf("%s exists (%d bytes); %s", nouveauBlacklistPath, len(existing), evidence)
		insp.UndoCommands = []string{"restore previous content of " + nouveauBlacklistPath}
	default:
		// Apply will refuse rather than overwrite something it cannot put
		// back, so there is nothing to record and no undo to describe.
		restorable = false
		insp.UndoInfo = ""
		insp.Current = fmt.Sprintf("%s exists (%d bytes) but its existing content is not restorable; apply will be refused. %s",
			nouveauBlacklistPath, len(existing), evidence)
	}

	insp.ApplyCommands = []string{
		fmt.Sprintf("write %s with: %s", nouveauBlacklistPath,
			strings.Join(strings.Split(strings.TrimSuffix(nouveauBlacklistContent, "\n"), "\n"), " | ")),
	}
	if tool, found := detectInitramfsTool(lookPath); found {
		rebuild := cmdString(tool.Name, tool.Args...)
		insp.ApplyCommands = append(insp.ApplyCommands, rebuild)
		if restorable {
			insp.UndoCommands = append(insp.UndoCommands, rebuild)
		}
	} else {
		insp.ApplyCommands = append(insp.ApplyCommands, "(no initramfs tool found; manual rebuild needed)")
	}
	return insp, nil
}

func (e *Engine) inspectUpdateLdconfig() (inspection, error) {
	// "ldconfig -p" is read-only and works unprivileged; count the libcuda
	// entries so the user can see whether the cache already knows about CUDA.
	current := "ldconfig cache state unknown"
	if out, err := e.executor.Run("ldconfig", "-p"); err == nil {
		n := 0
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "libcuda.so") {
				n++
			}
		}
		current = fmt.Sprintf("ldconfig cache lists %d libcuda.so entr(y/ies)", n)
	}
	return inspection{
		Current:       current,
		UndoInfo:      "",
		ApplyCommands: []string{"ldconfig"},
		UndoCommands:  []string{"ldconfig (cache is simply regenerated; nothing to restore)"},
	}, nil
}

// getAvailableActions returns the remediation actions available on Linux:
// the catalog entries (see catalog.go) whose Platform is "linux".
func getAvailableActions() []types.RemediationAction {
	return catalogForPlatform("linux")
}

package linux

// GB10 host-state facts that live on PlatformInfo rather than DGXOSInfo:
// firmware components, the clock-cap unit, previous-boot classification,
// pstore, ACPI thermal zones, the GDM sleep policy and suspend markers.
// Spec: docs/roadmap/spark-support.md WP1 item (6) and section 4
// (PlatformInfo fields). Read-only; the runner calls it on dgx-spark only.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const (
	// clockCapUnit is the systemd unit that locks GB10 clocks (spec 4
	// PlatformInfo.ClockCapUnit comment, rule gb10-clock-cap-active, S48).
	clockCapUnit = "gb10-clock-cap.service"
	// clockCapUnitFile is where the S48 instructions install that unit; rule
	// gb10-clock-cap-active triggers on "gb10-clock-cap.service exists", not
	// only on it being active.
	clockCapUnitFile = "/etc/systemd/system/" + clockCapUnit
	// nvidiaSuspendUnit is the driver's suspend hook whose failed state is
	// the second variant of rule dgx-spark-suspend-failure ("OR
	// nvidia-suspend.service failed").
	nvidiaSuspendUnit = "nvidia-suspend.service"
	// pstoreDir holds crash records that a logless hard power-off leaves
	// empty (rule gb10-logless-hard-poweroff, S48).
	pstoreDir = "/sys/fs/pstore"
	// thermalDir holds the acpitz zones, the only extra sensors on GB10
	// (spec 2.1 "Power/thermal").
	thermalDir = "/sys/class/thermal"
	// acpiThermalType is the sysfs type of the ACPI zones.
	acpiThermalType = "acpitz"
	// gdmGreeterDefaults carries the GDM greeter's sleep policy on Ubuntu
	// (rule dgx-spark-suspend-failure, "headless GDM sleep policy", S52 S127).
	gdmGreeterDefaults = "/etc/gdm3/greeter.dconf-defaults"
	// gdmSleepKey is the dconf key for the greeter's on-AC idle action.
	gdmSleepKey = "sleep-inactive-ac-type"
	// maxBootsChecked bounds how many previous boots are classified.
	maxBootsChecked = 5
	// UncleanBootWindowDays is the {days} window of rule
	// gb10-logless-hard-poweroff (spark-rules.json: "in the last {days} days
	// (default 14)"). Exported so the analyzer's evidence template can print
	// the same window that UncleanBoots was counted over.
	UncleanBootWindowDays = 14
	// prevBootTailLines is how many lines of the previous boot are inspected
	// for a clean-shutdown marker.
	prevBootTailLines = 30
)

// cleanShutdownMarkers are the journal lines that end a clean boot (WP1 item
// 6 wording, S48). Any of them in the tail of a boot marks it clean.
var cleanShutdownMarkers = []string{
	"Journal stopped",
	"systemd-shutdown",
	"Shutting down.",
	"Reached target Power-Off",
	"Reached target Reboot",
	"Reached target Halt",
}

// loggedFailureMarkers identify boots that ended in a logged failure: kernel
// panic, OOM kill, Xid, thermal shutdown. Rule gb10-logless-hard-poweroff
// requires "no panic/OOM/Xid/thermal lines", so such boots are unclean but
// not log-less and never count towards UncleanBoots (a GSP crash, rule
// dgx-spark-gsp-init-failure, must not look like a hard power-off). Matched
// case-insensitively.
var loggedFailureMarkers = []string{
	"kernel panic",
	"out of memory:",
	"nvrm: xid",
	"critical temperature",
	"thermal shutdown",
}

// loggedFailureMarkerPairs are failure signatures that need two substrings on
// the same line: a thermal zone reporting a critical trip. The bare word
// "thermal" is deliberately not a marker ("Started thermald.service" is a
// normal boot line, not a failure).
var loggedFailureMarkerPairs = [][2]string{
	{"thermal_zone", "critical"},
}

// suspendEntryMarker counts suspend attempts; suspendFailureMarkers mark a
// failed cycle (rule dgx-spark-suspend-failure: s2idle +
// nv_set_system_power_state warning, S51 S52).
const suspendEntryMarker = "PM: suspend entry"

var suspendFailureMarkers = []string{
	"nv_set_system_power_state",
	"suspend of devices failed",
	"Failed to suspend",
}

// CollectDGXHostState fills the GB10 host facts of PlatformInfo. Every probe
// is optional; unreadable sources leave the pointer fields nil.
func CollectDGXHostState(timeout int, p *types.PlatformInfo) []types.CollectorError {
	var errs []types.CollectorError
	if p == nil {
		return errs
	}

	if util.CommandExists("fwupdmgr") {
		// get-devices output is shared with CollectDGXOS through runFwupdmgr.
		comps := parseFwupdDevices(runFwupdmgr(timeout, "get-devices").Stdout)
		// get-upgrades exits 2 when nothing is pending; its stdout is parsed
		// regardless (rule dgx-spark-firmware-behind, OEM path: "report only
		// pending capsules from fwupdmgr get-upgrades").
		comps = applyPendingFirmware(comps, parseFwupdUpgrades(runFwupdmgr(timeout, "get-upgrades").Stdout))
		if len(comps) > 0 {
			p.Firmware = comps
		}
	}

	p.ClockCapUnit = clockCapUnitState(timeout)

	collectBootHistory(p, timeout, time.Now())
	p.PstoreEmpty = dirEmpty(pstoreDir)
	if zones := readACPIThermalZones(); len(zones) > 0 {
		p.ACPIThermalMC = zones
	}
	if content := readSimFile(gdmGreeterDefaults); content != "" {
		p.GDMSleepPolicy = parseGDMSleepPolicy(content)
	}
	collectSuspendMarkers(p, timeout)

	return errs
}

// fwupdKV matches "Key:   value" attribute lines of fwupdmgr get-devices
// after the tree characters have been stripped.
var fwupdKV = regexp.MustCompile(`^([A-Za-z][A-Za-z ]+):\s+(.*)$`)

// stripTreeChars removes the box-drawing prefix fwupdmgr draws.
func stripTreeChars(line string) string {
	return strings.TrimLeft(line, " \t│├─└┬┼")
}

// parseFwupdDevices parses "fwupdmgr get-devices" output. A device starts
// with a line that ends in ":" and carries no other colon (e.g. "Embedded
// Controller:"); "Current version" (dotted or hex, decoded by
// normalizeFirmwareVersion), the first "GUID" and a non-success "Update
// State" are kept.
func parseFwupdDevices(out string) []types.FirmwareComponent {
	var comps []types.FirmwareComponent
	var cur *types.FirmwareComponent
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(stripTreeChars(raw))
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, ":") && strings.Count(line, ":") == 1 {
			comps = append(comps, types.FirmwareComponent{Name: strings.TrimSuffix(line, ":")})
			cur = &comps[len(comps)-1]
			continue
		}
		if cur == nil {
			continue
		}
		m := fwupdKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2])
		switch key {
		case "current version":
			cur.Version = normalizeFirmwareVersion(val)
		case "guid":
			if cur.GUID == "" {
				cur.GUID = strings.Fields(val)[0]
			}
		case "update state":
			if l := strings.ToLower(val); l != "" && l != "success" && l != "unknown" {
				cur.Pending = val
			}
		}
	}
	return comps
}

// parseFwupdUpgrades parses "fwupdmgr get-upgrades" into device name ->
// pending version. A device block ("System Firmware:" followed by Device ID /
// Current version / GUIDs) is followed by one or more release blocks ("UEFI
// Firmware Update:" ... "New version: 2.160.1"); the first, newest, New
// version per device is kept. The "Devices with the latest available
// firmware version:" bullet list has no key/value rows and is skipped. Rule
// dgx-spark-firmware-behind (OEM path). ASSUMPTION: layout taken from
// fwupdmgr's documented tree output pending a GB10 capture (spec section 12).
func parseFwupdUpgrades(out string) map[string]string {
	pending := map[string]string{}
	var device, header string
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(stripTreeChars(raw))
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, ":") && strings.Count(line, ":") == 1 {
			header = strings.TrimSuffix(line, ":")
			continue
		}
		m := fwupdKV.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key, val := strings.ToLower(strings.TrimSpace(m[1])), strings.TrimSpace(m[2])
		switch key {
		case "device id", "current version", "guid", "guids":
			// Keys that only device blocks carry: the last header named a device.
			if header != "" {
				device, header = header, ""
			}
		case "new version":
			// The last header (if any) named a release of the current device.
			header = ""
			if device != "" && val != "" && pending[device] == "" {
				pending[device] = normalizeFirmwareVersion(val)
			}
		}
	}
	return pending
}

// applyPendingFirmware fills FirmwareComponent.Pending from the get-upgrades
// map (a pending capsule version replaces the bare "Update State" word that
// get-devices may have left there) and appends devices that get-devices did
// not list.
func applyPendingFirmware(comps []types.FirmwareComponent, pending map[string]string) []types.FirmwareComponent {
	if len(pending) == 0 {
		return comps
	}
	seen := map[string]bool{}
	for i := range comps {
		seen[comps[i].Name] = true
		if v, ok := pending[comps[i].Name]; ok {
			comps[i].Pending = v
		}
	}
	names := make([]string, 0, len(pending))
	for n := range pending {
		if !seen[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		comps = append(comps, types.FirmwareComponent{Name: n, Pending: pending[n]})
	}
	return comps
}

// clockCapUnitState returns "" when gb10-clock-cap.service neither runs nor
// exists, the bare unit name when it is active, and "gb10-clock-cap.service
// (<state>)" when the unit file exists but is inactive or failed. systemctl
// is-active prints "inactive" for a missing unit and for an installed but
// stopped one alike, so existence is checked separately (unit file under
// /etc/systemd/system or LoadState=loaded).
func clockCapUnitState(timeout int) string {
	state := unitState(timeout, clockCapUnit)
	if state == "active" {
		return clockCapUnit
	}
	if !common.SimFileExists(clockCapUnitFile) && !unitLoaded(timeout, clockCapUnit) {
		return ""
	}
	if state == "" {
		state = "inactive"
	}
	return clockCapUnit + " (" + state + ")"
}

// unitLoaded reports whether systemd knows the unit file (LoadState=loaded;
// a missing unit prints not-found).
func unitLoaded(timeout int, unit string) bool {
	if !util.CommandExists("systemctl") {
		return false
	}
	r := util.RunCommand(timeout, "systemctl", "show", "-p", "LoadState", "--value", unit)
	return firstLineOfText(r.Stdout) == "loaded"
}

// hexVersionRe matches the hex form fwupd reports for some GB10 components,
// e.g. 0x03000508.
var hexVersionRe = regexp.MustCompile(`^0[xX]([0-9a-fA-F]{1,8})$`)

// normalizeFirmwareVersion converts a hex firmware version to dotted form as
// major(8 bits).minor(16 bits).patch(8 bits): 0x03000508 = 3.5.8,
// 0x02009b0b = 2.155.11, 0x00000516 = 0.5.22. ASSUMPTION (spec rule
// dgx-spark-firmware-behind): this decoding is arithmetic pending an fwupdmgr
// capture. Dotted versions are returned unchanged.
func normalizeFirmwareVersion(v string) string {
	v = strings.TrimSpace(v)
	m := hexVersionRe.FindStringSubmatch(v)
	if m == nil {
		return v
	}
	n, err := strconv.ParseUint(m[1], 16, 32)
	if err != nil {
		return v
	}
	return strconv.FormatUint(n>>24, 10) + "." + strconv.FormatUint((n>>8)&0xffff, 10) + "." + strconv.FormatUint(n&0xff, 10)
}

// bootListDateRe finds the ISO dates in a "journalctl --list-boots" row.
var bootListDateRe = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// bootListIndexRe matches the leading boot offset ("0", "-1", "-2") of a
// --list-boots row (old and new journalctl layouts both start with it).
var bootListIndexRe = regexp.MustCompile(`^\s*(-?\d+)\s`)

// bootEntry is one previous boot from --list-boots.
type bootEntry struct {
	Index   int       // 0 = current, -1 = previous, ...
	LastDay time.Time // date of the boot's last journal entry; zero when unparsed
}

// parseBootList parses "journalctl --list-boots" rows into previous boots
// (index < 0), most recent first. Header rows and the current boot are
// skipped.
func parseBootList(out string) []bootEntry {
	var boots []bootEntry
	for _, line := range strings.Split(out, "\n") {
		m := bootListIndexRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		idx, err := strconv.Atoi(m[1])
		if err != nil || idx >= 0 {
			continue
		}
		b := bootEntry{Index: idx}
		if dates := bootListDateRe.FindAllString(line, -1); len(dates) > 0 {
			if t, err := time.Parse("2006-01-02", dates[len(dates)-1]); err == nil {
				b.LastDay = t
			}
		}
		boots = append(boots, b)
	}
	sort.Slice(boots, func(i, j int) bool { return boots[i].Index > boots[j].Index })
	return boots
}

// bootTail is the classification of one boot's final journal lines.
type bootTail struct {
	Clean         bool   // a clean-shutdown marker is present
	LoggedFailure bool   // a panic/OOM/Xid/thermal line is present
	LastLine      string // last non-empty line, truncated
}

// Logless reports whether the boot ended with neither a clean-shutdown marker
// nor a logged failure: the signature rule gb10-logless-hard-poweroff counts.
func (b bootTail) Logless() bool { return !b.Clean && !b.LoggedFailure }

// classifyBootTail inspects a boot's final journal lines for clean-shutdown
// and logged-failure markers and keeps the last non-empty line.
func classifyBootTail(out string) bootTail {
	var b bootTail
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if b.LastLine == "" {
			b.LastLine = util.TruncateString(l, 200)
		}
		for _, marker := range cleanShutdownMarkers {
			if strings.Contains(l, marker) {
				b.Clean = true
			}
		}
		lower := strings.ToLower(l)
		for _, marker := range loggedFailureMarkers {
			if strings.Contains(lower, marker) {
				b.LoggedFailure = true
			}
		}
		for _, pair := range loggedFailureMarkerPairs {
			if strings.Contains(lower, pair[0]) && strings.Contains(lower, pair[1]) {
				b.LoggedFailure = true
			}
		}
	}
	return b
}

// collectBootHistory classifies the previous boot (PrevBootClean,
// PrevBootLastLine) and counts log-less boots inside the window
// (UncleanBoots: no clean-shutdown marker AND no panic/OOM/Xid/thermal line,
// per rule gb10-logless-hard-poweroff). Without journalctl or a persistent
// journal everything stays nil/zero. The tail is read with -o cat (message
// only) so no hostname reaches PrevBootLastLine.
func collectBootHistory(p *types.PlatformInfo, timeout int, now time.Time) {
	if !util.CommandExists("journalctl") {
		return
	}
	r := util.RunCommand(timeout, "journalctl", "--list-boots", "--no-pager", "-q")
	if r.Err != nil && strings.TrimSpace(r.Stdout) == "" {
		return
	}
	boots := parseBootList(r.Stdout)
	if len(boots) > maxBootsChecked {
		boots = boots[:maxBootsChecked]
	}
	cutoff := now.AddDate(0, 0, -UncleanBootWindowDays)
	for _, b := range boots {
		tail := util.RunCommand(timeout, "journalctl", "-b", strconv.Itoa(b.Index), "--no-pager", "-q", "-o", "cat", "-n", strconv.Itoa(prevBootTailLines))
		if strings.TrimSpace(tail.Stdout) == "" {
			continue
		}
		t := classifyBootTail(tail.Stdout)
		if b.Index == -1 {
			c := t.Clean
			p.PrevBootClean = &c
			p.PrevBootLastLine = t.LastLine
		}
		inWindow := b.LastDay.IsZero() || !b.LastDay.Before(cutoff)
		if t.Logless() && inWindow {
			p.UncleanBoots++
		}
	}
}

// dirEmpty reports whether a directory (through simPath) has no entries; nil
// when it cannot be read.
func dirEmpty(dir string) *bool {
	entries, err := os.ReadDir(common.SimPath(dir))
	if err != nil {
		return nil
	}
	empty := len(entries) == 0
	return &empty
}

// readACPIThermalZones returns thermal_zoneN -> millidegrees for every
// acpitz zone under /sys/class/thermal (through simPath).
func readACPIThermalZones() map[string]int {
	matches, _ := filepath.Glob(filepath.Join(common.SimPath(thermalDir), "thermal_zone*"))
	zones := map[string]int{}
	for _, zone := range matches {
		typ, err := os.ReadFile(filepath.Join(zone, "type"))
		if err != nil || strings.TrimSpace(string(typ)) != acpiThermalType {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(zone, "temp"))
		if err != nil {
			continue
		}
		mc, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			continue
		}
		zones[filepath.Base(zone)] = mc
	}
	return zones
}

// parseGDMSleepPolicy returns the uncommented sleep-inactive-ac-type value of
// a greeter dconf defaults file ("nothing", "suspend", ...), or "" when the
// key is absent or commented out (GNOME's default then applies; the analyzer
// decides what that means).
func parseGDMSleepPolicy(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v := util.ParseKeyValue(line, "=")
		if k == gdmSleepKey {
			return strings.Trim(v, `'"`)
		}
	}
	return ""
}

// parseSuspendMarkers counts "PM: suspend entry" lines and reports whether
// any failure marker appears.
func parseSuspendMarkers(out string) (attempts int, failed bool) {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, suspendEntryMarker) {
			attempts++
		}
		for _, m := range suspendFailureMarkers {
			if strings.Contains(line, m) {
				failed = true
			}
		}
	}
	return attempts, failed
}

// collectSuspendMarkers reads the current boot's journal (dmesg fallback) for
// suspend attempts and failures, then checks the nvidia-suspend.service unit
// (rule dgx-spark-suspend-failure: "... OR nvidia-suspend.service failed").
func collectSuspendMarkers(p *types.PlatformInfo, timeout int) {
	var out string
	switch {
	case util.CommandExists("journalctl"):
		pattern := suspendEntryMarker + "|" + strings.Join(suspendFailureMarkers, "|")
		r := util.RunCommand(timeout, "journalctl", "-b", "--no-pager", "-q", "-g", pattern)
		out = r.Stdout
	case util.CommandExists("dmesg"):
		r := util.RunCommand(timeout, "dmesg")
		out = r.Stdout
	}
	if out != "" {
		p.SuspendAttempts, p.SuspendFailed = parseSuspendMarkers(out)
	}
	if unitState(timeout, nvidiaSuspendUnit) == "failed" {
		p.SuspendFailed = true
		if p.SuspendAttempts == 0 {
			p.SuspendAttempts = 1
		}
	}
}

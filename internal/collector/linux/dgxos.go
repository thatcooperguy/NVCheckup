package linux

// DGX OS collector for DGX Spark (GB10). Everything here is read-only and
// tolerant of partial data: a missing file, tool or unit simply leaves the
// corresponding field empty. The file carries no build tag so its parsers are
// unit-tested on every OS; the runner only calls it on Linux when
// Platform.Class is dgx-spark (spec section 4, "Placement").
//
// Spec: docs/roadmap/spark-support.md sections 2.1, 3.1 (row 4), 3.2 and
// work package WP1 item (6).

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const (
	// dgxReleaseFile carries the DGX_* key=value lines (spec 3.1 row 4, S104).
	dgxReleaseFile = "/etc/dgx-release"
	// fastosReleaseFile: NAME="DGX SPARK FASTOS" (spec 3.1 row 4).
	fastosReleaseFile = "/etc/fastos-release"
	// otaCheckTool and its sub-commands (spec 2.1 "Updates").
	otaCheckTool = "nvidia-spark-ota-check"
	// otaCheckTimeoutSec bounds each nvidia-spark-ota-check call (WP1 item 6: 10 s).
	otaCheckTimeoutSec = 10
	// dashboardPort is the DGX Dashboard listener (spec 2.1 "Updates": http://localhost:11000).
	dashboardPort = 11000
	// containerToolkitAptList is the apt source whose first line is checked
	// for corruption (WP1 item 6, S44).
	containerToolkitAptList = "/etc/apt/sources.list.d/nvidia-container-toolkit.list"
)

// Systemd units whose state DGXOSInfo records (spec 2.1 "Updates" and WP1
// item 6).
const (
	unitDashboard      = "dgx-dashboard.service"
	unitDashboardAdmin = "dgx-dashboard-admin.service"
	unitFwupd          = "fwupd.service"
	unitPersistenced   = "nvidia-persistenced.service"
)

// dpkgPatterns are the package families the pairing rules look at (spec 3.2
// "Packages"): driver metapackage, firmware, kernel modules, dkms, plus the
// DGX tooling whose versions are informational.
var dpkgPatterns = []string{
	"nvidia-driver-*",
	"nvidia-firmware-*",
	"linux-modules-nvidia-*",
	"nvidia-dkms-*",
	"dgx-release",
	"dgx-dashboard",
	"nvidia-spark-ota-check",
}

// CollectDGXOS gathers DGX OS release, OTA, package pairing and service
// state. It never fails: missing inputs leave fields empty and only tool
// failures worth explaining are reported as non-fatal CollectorErrors.
func CollectDGXOS(timeout int) (types.DGXOSInfo, []types.CollectorError) {
	var info types.DGXOSInfo
	var errs []types.CollectorError

	if content := readSimFile(dgxReleaseFile); content != "" {
		applyDGXRelease(&info, parseDGXRelease(content))
	}
	if content := readSimFile(fastosReleaseFile); content != "" {
		info.FastOSVersion = parseFastOSRelease(content)
	}

	collectOTAState(&info, &errs)
	collectDGXPackages(&info, &errs, timeout)
	collectDGXUnits(&info, timeout)
	info.DashboardPortOpen = tcpPortListening(dashboardPort)
	collectFwupdError(&info, timeout)
	info.AptSourceCorrupt = checkAptSource(containerToolkitAptList)

	return info, errs
}

// parseDGXRelease parses the key=value lines of /etc/dgx-release. Values are
// unquoted; blank lines and comments are skipped. Keys are returned verbatim
// (DGX_NAME, DGX_PRETTY_NAME, DGX_SWBUILD_VERSION, ...; list per S104).
func parseDGXRelease(content string) map[string]string {
	kv := make(map[string]string)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v := util.ParseKeyValue(line, "=")
		if k == "" {
			continue
		}
		kv[k] = strings.Trim(v, `"`)
	}
	return kv
}

// applyDGXRelease copies the known DGX_* keys into DGXOSInfo (spec 3.1 row 4
// and 3.2; key list per S104). DGX_SERIAL_NUMBER is stored as-is and
// redacted by internal/redact.
func applyDGXRelease(info *types.DGXOSInfo, kv map[string]string) {
	info.Name = kv["DGX_NAME"]
	info.PrettyName = kv["DGX_PRETTY_NAME"]
	info.SWBuildVersion = kv["DGX_SWBUILD_VERSION"]
	info.SWBuildDate = kv["DGX_SWBUILD_DATE"]
	info.Platform = kv["DGX_PLATFORM"]
	info.CommitID = kv["DGX_COMMIT_ID"]
	info.SerialNumber = kv["DGX_SERIAL_NUMBER"]
	info.OTAVersion = kv["DGX_OTA_VERSION"]
	info.OTADate = kv["DGX_OTA_DATE"]
}

// parseFastOSRelease returns the VERSION of /etc/fastos-release when its NAME
// is the DGX Spark FastOS (spec 3.1 row 4: NAME="DGX SPARK FASTOS"). When the
// NAME differs the version is prefixed with it so the report shows what was
// found.
func parseFastOSRelease(content string) string {
	kv := parseDGXRelease(content)
	version := kv["VERSION"]
	name := kv["NAME"]
	switch {
	case version != "" && strings.EqualFold(name, "DGX SPARK FASTOS"):
		return version
	case version != "" && name != "":
		return name + " " + version
	case version != "":
		return version
	default:
		return name
	}
}

// otaSummaryFailedRe extracts the failed list of "nvidia-spark-ota-check
// summary", e.g. `failed: ["driver"]` (spec 3.2) or `failed: []`.
var otaSummaryFailedRe = regexp.MustCompile(`failed:\s*\[([^\]]*)\]`)

// otaNameRe matches OTA names of the form OTAyymm (spec 2.1 "Updates").
var otaNameRe = regexp.MustCompile(`\bOTA\d{4}\b`)

// parseOTASummary parses the one-line summary
// "detected_ota OTA2607, match 100.0%, total_checks 153, passed_checks 153, failed: []"
// into the OTA name and the failed component list.
func parseOTASummary(out string) (name string, failed []string) {
	if m := otaNameRe.FindString(out); m != "" {
		name = m
	}
	if m := otaSummaryFailedRe.FindStringSubmatch(out); m != nil {
		for _, item := range strings.Split(m[1], ",") {
			item = strings.Trim(strings.TrimSpace(item), `"'`)
			if item != "" {
				failed = append(failed, item)
			}
		}
	}
	return name, failed
}

// tornScoreRe captures the integer that follows a "torn score" / "torn-score"
// / "torn_score" label on the same line, separated only by an optional "for",
// whitespace, ":" or "=" (so "torn-score|installed-name>" in a usage banner
// never reaches a number further down the text).
var tornScoreRe = regexp.MustCompile(`(?i)torn[-_ ]?score(?:[ 	]+for)?[ 	]*[:=]?[ 	]*(-?\d+)`)

// bareIntRe matches output that is nothing but one integer.
var bareIntRe = regexp.MustCompile(`^-?\d+$`)

// parseTornScore returns the score printed by "nvidia-spark-ota-check
// torn-score". ASSUMPTION: the exact output format of torn-score is not in
// the spec (only that a score > 0 means a torn OTA; spec 2.1 "Updates"). The
// number after a "torn-score" label is preferred; without the label the LAST
// integer is taken after removing OTA names (OTA2607, spec 2.1), so the OTA
// name can never be mistaken for the score. Pending a field capture (spec
// section 12).
// Only two shapes are accepted: a labelled "torn-score: N" anywhere in the
// output, or the whole trimmed output being a single integer. Any other text
// (error banners, usage help, version strings) yields no score rather than a
// guessed one.
func parseTornScore(out string) (int, bool) {
	stripped := otaNameRe.ReplaceAllString(out, "")
	if m := tornScoreRe.FindStringSubmatch(stripped); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n, true
		}
	}
	trimmed := strings.TrimSpace(out)
	if bareIntRe.MatchString(trimmed) {
		if n, err := strconv.Atoi(trimmed); err == nil {
			return n, true
		}
	}
	return 0, false
}

func collectOTAState(info *types.DGXOSInfo, errs *[]types.CollectorError) {
	if !util.CommandExists(otaCheckTool) {
		return
	}
	r := util.RunCommand(otaCheckTimeoutSec, otaCheckTool, "summary")
	if r.TimedOut {
		*errs = append(*errs, types.CollectorError{Collector: "linux.dgxos.ota", Error: otaCheckTool + " summary timed out after 10 s"})
		return
	}
	if out := strings.TrimSpace(r.Stdout + "\n" + r.Stderr); out != "" {
		name, failed := parseOTASummary(out)
		if name != "" {
			info.OTAName = name
		}
		info.OTAFailed = failed
	}
	// The score is only trusted on exit status 0: unprivileged runs print
	// "Error: nvidia-spark-ota-check must be run as root (uid 1000)" and exit
	// non-zero (spec section 12 runs the tool with sudo).
	r = util.RunCommand(otaCheckTimeoutSec, otaCheckTool, "torn-score")
	if r.Err == nil {
		if n, ok := parseTornScore(r.Stdout); ok {
			info.OTATorn = &n
		}
	}
	if info.OTAName == "" {
		r = util.RunCommand(otaCheckTimeoutSec, otaCheckTool, "installed-name")
		if m := otaNameRe.FindString(r.Stdout); m != "" {
			info.OTAName = m
		}
	}
}

// dpkgPackage is one installed-package row.
type dpkgPackage struct {
	Name      string
	Version   string
	Installed bool
}

// parseDpkgQuery parses rows printed by
// dpkg-query -W -f '${Package}\t${Version}\t${Status}\n'.
// Rows without a tab are ignored; a missing status column counts as installed.
func parseDpkgQuery(out string) []dpkgPackage {
	var pkgs []dpkgPackage
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		p := dpkgPackage{Name: strings.TrimSpace(fields[0]), Version: strings.TrimSpace(fields[1]), Installed: true}
		if len(fields) >= 3 {
			p.Installed = strings.Contains(fields[2], "installed") && !strings.Contains(fields[2], "not-installed")
		}
		if p.Name != "" {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs
}

// parseDpkgList parses "dpkg -l" rows ("ii  name  version  arch  description")
// as the fallback when dpkg-query is unavailable (the simulated scenario ships
// a dpkg shim, spec section 10). Only rows whose first column is a dpkg
// status pair (ii, rc, un, hi, ...) are considered.
func parseDpkgList(out string) []dpkgPackage {
	var pkgs []dpkgPackage
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || len(fields[0]) != 2 {
			continue
		}
		switch fields[0][0] {
		case 'i', 'r', 'u', 'h', 'p':
		default:
			continue
		}
		pkgs = append(pkgs, dpkgPackage{
			Name:      fields[1],
			Version:   fields[2],
			Installed: fields[0][1] == 'i',
		})
	}
	return pkgs
}

var (
	// driverPkgRe matches the driver metapackage, e.g. nvidia-driver-580-open
	// (spec 3.2). Non-open and -server variants match too so a foreign
	// install still yields a version for the pairing evidence.
	driverPkgRe = regexp.MustCompile(`^nvidia-driver-(\d+)(-open)?(-server)?$`)
	// firmwarePkgRe matches nvidia-firmware-580-<ver> (spec 3.2).
	firmwarePkgRe = regexp.MustCompile(`^nvidia-firmware-(\d+)-`)
	// modulesPkgRe matches linux-modules-nvidia-580-open-<kernel> (spec 3.2).
	modulesPkgRe = regexp.MustCompile(`^linux-modules-nvidia-(\d+)(-open)?-(.+)$`)
)

// dgxPackageFacts derives the pairing fields from the package rows: driver
// and firmware package versions (open variant preferred) and whether a
// linux-modules-nvidia-* package matches the running kernel.
func dgxPackageFacts(pkgs []dpkgPackage, kernel string) (driver, firmware string, modulesForKernel bool) {
	var fallbackDriver string
	var firmwares []string
	for _, p := range pkgs {
		if !p.Installed {
			continue
		}
		if m := driverPkgRe.FindStringSubmatch(p.Name); m != nil {
			if m[2] == "-open" && m[3] == "" {
				if driver == "" {
					driver = p.Version
				}
			} else if fallbackDriver == "" {
				fallbackDriver = p.Version
			}
			continue
		}
		if firmwarePkgRe.MatchString(p.Name) {
			firmwares = append(firmwares, p.Version)
			continue
		}
		if m := modulesPkgRe.FindStringSubmatch(p.Name); m != nil && kernel != "" && m[3] == kernel {
			modulesForKernel = true
		}
	}
	if driver == "" {
		driver = fallbackDriver
	}
	return driver, pickFirmwareVersion(firmwares, driver), modulesForKernel
}

// pickFirmwareVersion chooses among the installed nvidia-firmware-<branch>-*
// rows. After an OTA the previous versioned firmware package (e.g.
// nvidia-firmware-580-580.126.09) routinely stays installed until autoremove
// and sorts before the current one in dpkg-query output, so taking the first
// row would report a false pairing mismatch (rule dgx-spark-ota-torn:
// "nvidia-driver-580-open version != nvidia-firmware-580-* version"). The row
// equal to the driver version wins; otherwise the highest version by dpkg
// ordering.
func pickFirmwareVersion(versions []string, driver string) string {
	best := ""
	for _, v := range versions {
		if v == driver && v != "" {
			return v
		}
		if best == "" || compareDebVersion(best, v) < 0 {
			best = v
		}
	}
	return best
}

// compareDebVersion orders two Debian package versions like dpkg
// --compare-versions: epoch, then upstream, then revision, each compared by
// alternating non-digit and digit runs with '~' sorting before everything
// (including the end of the string). Returns -1, 0 or 1.
func compareDebVersion(a, b string) int {
	ea, ua, ra := splitDebVersion(a)
	eb, ub, rb := splitDebVersion(b)
	switch {
	case ea < eb:
		return -1
	case ea > eb:
		return 1
	}
	if c := compareDebPart(ua, ub); c != 0 {
		return c
	}
	return compareDebPart(ra, rb)
}

// splitDebVersion splits [epoch:]upstream[-revision].
func splitDebVersion(v string) (epoch int, upstream, revision string) {
	if i := strings.IndexByte(v, ':'); i >= 0 {
		epoch, _ = strconv.Atoi(v[:i])
		v = v[i+1:]
	}
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		return epoch, v[:i], v[i+1:]
	}
	return epoch, v, ""
}

func isASCIIDigit(c byte) bool { return c >= '0' && c <= '9' }

// debCharOrder ranks a byte for the non-digit comparison (dpkg's order()):
// '~' before everything, then end-of-string/digit, then letters, then the
// remaining punctuation.
func debCharOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case c == 0 || isASCIIDigit(c):
		return 0
	case (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z'):
		return int(c)
	default:
		return int(c) + 256
	}
}

func compareDebPart(a, b string) int {
	at := func(s string) byte {
		if s == "" {
			return 0
		}
		return s[0]
	}
	for a != "" || b != "" {
		for (a != "" && !isASCIIDigit(a[0])) || (b != "" && !isASCIIDigit(b[0])) {
			oa, ob := debCharOrder(at(a)), debCharOrder(at(b))
			if oa != ob {
				if oa < ob {
					return -1
				}
				return 1
			}
			// Same order and at least one side is a non-digit character, so
			// both sides hold the same character: advance both.
			a, b = a[1:], b[1:]
		}
		i, j := 0, 0
		for i < len(a) && isASCIIDigit(a[i]) {
			i++
		}
		for j < len(b) && isASCIIDigit(b[j]) {
			j++
		}
		da, db := strings.TrimLeft(a[:i], "0"), strings.TrimLeft(b[:j], "0")
		a, b = a[i:], b[j:]
		switch {
		case len(da) != len(db):
			if len(da) < len(db) {
				return -1
			}
			return 1
		case da != db:
			if da < db {
				return -1
			}
			return 1
		}
	}
	return 0
}

// runningKernel returns the running kernel release from
// /proc/sys/kernel/osrelease (through simPath) or "uname -r".
func runningKernel(timeout int) string {
	if v := readSimFile("/proc/sys/kernel/osrelease"); v != "" {
		return v
	}
	if util.CommandExists("uname") {
		r := util.RunCommand(timeout, "uname", "-r")
		if r.Err == nil {
			return strings.TrimSpace(r.Stdout)
		}
	}
	return ""
}

// nvidiaModulesOnDisk reports whether an nvidia kernel module for the
// running kernel exists on disk: Ubuntu's linux-modules-nvidia packages
// install under /lib/modules/<kernel>/kernel/nvidia-<branch>/, DKMS builds
// under /lib/modules/<kernel>/updates/dkms/ (WP1 item 6 wording).
func nvidiaModulesOnDisk(kernel string) bool {
	if kernel == "" {
		return false
	}
	base := simPath("/lib/modules/" + kernel)
	for _, pattern := range []string{
		filepath.Join(base, "updates", "dkms", "nvidia.ko*"),
		filepath.Join(base, "kernel", "nvidia-*", "nvidia.ko*"),
	} {
		if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
			return true
		}
	}
	return false
}

func collectDGXPackages(info *types.DGXOSInfo, errs *[]types.CollectorError, timeout int) {
	kernel := runningKernel(timeout)
	var pkgs []dpkgPackage
	switch {
	case util.CommandExists("dpkg-query"):
		args := append([]string{"-W", "-f", "${Package}\t${Version}\t${Status}\n"}, dpkgPatterns...)
		// dpkg-query exits 1 when a pattern matches nothing but still prints
		// the rows it found, so stdout is used regardless of the exit code.
		r := util.RunCommand(timeout, "dpkg-query", args...)
		pkgs = parseDpkgQuery(r.Stdout)
		if len(pkgs) == 0 && r.TimedOut {
			*errs = append(*errs, types.CollectorError{Collector: "linux.dgxos.dpkg", Error: "dpkg-query timed out"})
		}
	case util.CommandExists("dpkg"):
		r := util.RunCommand(timeout, "dpkg", "-l")
		pkgs = parseDpkgList(r.Stdout)
	default:
		return
	}
	info.DriverPkgVersion, info.FirmwarePkgVersion, info.ModulesForKernel = dgxPackageFacts(pkgs, kernel)
	if !info.ModulesForKernel {
		info.ModulesForKernel = nvidiaModulesOnDisk(kernel)
	}
}

// unitState returns the "systemctl is-active" answer for a unit ("active",
// "inactive", "failed", "activating", ...), or "" when systemctl is absent.
// systemctl exits non-zero for anything but active, so stdout is read either
// way.
func unitState(timeout int, unit string) string {
	if !util.CommandExists("systemctl") {
		return ""
	}
	r := util.RunCommand(timeout, "systemctl", "is-active", unit)
	return firstLineOfText(r.Stdout)
}

// collectDGXUnits reads the systemd states of the DGX OS units named by WP1
// item 6 (dgx-dashboard, dgx-dashboard-admin, fwupd, nvidia-persistenced).
//
// UnitsQueried is the integration contract for rule
// dgx-spark-dashboard-unhealthy: it is true only when systemctl answered with
// a state for at least one unit, so the *Active booleans are measurements.
// With systemctl absent or unable to talk to systemd (containers, the
// simulated run without a shim) it stays false and the booleans are unknown.
func collectDGXUnits(info *types.DGXOSInfo, timeout int) {
	dash := unitState(timeout, unitDashboard)
	admin := unitState(timeout, unitDashboardAdmin)
	fw := unitState(timeout, unitFwupd)
	pers := unitState(timeout, unitPersistenced)
	info.UnitsQueried = dash != "" || admin != "" || fw != "" || pers != ""
	info.DashboardActive = dash == "active"
	info.DashboardAdminActive = admin == "active"
	info.FwupdActive = fw == "active"
	if fw == "failed" {
		info.FwupdError = unitFwupd + " failed"
	}
	info.PersistencedActive = pers == "active"
}

// ProcNetTCPListening returns the local ports in LISTEN state (st 0A) from
// the contents of /proc/net/tcp or /proc/net/tcp6. The local address field
// is "hexip:hexport"; only the port is decoded.
func ProcNetTCPListening(content string) []int {
	var ports []int
	seen := map[int]bool{}
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] == "sl" || fields[3] != "0A" {
			continue
		}
		local := fields[1]
		colon := strings.LastIndexByte(local, ':')
		if colon < 0 {
			continue
		}
		port, err := strconv.ParseInt(local[colon+1:], 16, 32)
		if err != nil || seen[int(port)] {
			continue
		}
		seen[int(port)] = true
		ports = append(ports, int(port))
	}
	sort.Ints(ports)
	return ports
}

// ListeningTCPPorts reads /proc/net/tcp and /proc/net/tcp6 (through simPath)
// and returns the union of LISTEN ports. No socket is ever opened. It is
// exported for the ecosystem collector (inference ports, WP1 item 8).
func ListeningTCPPorts() []int {
	seen := map[int]bool{}
	var ports []int
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(simPath(f))
		if err != nil {
			continue
		}
		for _, p := range ProcNetTCPListening(string(data)) {
			if !seen[p] {
				seen[p] = true
				ports = append(ports, p)
			}
		}
	}
	sort.Ints(ports)
	return ports
}

// tcpPortListening reports whether a local TCP listener exists on port,
// judged from /proc/net/tcp{,6} only (no connect attempt).
func tcpPortListening(port int) bool {
	for _, p := range ListeningTCPPorts() {
		if p == port {
			return true
		}
	}
	return false
}

// fwupdMismatchRe matches the client/daemon version mismatch text
// "libfwupd version 1.9.34 does not match daemon 1.9.30" (spec section 6).
var fwupdMismatchRe = regexp.MustCompile(`libfwupd version \S+ does not match daemon \S+`)

// fwupdErrorFromOutput extracts the most useful error line from fwupdmgr
// output: the libfwupd/daemon mismatch when present, otherwise the first
// non-empty stderr line.
func fwupdErrorFromOutput(stdout, stderr string) string {
	if m := fwupdMismatchRe.FindString(stdout + "\n" + stderr); m != "" {
		return m
	}
	return firstLineOfText(strings.TrimSpace(stderr))
}

// fwupdmgrArgs keep fwupdmgr non-interactive and offline: no metadata
// refresh, no remote check, no prompts.
var fwupdmgrArgs = []string{"--no-unreported-check", "--no-metadata-check", "--no-remote-check", "--no-device-prompt"}

// fwupdOutputs caches each fwupdmgr sub-command result for the life of the
// process so CollectDGXOS (mismatch/error text) and CollectDGXHostState
// (device tree, pending capsules) run every query once. One CLI invocation is
// one pipeline, so the cache never goes stale in practice.
var fwupdOutputs = struct {
	sync.Mutex
	byCmd map[string]util.CommandResult
}{byCmd: map[string]util.CommandResult{}}

// runFwupdmgr runs "fwupdmgr <sub>" with the offline flags, memoised per
// sub-command.
func runFwupdmgr(timeout int, sub string) util.CommandResult {
	fwupdOutputs.Lock()
	defer fwupdOutputs.Unlock()
	if r, ok := fwupdOutputs.byCmd[sub]; ok {
		return r
	}
	r := util.RunCommand(timeout, "fwupdmgr", append([]string{sub}, fwupdmgrArgs...)...)
	fwupdOutputs.byCmd[sub] = r
	return r
}

func collectFwupdError(info *types.DGXOSInfo, timeout int) {
	if !util.CommandExists("fwupdmgr") {
		return
	}
	r := runFwupdmgr(timeout, "get-devices")
	if r.Err == nil && !fwupdMismatchRe.MatchString(r.Stdout+r.Stderr) {
		return
	}
	if e := fwupdErrorFromOutput(r.Stdout, r.Stderr); e != "" {
		info.FwupdError = e
	}
}

// aptSourceFirstLineOK reports whether the first non-empty line of an apt
// source file is well-formed: a one-line "deb"/"deb-src" stanza, a comment,
// or a deb822 header. Anything else (S44: a truncated or garbage first line
// in nvidia-container-toolkit.list) is treated as corrupt.
func aptSourceFirstLineOK(content string) (ok bool, first string) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "#"),
			strings.HasPrefix(line, "deb "),
			strings.HasPrefix(line, "deb\t"),
			strings.HasPrefix(line, "deb-src "),
			strings.HasPrefix(line, "Types:"),
			strings.HasPrefix(line, "Enabled:"),
			strings.HasPrefix(line, "X-Repolib"):
			return true, line
		default:
			return false, line
		}
	}
	return true, ""
}

// checkAptSource returns "<file>: <first line>" when the source's first line
// does not parse, or "" when the file is absent or fine.
func checkAptSource(path string) string {
	data, err := os.ReadFile(simPath(path))
	if err != nil {
		return ""
	}
	ok, first := aptSourceFirstLineOK(string(data))
	if ok {
		return ""
	}
	return filepath.Base(path) + ": " + util.TruncateString(first, 80)
}

// firstLineOfText returns the first line of s, trimmed. (xid.go has the same
// helper under a linux build tag; this untagged file needs its own copy.)
func firstLineOfText(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

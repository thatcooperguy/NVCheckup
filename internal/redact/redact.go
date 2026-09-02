// Package redact provides privacy-preserving redaction for NVCheckup reports
// and snapshots. Redaction is on by default; the tokens it emits are stable so
// that documentation and forum helpers can recognise them:
//
//	<user>                 the local username
//	<host>                 the machine hostname
//	<home>                 the user's home directory
//	<lan-ip>               a private/loopback IPv4 address (RFC 1918, 127/8, 0.0.0.0)
//	<public-ip-redacted>   any other IPv4 address
//	<email-redacted>       an email address
//	SSID: <redacted>       a WiFi network name
package redact

import (
	"net"
	"os"
	"os/user"
	"regexp"
	"runtime"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// minStandaloneUsernameLen is the shortest username we are willing to replace
// when it appears as a bare word (outside a path). Very short usernames such
// as "ai", "me" or "gpu" would otherwise corrupt unrelated words in the report
// ("AI / CUDA" -> "<user> / CUDA"). Paths containing the username are still
// redacted regardless of length.
const minStandaloneUsernameLen = 3

// ipv4Re matches dotted-quad IPv4 addresses. Replacement is done with a
// function so that LAN and public addresses get different tokens.
var ipv4Re = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`)

// Redactor holds patterns for redaction.
type Redactor struct {
	enabled  bool
	username string
	hostname string
	homeDir  string
	homeRe   *regexp.Regexp
	patterns []*replacementPattern
}

type replacementPattern struct {
	re          *regexp.Regexp
	replacement string
}

// New creates a Redactor seeded with the current OS user and hostname.
// If enabled is false, every method passes its input through unchanged.
func New(enabled bool) *Redactor {
	if !enabled {
		return &Redactor{enabled: false}
	}
	hostname, _ := os.Hostname()
	username, homeDir := "", ""
	if u, err := user.Current(); err == nil {
		username = u.Username
		homeDir = u.HomeDir
	}
	return NewWithIdentity(true, username, hostname, homeDir)
}

// NewWithIdentity creates a Redactor for a specific identity. It exists so
// tests can exercise the patterns deterministically without depending on the
// account running the test suite. A Windows "DOMAIN\user" username is reduced
// to its bare user part.
func NewWithIdentity(enabled bool, username, hostname, homeDir string) *Redactor {
	r := &Redactor{enabled: enabled}
	if !enabled {
		return r
	}
	if idx := strings.LastIndex(username, `\`); idx >= 0 {
		username = username[idx+1:]
	}
	r.username = strings.TrimSpace(username)
	r.hostname = strings.TrimSpace(hostname)
	r.homeDir = strings.TrimRight(strings.TrimSpace(homeDir), `\/`)
	r.buildPatterns()
	return r
}

// buildPatterns compiles the ordered replacement list. Order matters:
//  1. emails first, so a username that is also the local-part of an address
//     does not leave "<user>@example.com" behind;
//  2. the full home directory, so it becomes <home> rather than C:\Users\<user>;
//  3. hostname, which often embeds the username;
//  4. username inside other paths, then as a standalone word;
//  5. WiFi SSIDs.
//
// IPv4 addresses are handled separately in Redact via ReplaceAllStringFunc.
func (r *Redactor) buildPatterns() {
	// Windows file systems and account names are case-insensitive.
	flags := ""
	if runtime.GOOS == "windows" {
		flags = "(?i)"
	}

	r.patterns = append(r.patterns, &replacementPattern{
		re:          regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
		replacement: "<email-redacted>",
	})

	if r.homeDir != "" {
		// Accept either slash style so "C:/Users/x" from Python output also matches.
		r.homeRe = regexp.MustCompile(`(?i)` + eitherSlashPattern(r.homeDir))
		r.patterns = append(r.patterns, &replacementPattern{
			re:          r.homeRe,
			replacement: "<home>",
		})
	}

	// Hostname before username: hostnames such as "ALICE-DESKTOP" frequently
	// embed the username, and "<user>-DESKTOP" would still identify the machine.
	if r.hostname != "" {
		r.patterns = append(r.patterns, &replacementPattern{
			re:          regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(r.hostname) + `\b`),
			replacement: "<host>",
		})
	}

	if r.username != "" {
		quoted := regexp.QuoteMeta(r.username)
		r.patterns = append(r.patterns, &replacementPattern{
			re:          regexp.MustCompile(flags + `([A-Za-z]:[\\/]Users[\\/]|/home/|/Users/)` + quoted + `\b`),
			replacement: "${1}<user>",
		})
		if len(r.username) >= minStandaloneUsernameLen {
			r.patterns = append(r.patterns, &replacementPattern{
				re:          regexp.MustCompile(flags + `\b` + quoted + `\b`),
				replacement: "<user>",
			})
		}
	}

	// WiFi network names as printed by netsh / iwconfig / nmcli.
	r.patterns = append(r.patterns, &replacementPattern{
		re:          regexp.MustCompile(`(?i)\bE?SSID\s*[:=]\s*"?([^"\r\n]+)"?`),
		replacement: "SSID: <redacted>",
	})
}

// eitherSlashPattern quotes a path for use in a regexp, letting each path
// separator match either "\" or "/".
func eitherSlashPattern(p string) string {
	const sep = `[\\/]`
	var sb strings.Builder
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		// Keep the leading separator of absolute Unix paths so "/home/x"
		// becomes "<home>" rather than "/<home>".
		sb.WriteString(sep)
	}
	for i, part := range strings.FieldsFunc(p, func(c rune) bool { return c == '\\' || c == '/' }) {
		if i > 0 {
			sb.WriteString(sep)
		}
		sb.WriteString(regexp.QuoteMeta(part))
	}
	return sb.String()
}

// Redact applies all redaction patterns to the input string.
func (r *Redactor) Redact(s string) string {
	if !r.enabled || s == "" {
		return s
	}
	result := s
	for _, p := range r.patterns {
		result = p.re.ReplaceAllString(result, p.replacement)
	}
	return ipv4Re.ReplaceAllStringFunc(result, r.RedactIP)
}

// RedactIP replaces a single IP address, labelling private/loopback ranges as
// <lan-ip> so a reader can still tell a home-router hop from an ISP hop.
// Non-IP input is returned unchanged.
func (r *Redactor) RedactIP(ip string) string {
	if !r.enabled {
		return ip
	}
	parsed := net.ParseIP(strings.TrimSpace(ip))
	if parsed == nil {
		return ip
	}
	if isPrivateIP(parsed) {
		return "<lan-ip>"
	}
	return "<public-ip-redacted>"
}

// isPrivateIP reports whether ip is in a private, loopback, link-local or
// unspecified range. 0.0.0.0 counts as LAN because it only ever appears as a
// bind address, never as an identifying public endpoint.
func isPrivateIP(ip net.IP) bool {
	if ip.IsUnspecified() {
		return true
	}
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16",
		"::1/128",
		"fe80::/10",
	}
	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// RedactPath redacts the home directory and username from a file path.
func (r *Redactor) RedactPath(path string) string {
	if !r.enabled || path == "" {
		return path
	}
	result := path
	if r.homeRe != nil {
		result = r.homeRe.ReplaceAllLiteralString(result, "<home>")
	}
	if r.username != "" {
		result = strings.ReplaceAll(result, `\`+r.username+`\`, `\<user>\`)
		result = strings.ReplaceAll(result, `/`+r.username+`/`, `/<user>/`)
	}
	return result
}

// RedactHostname replaces the machine hostname.
func (r *Redactor) RedactHostname(s string) string {
	if !r.enabled || r.hostname == "" {
		return s
	}
	return strings.ReplaceAll(s, r.hostname, "<host>")
}

// Summary returns a human-readable summary of what will be redacted.
func (r *Redactor) Summary() string {
	if !r.enabled {
		return "Redaction is DISABLED. Report may contain personally identifiable information."
	}
	return `Redaction is ENABLED. The following are automatically redacted:
  - Machine hostname -> <host>
  - Local username -> <user>
  - Home directory paths -> <home>
  - Public IP addresses -> <public-ip-redacted>
  - LAN/loopback IP addresses -> <lan-ip>
  - Email addresses -> <email-redacted>
  - WiFi SSIDs -> SSID: <redacted>
Use --no-redact to disable redaction (not recommended for public sharing).`
}

// ApplyToReport redacts every free-text and path field of a Report in place.
// It is a no-op when the redactor is disabled.
func ApplyToReport(r *types.Report, red *Redactor) {
	if r == nil || red == nil || !red.enabled {
		return
	}
	r.System.Hostname = red.RedactHostname(r.System.Hostname)
	r.SummaryBlock = red.Redact(r.SummaryBlock)

	for i := range r.GPUs {
		r.GPUs[i].PCIBusID = red.Redact(r.GPUs[i].PCIBusID)
	}

	r.Driver.NvidiaSmiOutput = red.Redact(r.Driver.NvidiaSmiOutput)
	r.Driver.NvidiaSmiPath = red.RedactPath(r.Driver.NvidiaSmiPath)

	for i := range r.Findings {
		r.Findings[i].Evidence = red.Redact(r.Findings[i].Evidence)
	}
	for i := range r.TopIssues {
		r.TopIssues[i] = red.Redact(r.TopIssues[i])
	}
	for i := range r.NextSteps {
		r.NextSteps[i] = red.Redact(r.NextSteps[i])
	}
	for i := range r.CollectorErrors {
		r.CollectorErrors[i].Error = red.Redact(r.CollectorErrors[i].Error)
	}

	if r.Windows != nil {
		redactWindows(r.Windows, red)
	}
	if r.Linux != nil {
		redactLinux(r.Linux, red)
	}
	if r.AI != nil {
		redactAI(r.AI, red)
	}

	if r.Network != nil {
		r.Network.InterfaceName = red.Redact(r.Network.InterfaceName)
		for i := range r.Network.Hops {
			r.Network.Hops[i].Address = red.RedactIP(r.Network.Hops[i].Address)
		}
	}
}

// ApplyToSnapshot redacts the identifying fields of a Snapshot in place.
// It is a no-op when the redactor is disabled.
func ApplyToSnapshot(s *types.Snapshot, red *Redactor) {
	if s == nil || red == nil || !red.enabled {
		return
	}
	s.System.Hostname = red.RedactHostname(s.System.Hostname)
	s.Driver.NvidiaSmiOutput = red.Redact(s.Driver.NvidiaSmiOutput)
	s.Driver.NvidiaSmiPath = red.RedactPath(s.Driver.NvidiaSmiPath)
	for i := range s.GPUs {
		s.GPUs[i].PCIBusID = red.Redact(s.GPUs[i].PCIBusID)
	}
	if s.Windows != nil {
		redactWindows(s.Windows, red)
	}
	if s.Linux != nil {
		redactLinux(s.Linux, red)
	}
	if s.AI != nil {
		redactAI(s.AI, red)
	}
}

// redactWindows scrubs monitor names (which can embed serial-like ids) and
// event-log messages (which frequently embed user profile paths).
func redactWindows(w *types.WindowsInfo, red *Redactor) {
	for i := range w.Monitors {
		w.Monitors[i].Name = red.Redact(w.Monitors[i].Name)
	}
	redactEvents(w.DriverResetEvents, red)
	redactEvents(w.NvlddmkmErrors, red)
	redactEvents(w.WHEAErrors, red)
	w.DxDiagSummary = red.Redact(w.DxDiagSummary)
}

func redactEvents(events []types.EventLogEntry, red *Redactor) {
	for i := range events {
		events[i].Message = red.Redact(events[i].Message)
	}
}

func redactLinux(l *types.LinuxInfo, red *Redactor) {
	l.LibCudaPath = red.RedactPath(l.LibCudaPath)
	l.JournalSnippets = red.Redact(l.JournalSnippets)
	l.DmesgSnippets = red.Redact(l.DmesgSnippets)
	l.DKMSErrors = red.Redact(l.DKMSErrors)
	l.GLRenderer = red.Redact(l.GLRenderer)
	for i := range l.XidErrors {
		l.XidErrors[i].Message = red.Redact(l.XidErrors[i].Message)
	}
}

func redactAI(ai *types.AIInfo, red *Redactor) {
	ai.NvccPath = red.RedactPath(ai.NvccPath)
	for i := range ai.PythonVersions {
		ai.PythonVersions[i].Path = red.RedactPath(ai.PythonVersions[i].Path)
	}
	if ai.PyTorchInfo != nil {
		ai.PyTorchInfo.Error = red.Redact(ai.PyTorchInfo.Error)
	}
	if ai.TensorFlowInfo != nil {
		ai.TensorFlowInfo.Error = red.Redact(ai.TensorFlowInfo.Error)
	}
}

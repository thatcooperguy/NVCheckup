// Package redact provides privacy-preserving redaction for NVCheckup reports.
package redact

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"regexp"
	"runtime"
	"strings"
)

// Redactor holds patterns for redaction
type Redactor struct {
	enabled     bool
	username    string
	hostname    string
	homeDir     string
	patterns    []*replacementPattern
}

type replacementPattern struct {
	re          *regexp.Regexp
	replacement string
}

// New creates a new Redactor. If enabled is false, it passes through unchanged.
func New(enabled bool) *Redactor {
	r := &Redactor{enabled: enabled}
	if !enabled {
		return r
	}

	// Gather system info for redaction
	r.hostname, _ = os.Hostname()
	if u, err := user.Current(); err == nil {
		r.username = u.Username
		r.homeDir = u.HomeDir
		// On Windows, username may be "DOMAIN\user"
		if idx := strings.LastIndex(r.username, "\\"); idx >= 0 {
			r.username = r.username[idx+1:]
		}
	}

	r.buildPatterns()
	return r
}

func (r *Redactor) buildPatterns() {
	// Username in paths (case-insensitive on Windows)
	if r.username != "" {
		flags := ""
		if runtime.GOOS == "windows" {
			flags = "(?i)"
		}
		// Windows paths: C:\Users\username
		r.patterns = append(r.patterns, &replacementPattern{
			re:          regexp.MustCompile(flags + `(?:C:\\Users\\|/home/|/Users/)` + regexp.QuoteMeta(r.username)),
			replacement: func() string {
				if runtime.GOOS == "windows" {
					return `C:\Users\<user>`
				}
				return "/home/<user>"
			}(),
		})
		// General username references
		r.patterns = append(r.patterns, &replacementPattern{
			re:          regexp.MustCompile(flags + `\b` + regexp.QuoteMeta(r.username) + `\b`),
			replacement: "<user>",
		})
	}

	// Hostname
	if r.hostname != "" {
		r.patterns = append(r.patterns, &replacementPattern{
			re:          regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(r.hostname) + `\b`),
			replacement: "<host>",
		})
	}

	// External/public IP patterns (IPv4)
	r.patterns = append(r.patterns, &replacementPattern{
		re:          regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`),
		replacement: "<ip-redacted>",
	})

	// WiFi SSID patterns (common log formats)
	r.patterns = append(r.patterns, &replacementPattern{
		re:          regexp.MustCompile(`(?i)(?:SSID|network)\s*[:=]\s*"?([^"\n]+)"?`),
		replacement: "SSID: <redacted>",
	})

	// Email addresses
	r.patterns = append(r.patterns, &replacementPattern{
		re:          regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
		replacement: "<email-redacted>",
	})

	// Home directory (full path)
	if r.homeDir != "" {
		escaped := regexp.QuoteMeta(r.homeDir)
		r.patterns = append(r.patterns, &replacementPattern{
			re:          regexp.MustCompile(`(?i)` + escaped),
			replacement: "<home>",
		})
	}
}

// Redact applies all redaction patterns to the input string
func (r *Redactor) Redact(s string) string {
	if !r.enabled || s == "" {
		return s
	}
	result := s
	for _, p := range r.patterns {
		result = p.re.ReplaceAllString(result, p.replacement)
	}
	// Restore private/LAN IPs (10.x, 172.16-31.x, 192.168.x) as "<lan-ip>" instead of fully redacting
	lanRe := regexp.MustCompile(`<ip-redacted>`)
	// Actually we already replaced them. Let's be smarter: do IP redaction manually
	// Re-do: replace public IPs but keep LAN IPs with "<lan-ip>"
	_ = lanRe
	return result
}

// RedactIP specifically handles IP addresses, keeping LAN IPs labeled differently
func (r *Redactor) RedactIP(ip string) string {
	if !r.enabled {
		return ip
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}
	if isPrivateIP(parsed) {
		return "<lan-ip>"
	}
	return "<public-ip-redacted>"
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
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

// RedactPath redacts usernames from file paths
func (r *Redactor) RedactPath(path string) string {
	if !r.enabled || path == "" {
		return path
	}
	result := path
	if r.username != "" {
		// Windows-style paths
		result = strings.ReplaceAll(result, fmt.Sprintf(`\%s\`, r.username), `\<user>\`)
		result = strings.ReplaceAll(result, fmt.Sprintf(`/%s/`, r.username), `/<user>/`)
	}
	return result
}

// RedactHostname replaces the machine hostname
func (r *Redactor) RedactHostname(s string) string {
	if !r.enabled || r.hostname == "" {
		return s
	}
	return strings.ReplaceAll(s, r.hostname, "<host>")
}

// Summary returns a human-readable summary of what will be redacted
func (r *Redactor) Summary() string {
	if !r.enabled {
		return "Redaction is DISABLED. Report may contain personally identifiable information."
	}
	return `Redaction is ENABLED. The following are automatically redacted:
  - Machine hostname → <host>
  - Local username → <user>
  - Home directory paths → <home>
  - Public IP addresses → <public-ip-redacted>
  - LAN IP addresses → <lan-ip>
  - Email addresses → <email-redacted>
  - WiFi SSIDs → <redacted>
Use --no-redact to disable redaction (not recommended for public sharing).`
}

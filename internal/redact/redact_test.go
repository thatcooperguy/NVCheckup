package redact

import (
	"net"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

func TestRedactorDisabled(t *testing.T) {
	r := New(false)
	input := "Hello from myhost, user admin at /home/admin 8.8.8.8"
	got := r.Redact(input)
	if got != input {
		t.Errorf("disabled redactor should pass through, got %q", got)
	}
	if r.RedactIP("8.8.8.8") != "8.8.8.8" {
		t.Error("disabled redactor should not touch IPs")
	}
}

func TestRedactIP(t *testing.T) {
	r := NewWithIdentity(true, "", "", "")
	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.1.1", "<lan-ip>"},
		{"10.0.0.1", "<lan-ip>"},
		{"172.16.0.1", "<lan-ip>"},
		{"127.0.0.1", "<lan-ip>"},
		{"0.0.0.0", "<lan-ip>"},
		{"8.8.8.8", "<public-ip-redacted>"},
		{"1.2.3.4", "<public-ip-redacted>"},
		{"not-an-ip", "not-an-ip"},
	}
	for _, tt := range tests {
		got := r.RedactIP(tt.ip)
		if got != tt.want {
			t.Errorf("RedactIP(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"192.168.1.1", true},
		{"10.255.0.1", true},
		{"172.16.5.5", true},
		{"172.32.0.1", false},
		{"8.8.8.8", false},
		{"127.0.0.1", true},
		{"0.0.0.0", true},
		{"169.254.10.1", true},
	}
	for _, tt := range tests {
		parsed := net.ParseIP(tt.ip)
		if parsed == nil {
			t.Fatalf("bad test IP: %s", tt.ip)
		}
		got := isPrivateIP(parsed)
		if got != tt.private {
			t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestRedactPath(t *testing.T) {
	r := NewWithIdentity(true, "admin", "", "")
	tests := []struct {
		path string
		want string
	}{
		{`C:\Users\admin\Documents`, `C:\Users\<user>\Documents`},
		{"/home/admin/code", "/home/<user>/code"},
		{"/var/log/syslog", "/var/log/syslog"},
	}
	for _, tt := range tests {
		got := r.RedactPath(tt.path)
		if got != tt.want {
			t.Errorf("RedactPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}

	// With a home directory configured the whole prefix collapses to <home>.
	r2 := NewWithIdentity(true, "admin", "", `C:\Users\admin`)
	if got := r2.RedactPath(`C:\Users\admin\AppData\Local\nvidia-smi.exe`); got != `<home>\AppData\Local\nvidia-smi.exe` {
		t.Errorf("RedactPath with home = %q", got)
	}
}

func TestRedact_EndToEnd(t *testing.T) {
	r := NewWithIdentity(true, `CORP\alice`, "ALICE-DESKTOP", `C:\Users\alice`)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"windows home path", `Python at C:\Users\alice\AppData\Local\python.exe`, `Python at <home>\AppData\Local\python.exe`},
		{"forward-slash home path", `C:/Users/alice/miniconda3`, `<home>/miniconda3`},
		{"hostname", "Computer name: ALICE-DESKTOP", "Computer name: <host>"},
		{"hostname case-insensitive", "host alice-desktop reachable", "host <host> reachable"},
		{"lan ip", "Gateway 192.168.1.1", "Gateway <lan-ip>"},
		{"public ip", "DNS 8.8.8.8", "DNS <public-ip-redacted>"},
		{"loopback", "bound to 127.0.0.1", "bound to <lan-ip>"},
		{"email", "Contact alice@example.com", "Contact <email-redacted>"},
		{"ssid", "SSID: MyNet", "SSID: <redacted>"},
		{"ssid netsh style", `    SSID                   : MyNet 5G`, `    SSID: <redacted>`},
		{"standalone username", "logged in as alice", "logged in as <user>"},
		{"other user path", "/home/alice/.bashrc", "/home/<user>/.bashrc"},
		{"mixed", "alice@ALICE-DESKTOP 10.0.0.5 -> 1.1.1.1", "<user>@<host> <lan-ip> -> <public-ip-redacted>"},
	}
	for _, tt := range tests {
		got := r.Redact(tt.in)
		if got != tt.want {
			t.Errorf("%s: Redact(%q)\n got  %q\n want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestRedact_ShortUsernameDoesNotCorruptWords(t *testing.T) {
	// A two-letter username must not be replaced as a bare word, otherwise
	// "AI / CUDA" would become "<user> / CUDA". Paths are still redacted.
	r := NewWithIdentity(true, "ai", "", "")
	if got := r.Redact("== AI / CUDA ENVIRONMENT =="); got != "== AI / CUDA ENVIRONMENT ==" {
		t.Errorf("short username corrupted text: %q", got)
	}
	if got := r.Redact("/home/ai/project"); got != "/home/<user>/project" {
		t.Errorf("short username path not redacted: %q", got)
	}
}

func TestRedact_UsernameWordBoundary(t *testing.T) {
	r := NewWithIdentity(true, "bob", "", "")
	if got := r.Redact("bobcat bobby bob"); got != "bobcat bobby <user>" {
		t.Errorf("word boundary not respected: %q", got)
	}
}

func TestRedact_VersionStringsSurvive(t *testing.T) {
	// Version numbers look like IPs only when they have exactly four octets;
	// a driver version such as 591.86 or CUDA 12.4 must not be touched.
	r := NewWithIdentity(true, "", "", "")
	in := "Driver 591.86 CUDA 12.4 Python 3.11.9"
	if got := r.Redact(in); got != in {
		t.Errorf("version strings altered: %q", got)
	}
}

func TestApplyToReport(t *testing.T) {
	r := NewWithIdentity(true, "alice", "ALICE-PC", `C:\Users\alice`)
	rep := &types.Report{
		System:       types.SystemInfo{Hostname: "ALICE-PC"},
		SummaryBlock: "Host ALICE-PC user alice",
		Driver:       types.DriverInfo{NvidiaSmiPath: `C:\Users\alice\bin\nvidia-smi.exe`},
		Windows: &types.WindowsInfo{
			Monitors:          []types.MonitorInfo{{Name: "ALICE-PC display"}},
			DriverResetEvents: []types.EventLogEntry{{Message: `Fault in C:\Users\alice\game.exe`}},
		},
		Network: &types.NetworkInfo{
			InterfaceName: "alice-wifi",
			Hops:          []types.HopInfo{{Address: "192.168.0.1"}, {Address: "8.8.4.4"}},
		},
		Findings:        []types.Finding{{Evidence: "seen on ALICE-PC"}},
		CollectorErrors: []types.CollectorError{{Error: "cannot read /home/alice/x"}},
	}
	ApplyToReport(rep, r)

	checks := map[string]string{
		"hostname":       rep.System.Hostname,
		"summary":        rep.SummaryBlock,
		"smi path":       rep.Driver.NvidiaSmiPath,
		"monitor":        rep.Windows.Monitors[0].Name,
		"event message":  rep.Windows.DriverResetEvents[0].Message,
		"interface":      rep.Network.InterfaceName,
		"evidence":       rep.Findings[0].Evidence,
		"collector note": rep.CollectorErrors[0].Error,
	}
	for name, v := range checks {
		if strings.Contains(v, "alice") || strings.Contains(v, "ALICE-PC") {
			t.Errorf("%s not redacted: %q", name, v)
		}
	}
	if rep.Network.Hops[0].Address != "<lan-ip>" || rep.Network.Hops[1].Address != "<public-ip-redacted>" {
		t.Errorf("hops not redacted: %+v", rep.Network.Hops)
	}
	if rep.Windows.DriverResetEvents[0].Message != `Fault in <home>\game.exe` {
		t.Errorf("event message = %q", rep.Windows.DriverResetEvents[0].Message)
	}
}

func TestApplyToSnapshot(t *testing.T) {
	r := NewWithIdentity(true, "alice", "ALICE-PC", "/home/alice")
	snap := &types.Snapshot{
		System: types.SystemInfo{Hostname: "ALICE-PC"},
		AI: &types.AIInfo{
			NvccPath:       "/home/alice/cuda/bin/nvcc",
			PythonVersions: []types.PythonEnv{{Path: "/home/alice/venv/bin/python"}},
		},
		Linux: &types.LinuxInfo{LibCudaPath: "/home/alice/lib/libcuda.so"},
	}
	ApplyToSnapshot(snap, r)
	if snap.System.Hostname != "<host>" {
		t.Errorf("hostname = %q", snap.System.Hostname)
	}
	if snap.AI.NvccPath != "<home>/cuda/bin/nvcc" {
		t.Errorf("nvcc path = %q", snap.AI.NvccPath)
	}
	if snap.AI.PythonVersions[0].Path != "<home>/venv/bin/python" {
		t.Errorf("python path = %q", snap.AI.PythonVersions[0].Path)
	}
	if snap.Linux.LibCudaPath != "<home>/lib/libcuda.so" {
		t.Errorf("libcuda path = %q", snap.Linux.LibCudaPath)
	}

	// Disabled redactor must leave everything alone.
	snap2 := &types.Snapshot{System: types.SystemInfo{Hostname: "ALICE-PC"}}
	ApplyToSnapshot(snap2, New(false))
	if snap2.System.Hostname != "ALICE-PC" {
		t.Error("disabled redactor modified snapshot")
	}
}

func TestSummary(t *testing.T) {
	r := New(true)
	s := r.Summary()
	for _, tok := range []string{"<host>", "<user>", "<home>", "<public-ip-redacted>", "<lan-ip>", "<email-redacted>", "<redacted>"} {
		if !strings.Contains(s, tok) {
			t.Errorf("summary missing token %s", tok)
		}
	}
	if New(false).Summary() == "" {
		t.Error("summary should not be empty when disabled")
	}
}

func TestRedact_FourPartVersionsAreNotIPs(t *testing.T) {
	// Regression: "NVIDIA App version 11.0.7.247 is installed." was rendered
	// as "NVIDIA App version <public-ip-redacted> is installed." because a
	// four-part version number looks exactly like a dotted quad.
	r := NewWithIdentity(true, "", "", "")
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"version word", "NVIDIA App version 11.0.7.247 is installed", "NVIDIA App version 11.0.7.247 is installed"},
		{"glued v", "GeForce v11.0.7.247", "GeForce v11.0.7.247"},
		{"upper V", "Xbox Game Bar (V7.326.8061.0)", "Xbox Game Bar (V7.326.8061.0)"},
		{"driver word", "Intel driver 32.0.101.6078", "Intel driver 32.0.101.6078"},
		{"build word", "Windows build 10.0.26200.1234", "Windows build 10.0.26200.1234"},
		{"release word", "release 2.0.1.5", "release 2.0.1.5"},
		{"ver dot", "ver. 1.2.3.4", "ver. 1.2.3.4"},
		{"five part", "package 1.2.3.4.5 installed", "package 1.2.3.4.5 installed"},
		{"ping", "ping 8.8.8.8", "ping <public-ip-redacted>"},
		{"gateway", "gateway 192.168.1.1", "gateway <lan-ip>"},
		{"hop", "hop 1.1.1.1 12ms", "hop <public-ip-redacted> 12ms"},
		{"sentence end", "resolver is 8.8.4.4.", "resolver is <public-ip-redacted>."},
		{"two ips", "10.0.0.5 -> 1.1.1.1", "<lan-ip> -> <public-ip-redacted>"},
		{"version then ip", "version 1.2.3.4 at 8.8.8.8", "version 1.2.3.4 at <public-ip-redacted>"},
	}
	for _, tt := range tests {
		if got := r.Redact(tt.in); got != tt.want {
			t.Errorf("%s: Redact(%q)\n got  %q\n want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestRedact_HomeDirDoesNotMatchSiblingProfile(t *testing.T) {
	// C:\Users\alice must not turn C:\Users\alice2\... into <home>2\...
	r := NewWithIdentity(true, "alice", "", `C:\Users\alice`)
	tests := []struct {
		in   string
		want string
	}{
		{`C:\Users\alice2\AppData\python.exe`, `C:\Users\alice2\AppData\python.exe`},
		{`C:/Users/alice2/miniconda3`, `C:/Users/alice2/miniconda3`},
		{`C:\Users\alice\AppData\python.exe`, `<home>\AppData\python.exe`},
		{`C:/Users/alice/miniconda3`, `<home>/miniconda3`},
		{`C:\Users\alice`, `<home>`},
		{`path "C:\Users\alice" quoted`, `path "<home>" quoted`},
		{`home is C:\Users\alice, ok`, `home is <home>, ok`},
	}
	for _, tt := range tests {
		if got := r.Redact(tt.in); got != tt.want {
			t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if got := r.RedactPath(tt.in); got != tt.want {
			t.Errorf("RedactPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	// Unix home directories behave the same way.
	u := NewWithIdentity(true, "bob", "", "/home/bob")
	if got := u.RedactPath("/home/bobby/.bashrc"); got != "/home/bobby/.bashrc" {
		t.Errorf("sibling unix profile altered: %q", got)
	}
	if got := u.RedactPath("/home/bob/.bashrc"); got != "<home>/.bashrc" {
		t.Errorf("unix home not redacted: %q", got)
	}
}

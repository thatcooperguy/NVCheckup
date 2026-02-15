package redact

import (
	"net"
	"testing"
)

func TestRedactorDisabled(t *testing.T) {
	r := New(false)
	input := "Hello from myhost, user admin at /home/admin"
	got := r.Redact(input)
	if got != input {
		t.Errorf("disabled redactor should pass through, got %q", got)
	}
}

func TestRedactIP(t *testing.T) {
	r := &Redactor{enabled: true}
	tests := []struct {
		ip   string
		want string
	}{
		{"192.168.1.1", "<lan-ip>"},
		{"10.0.0.1", "<lan-ip>"},
		{"172.16.0.1", "<lan-ip>"},
		{"127.0.0.1", "<lan-ip>"},
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
	r := &Redactor{enabled: true, username: "admin"}
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
}

func TestSummary(t *testing.T) {
	r := New(true)
	s := r.Summary()
	if s == "" {
		t.Error("summary should not be empty when enabled")
	}

	r2 := New(false)
	s2 := r2.Summary()
	if s2 == "" {
		t.Error("summary should not be empty when disabled")
	}
}

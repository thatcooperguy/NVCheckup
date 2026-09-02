package wsl

import "testing"

func TestWSLVersionFromProcVersion(t *testing.T) {
	cases := []struct {
		name, version, want string
	}{
		{"wsl2 kernel", "Linux version 5.15.153.1-microsoft-standard-WSL2 (root@...) #1 SMP", "2"},
		{"wsl2 older tag", "Linux version 4.19.128-microsoft-standard (oe-user@oe-host) #1 SMP", "2"},
		{"wsl2 custom kernel", "Linux version 6.6.36.3-WSL2-custom (build@host)", "2"},
		{"wsl1", "Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com) (gcc version 5.4.0)", "1"},
		{"plain linux", "Linux version 6.8.0-40-generic (buildd@lcy02-amd64-078)", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		if got := wslVersionFromProcVersion(c.version); got != c.want {
			t.Errorf("%s: wslVersionFromProcVersion(%q) = %q, want %q", c.name, c.version, got, c.want)
		}
	}
}

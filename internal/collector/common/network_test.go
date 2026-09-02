package common

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestParsePingTimesWindows(t *testing.T) {
	output := `Pinging 1.1.1.1 with 32 bytes of data:
Reply from 1.1.1.1: bytes=32 time=12ms TTL=57
Reply from 1.1.1.1: bytes=32 time=14ms TTL=57
Reply from 1.1.1.1: bytes=32 time<1ms TTL=57
Reply from 1.1.1.1: bytes=32 time=11ms TTL=57

Ping statistics for 1.1.1.1:
    Packets: Sent = 4, Received = 4, Lost = 0 (0% loss),`

	rtts := parsePingTimesWindows(output)
	if len(rtts) != 4 {
		t.Fatalf("Expected 4 RTTs, got %d", len(rtts))
	}
	if rtts[0] != 12 {
		t.Errorf("Expected first RTT=12, got %f", rtts[0])
	}
	if rtts[2] != 1 {
		t.Errorf("Expected third RTT=1 (from <1ms), got %f", rtts[2])
	}
}

func TestParsePingTimesLinux(t *testing.T) {
	output := `PING 1.1.1.1 (1.1.1.1) 56(84) bytes of data.
64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=12.3 ms
64 bytes from 1.1.1.1: icmp_seq=2 ttl=57 time=11.8 ms
64 bytes from 1.1.1.1: icmp_seq=3 ttl=57 time=13.1 ms

--- 1.1.1.1 ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2003ms`

	rtts := parsePingTimesLinux(output)
	if len(rtts) != 3 {
		t.Fatalf("Expected 3 RTTs, got %d", len(rtts))
	}
	if rtts[0] != 12.3 {
		t.Errorf("Expected first RTT=12.3, got %f", rtts[0])
	}
}

func TestParsePingLoss(t *testing.T) {
	tests := []struct {
		output   string
		expected float64
	}{
		{"Packets: Sent = 10, Received = 10, Lost = 0 (0% loss)", 0},
		{"Packets: Sent = 10, Received = 9, Lost = 1 (10% loss)", 10},
		{"3 packets transmitted, 3 received, 0% packet loss, time 2003ms", 0},
		{"10 packets transmitted, 8 received, 20% packet loss, time 9012ms", 20},
	}

	for _, tt := range tests {
		result := parsePingLoss(tt.output)
		if result != tt.expected {
			t.Errorf("parsePingLoss(%q) = %f, want %f", tt.output[:30], result, tt.expected)
		}
	}
}

func TestParseTracerouteWindows(t *testing.T) {
	output := `Tracing route to 1.1.1.1 over a maximum of 15 hops

  1    <1 ms    <1 ms    <1 ms  192.168.1.1
  2     *        *        *     Request timed out.
  3    12 ms    11 ms    12 ms  10.0.0.1`

	hops := parseTracerouteWindows(output)
	if len(hops) != 3 {
		t.Fatalf("Expected 3 hops, got %d", len(hops))
	}

	if hops[0].Number != 1 {
		t.Errorf("First hop number = %d, want 1", hops[0].Number)
	}
	if hops[0].Address != "192.168.1.1" {
		t.Errorf("First hop address = %s, want 192.168.1.1", hops[0].Address)
	}

	if !hops[1].Loss {
		t.Error("Second hop should be marked as loss")
	}

	if hops[2].Address != "10.0.0.1" {
		t.Errorf("Third hop address = %s, want 10.0.0.1", hops[2].Address)
	}
}

func TestParseTracerouteLinux(t *testing.T) {
	output := ` 1  192.168.1.1  0.543 ms  0.432 ms  0.389 ms
 2  * * *
 3  10.0.0.1  12.345 ms  11.234 ms  12.567 ms`

	hops := parseTracerouteLinux(output)
	if len(hops) != 3 {
		t.Fatalf("Expected 3 hops, got %d", len(hops))
	}

	if hops[0].Number != 1 {
		t.Errorf("First hop number = %d, want 1", hops[0].Number)
	}

	if !hops[1].Loss {
		t.Error("Second hop should be marked as loss")
	}

	if hops[2].Address != "10.0.0.1" {
		t.Errorf("Third hop address = %s, want 10.0.0.1", hops[2].Address)
	}
}

// Captured on a docked laptop: the Wi-Fi adapter exists but is disconnected.
// The old strings.Contains(output, "connected") check matched "disconnected".
const wlanDisconnectedSample = `
There is 1 interface on the system: 

    Name                   : Wi-Fi
    Description            : Intel(R) Wireless-AC 9260 160MHz
    GUID                   : 4febfb8d-d367-49db-9722-e20c96bdd9e6
    Physical address       : 04:56:e5:e1:79:18
    Interface type         : Primary
    State                  : disconnected
    Radio status           : Hardware On
                             Software Off
`

const wlanConnectedSample = `
There is 1 interface on the system: 

    Name                   : Wi-Fi
    Description            : Intel(R) Wi-Fi 6 AX201 160MHz
    GUID                   : 11111111-2222-3333-4444-555555555555
    Physical address       : aa:bb:cc:dd:ee:ff
    Interface type         : Primary
    State                  : connected
    SSID                   : HomeNet
    BSSID                  : 00:11:22:33:44:55
    Network type           : Infrastructure
    Radio type             : 802.11ax
    Authentication         : WPA2-Personal
    Cipher                 : CCMP
    Connection mode        : Auto Connect
    Band                   : 5 GHz
    Channel                : 44
    Receive rate (Mbps)    : 1201
    Transmit rate (Mbps)   : 1201
    Signal                 : 85%
    Profile                : HomeNet
`

func TestParseWlanInterfaces_Disconnected(t *testing.T) {
	ifaces := parseWlanInterfaces(wlanDisconnectedSample)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	if ifaces[0].Name != "Wi-Fi" || ifaces[0].State != "disconnected" {
		t.Errorf("unexpected parse: %+v", ifaces[0])
	}
	if _, ok := activeWlan(ifaces, "Wi-Fi"); ok {
		t.Error("disconnected wlan adapter must not be classified as active wifi")
	}
	if _, ok := activeWlan(ifaces, "Ethernet 7"); ok {
		t.Error("ethernet adapter must not be classified as wifi")
	}
}

func TestParseWlanInterfaces_Connected(t *testing.T) {
	ifaces := parseWlanInterfaces(wlanConnectedSample)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	w, ok := activeWlan(ifaces, "Wi-Fi")
	if !ok {
		t.Fatal("connected Wi-Fi that owns the default route should be wifi")
	}
	if w.RadioType != "802.11ax" || !w.HasSignal || w.SignalPct != 85 {
		t.Errorf("unexpected wlan details: %+v", w)
	}
	// Connected but not the default-route adapter (docked laptop): not wifi.
	if _, ok := activeWlan(ifaces, "Ethernet 7"); ok {
		t.Error("wifi must only be reported when its Name equals the active interface")
	}
	if _, ok := activeWlan(ifaces, ""); ok {
		t.Error("unknown active interface must not be classified as wifi")
	}
}

func TestParseNetshConnectedInterface(t *testing.T) {
	out := `
Admin State    State          Type             Interface Name
-------------------------------------------------------------------------
Enabled        Disconnected   Dedicated        Ethernet 9
Disabled       Disconnected   Dedicated        Ethernet 2
Enabled        Connected      Dedicated        Ethernet 7
Enabled        Connected      Dedicated        Tailscale
`
	if got := parseNetshConnectedInterface(out); got != "Ethernet 7" {
		t.Errorf("parseNetshConnectedInterface = %q, want %q", got, "Ethernet 7")
	}
	if got := parseNetshConnectedInterface("Admin State    State          Type             Interface Name\nEnabled        Disconnected   Dedicated        Wi-Fi\n"); got != "" {
		t.Errorf("disconnected-only table returned %q, want empty", got)
	}
}

func TestParseDefaultRouteDev(t *testing.T) {
	out := "default via 192.168.1.1 dev wlp3s0 proto dhcp metric 600 \ndefault via 10.0.0.1 dev eth0 proto static metric 700 \n"
	if got := parseDefaultRouteDev(out); got != "wlp3s0" {
		t.Errorf("parseDefaultRouteDev = %q, want wlp3s0", got)
	}
	if got := parseDefaultRouteDev(""); got != "" {
		t.Errorf("empty output returned %q", got)
	}
}

func TestParsePingTimesWindows_German(t *testing.T) {
	output := `Ping wird ausgeführt für 1.1.1.1 mit 32 Bytes Daten:
Antwort von 1.1.1.1: Bytes=32 Zeit=11ms TTL=57
Antwort von 1.1.1.1: Bytes=32 Zeit=13ms TTL=57
Antwort von 1.1.1.1: Bytes=32 Zeit<1ms TTL=57

Ping-Statistik für 1.1.1.1:
    Pakete: Gesendet = 3, Empfangen = 3, Verloren = 0
    (0% Verlust),
Ca. Zeitangaben in Millisek.:
    Minimum = 1ms, Maximum = 13ms, Mittelwert = 8ms`

	rtts := parsePingTimesWindows(output)
	if len(rtts) != 3 {
		t.Fatalf("expected 3 RTTs from de-DE output, got %d (%v)", len(rtts), rtts)
	}
	if rtts[0] != 11 || rtts[1] != 13 || rtts[2] != 1 {
		t.Errorf("unexpected RTTs: %v", rtts)
	}
	loss, found := parsePingLossFound(output)
	if !found || loss != 0 {
		t.Errorf("parsePingLossFound = (%v, %v), want (0, true)", loss, found)
	}
}

func TestParsePingLossFound_NotPresent(t *testing.T) {
	if _, found := parsePingLossFound("Pinging 1.1.1.1 with 32 bytes of data:\nRequest timed out."); found {
		t.Error("loss should not be reported when no percentage is present")
	}
	if loss, found := parsePingLossFound("Pakete: Gesendet = 4, Empfangen = 0, Verloren = 4 (100% Verlust)"); !found || loss != 100 {
		t.Errorf("expected 100%% loss found, got (%v, %v)", loss, found)
	}
}

func TestParsePingTimes_NoSamples(t *testing.T) {
	out := "Pinging 1.1.1.1 with 32 bytes of data:\nRequest timed out.\nRequest timed out.\n\nPing statistics for 1.1.1.1:\n    Packets: Sent = 2, Received = 0, Lost = 2 (100% loss),"
	if rtts := parsePingTimesWindows(out); len(rtts) != 0 {
		t.Errorf("expected no RTT samples, got %v", rtts)
	}
	if rtts := parsePingTimesLinux("connect: Network is unreachable"); len(rtts) != 0 {
		t.Errorf("expected no RTT samples, got %v", rtts)
	}
}

func TestMedianFloat(t *testing.T) {
	tests := []struct {
		in   []float64
		want float64
	}{
		{nil, 0},
		{[]float64{5}, 5},
		{[]float64{58, 4400, 60}, 60},
		{[]float64{1, 2, 3, 4}, 2.5},
	}
	for _, tt := range tests {
		if got := medianFloat(tt.in); got != tt.want {
			t.Errorf("medianFloat(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestJitterFloat(t *testing.T) {
	if got := jitterFloat([]float64{10, 10, 10}); got != 0 {
		t.Errorf("constant RTT jitter = %v, want 0", got)
	}
	if got := jitterFloat([]float64{10}); got != 0 {
		t.Errorf("single sample jitter = %v, want 0", got)
	}
	if got := jitterFloat([]float64{10, 20, 10, 20}); got != 0 {
		// deltas are all 10 -> stddev 0
		t.Errorf("uniform deltas jitter = %v, want 0", got)
	}
}

// fakeResolver returns an error for the first failures calls, then succeeds.
type fakeResolver struct {
	failures int
	calls    int
}

func (f *fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	f.calls++
	if f.calls <= f.failures {
		return nil, errors.New("simulated SERVFAIL")
	}
	return []net.IPAddr{{IP: net.IPv4(142, 250, 80, 46)}}, nil
}

func TestMeasureDNS(t *testing.T) {
	r := &fakeResolver{failures: 1}
	samples, lastErr := measureDNS(context.Background(), r, "google.com", 3)
	if len(samples) != 2 {
		t.Fatalf("expected 2 successful samples, got %d", len(samples))
	}
	if lastErr == nil {
		t.Error("expected the failed attempt to be reported as lastErr")
	}
	if r.calls != 3 {
		t.Errorf("expected 3 lookups, got %d", r.calls)
	}

	allFail := &fakeResolver{failures: 3}
	samples, lastErr = measureDNS(context.Background(), allFail, "google.com", 3)
	if len(samples) != 0 || lastErr == nil {
		t.Errorf("all-fail resolver should yield no samples and an error, got %v / %v", samples, lastErr)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	samples, lastErr = measureDNS(cancelled, &fakeResolver{}, "google.com", 3)
	if len(samples) != 0 || lastErr == nil {
		t.Errorf("cancelled context should yield no samples, got %v / %v", samples, lastErr)
	}
}

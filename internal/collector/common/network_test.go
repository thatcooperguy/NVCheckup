package common

import (
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

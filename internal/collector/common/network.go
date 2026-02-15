package common

import (
	"fmt"
	"math"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// CollectNetworkInfo gathers network diagnostic data including interface detection,
// latency, jitter, packet loss, DNS resolution time, and traceroute hops.
func CollectNetworkInfo(timeout int) (types.NetworkInfo, []types.CollectorError) {
	var info types.NetworkInfo
	var errs []types.CollectorError

	// Step 1: Detect active network interface
	detectActiveInterface(&info, &errs, timeout)

	// Step 2: Detect wifi vs ethernet and gather wifi details
	detectInterfaceType(&info, &errs, timeout)

	// Step 3: Latency, jitter, and packet loss via ping
	collectPingStats(&info, &errs, timeout)

	// Step 4: DNS resolution time
	collectDNSTime(&info, &errs, timeout)

	// Step 5: Traceroute
	collectTraceroute(&info, &errs, timeout)

	return info, errs
}

// detectActiveInterface finds the primary active network interface.
func detectActiveInterface(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	if runtime.GOOS == "windows" {
		detectActiveInterfaceWindows(info, errs, timeout)
	} else {
		detectActiveInterfaceLinux(info, errs, timeout)
	}
}

// detectActiveInterfaceWindows uses netsh to find connected interfaces.
func detectActiveInterfaceWindows(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "netsh", "interface", "show", "interface")
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "network.interface",
			Error:     fmt.Sprintf("netsh interface show failed: %v", r.Err),
		})
		return
	}

	// Parse output lines looking for "Connected" state
	// Format: Admin State    State          Type             Interface Name
	for _, line := range strings.Split(r.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "Connected") && !strings.Contains(line, "Disconnected") {
			// Split by multiple spaces to get columns
			parts := regexp.MustCompile(`\s{2,}`).Split(line, -1)
			if len(parts) >= 4 {
				info.InterfaceName = strings.TrimSpace(parts[len(parts)-1])
				break
			}
		}
	}
}

// detectActiveInterfaceLinux uses ip route to find the default interface.
func detectActiveInterfaceLinux(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "ip", "route", "show", "default")
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "network.interface",
			Error:     fmt.Sprintf("ip route show default failed: %v", r.Err),
		})
		return
	}

	// Output: "default via 192.168.1.1 dev eth0 proto ..."
	// Extract device name after "dev"
	parts := strings.Fields(r.Stdout)
	for i, p := range parts {
		if p == "dev" && i+1 < len(parts) {
			info.InterfaceName = parts[i+1]
			break
		}
	}
}

// detectInterfaceType determines if the active interface is wifi or ethernet.
func detectInterfaceType(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	if runtime.GOOS == "windows" {
		detectInterfaceTypeWindows(info, errs, timeout)
	} else {
		detectInterfaceTypeLinux(info, errs, timeout)
	}
}

// detectInterfaceTypeWindows uses netsh wlan to check for wifi.
func detectInterfaceTypeWindows(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "netsh", "wlan", "show", "interfaces")
	if r.Err != nil {
		// netsh wlan may fail if no wifi adapter exists; that means ethernet
		info.InterfaceType = "ethernet"
		return
	}

	output := r.Stdout
	if strings.Contains(output, "State") && strings.Contains(output, "connected") {
		info.InterfaceType = "wifi"

		// Parse signal quality
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Signal") {
				// "Signal                 : 85%"
				_, val := util.ParseKeyValue(line, ":")
				val = strings.TrimSuffix(strings.TrimSpace(val), "%")
				if pct, err := strconv.Atoi(val); err == nil {
					// Convert signal quality percentage to approximate dBm
					// Windows reports quality 0-100%; rough mapping: dBm = (quality/2) - 100
					info.WifiSignalDBM = (pct / 2) - 100
				}
			}
			if strings.HasPrefix(line, "Radio type") {
				// "Radio type             : 802.11ax"
				_, val := util.ParseKeyValue(line, ":")
				val = strings.TrimSpace(val)
				info.WifiBand = val
			}
		}
	} else {
		info.InterfaceType = "ethernet"
	}
}

// detectInterfaceTypeLinux checks /sys/class/net and iwconfig for wifi.
func detectInterfaceTypeLinux(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	if info.InterfaceName == "" {
		info.InterfaceType = "unknown"
		return
	}

	// Check if the interface has a wireless directory
	r := util.RunCommand(timeout, "test", "-d",
		fmt.Sprintf("/sys/class/net/%s/wireless", info.InterfaceName))
	if r.ExitCode == 0 {
		info.InterfaceType = "wifi"

		// Try iwconfig for signal strength
		if util.CommandExists("iwconfig") {
			r = util.RunCommand(timeout, "iwconfig", info.InterfaceName)
			if r.Err == nil {
				// Parse signal level: "Signal level=-55 dBm"
				sigRe := regexp.MustCompile(`Signal level[=:](-?\d+)\s*dBm`)
				if m := sigRe.FindStringSubmatch(r.Stdout); m != nil {
					if v, err := strconv.Atoi(m[1]); err == nil {
						info.WifiSignalDBM = v
					}
				}

				// Parse frequency / standard for band info
				freqRe := regexp.MustCompile(`Frequency[=:](\d+\.?\d*)\s*GHz`)
				if m := freqRe.FindStringSubmatch(r.Stdout); m != nil {
					info.WifiBand = m[1] + " GHz"
				}
			}
		}
	} else {
		info.InterfaceType = "ethernet"
	}
}

// collectPingStats runs ping to 1.1.1.1 and computes latency, jitter, packet loss.
func collectPingStats(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	pingTimeout := timeout * 2

	var r util.CommandResult
	if runtime.GOOS == "windows" {
		r = util.RunCommand(pingTimeout, "ping", "-n", "10", "1.1.1.1")
	} else {
		r = util.RunCommand(pingTimeout, "ping", "-c", "10", "-i", "0.5", "1.1.1.1")
	}

	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "network.ping",
			Error:     fmt.Sprintf("ping failed: %v", r.Err),
		})
		// Still try to parse partial output if available
		if r.Stdout == "" {
			return
		}
	}

	// Parse round-trip times from output
	var rtts []float64
	if runtime.GOOS == "windows" {
		rtts = parsePingTimesWindows(r.Stdout)
	} else {
		rtts = parsePingTimesLinux(r.Stdout)
	}

	if len(rtts) > 0 {
		// Compute average latency
		var sum float64
		for _, rtt := range rtts {
			sum += rtt
		}
		info.LatencyMs = math.Round(sum/float64(len(rtts))*100) / 100

		// Compute jitter as standard deviation of consecutive differences
		if len(rtts) > 1 {
			var deltas []float64
			for i := 1; i < len(rtts); i++ {
				deltas = append(deltas, math.Abs(rtts[i]-rtts[i-1]))
			}
			var deltaSum float64
			for _, d := range deltas {
				deltaSum += d
			}
			mean := deltaSum / float64(len(deltas))

			var variance float64
			for _, d := range deltas {
				diff := d - mean
				variance += diff * diff
			}
			variance /= float64(len(deltas))
			info.JitterMs = math.Round(math.Sqrt(variance)*100) / 100
		}
	}

	// Parse packet loss from summary
	info.PacketLossPct = parsePingLoss(r.Stdout)
}

// parsePingTimesWindows extracts RTT values from Windows ping output.
// Windows format: "Reply from 1.1.1.1: bytes=32 time=12ms TTL=57"
func parsePingTimesWindows(output string) []float64 {
	var rtts []float64
	re := regexp.MustCompile(`time[=<](\d+(?:\.\d+)?)ms`)
	for _, line := range strings.Split(output, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				rtts = append(rtts, v)
			}
		}
	}
	return rtts
}

// parsePingTimesLinux extracts RTT values from Linux ping output.
// Linux format: "64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=12.3 ms"
func parsePingTimesLinux(output string) []float64 {
	var rtts []float64
	re := regexp.MustCompile(`time=(\d+(?:\.\d+)?)\s*ms`)
	for _, line := range strings.Split(output, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				rtts = append(rtts, v)
			}
		}
	}
	return rtts
}

// parsePingLoss extracts packet loss percentage from ping summary output.
func parsePingLoss(output string) float64 {
	// Windows: "(0% loss)" or Linux: "0% packet loss"
	re := regexp.MustCompile(`(\d+(?:\.\d+)?)%\s*(?:loss|packet loss)`)
	if m := re.FindStringSubmatch(output); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			return v
		}
	}
	return 0
}

// collectDNSTime measures DNS resolution time using nslookup.
func collectDNSTime(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	start := time.Now()
	r := util.RunCommand(timeout, "nslookup", "google.com")
	elapsed := time.Since(start)

	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "network.dns",
			Error:     fmt.Sprintf("nslookup failed: %v", r.Err),
		})
		return
	}

	info.DNSTimeMs = math.Round(float64(elapsed.Microseconds())/10) / 100 // milliseconds, 2 decimal places
}

// collectTraceroute runs traceroute/tracert and parses hop data.
func collectTraceroute(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	var r util.CommandResult
	if runtime.GOOS == "windows" {
		r = util.RunCommand(timeout*2, "tracert", "-d", "-h", "15", "-w", "2000", "1.1.1.1")
	} else {
		if util.CommandExists("traceroute") {
			r = util.RunCommand(timeout*2, "traceroute", "-n", "-m", "15", "-w", "2", "1.1.1.1")
		} else {
			*errs = append(*errs, types.CollectorError{
				Collector: "network.traceroute",
				Error:     "traceroute not found in PATH",
			})
			return
		}
	}

	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "network.traceroute",
			Error:     fmt.Sprintf("traceroute failed: %v", r.Err),
		})
		// Still try to parse partial output
		if r.Stdout == "" {
			return
		}
	}

	if runtime.GOOS == "windows" {
		info.Hops = parseTracerouteWindows(r.Stdout)
	} else {
		info.Hops = parseTracerouteLinux(r.Stdout)
	}
}

// parseTracerouteWindows parses Windows tracert output.
// Format:
//
//	1    <1 ms    <1 ms    <1 ms  192.168.1.1
//	2     *        *        *     Request timed out.
//	3    12 ms    11 ms    12 ms  10.0.0.1
func parseTracerouteWindows(output string) []types.HopInfo {
	var hops []types.HopInfo

	// Match lines starting with a hop number
	hopRe := regexp.MustCompile(`^\s*(\d+)\s+(.+)$`)
	// Match individual time values like "12 ms" or "<1 ms" or "*"
	timeRe := regexp.MustCompile(`(\d+)\s*ms`)
	// Match IP address at end of line
	ipRe := regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\s*$`)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		m := hopRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		hopNum, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		hop := types.HopInfo{
			Number: hopNum,
		}

		rest := m[2]

		// Check for complete timeout (all asterisks / "Request timed out")
		if strings.Contains(rest, "Request timed out") ||
			(strings.Count(rest, "*") >= 3 && !ipRe.MatchString(rest)) {
			hop.Loss = true
			hop.Address = "*"
			hops = append(hops, hop)
			continue
		}

		// Extract IP address
		if ipMatch := ipRe.FindStringSubmatch(rest); ipMatch != nil {
			hop.Address = ipMatch[1]
		}

		// Extract time values and compute average
		timeMatches := timeRe.FindAllStringSubmatch(rest, -1)
		if len(timeMatches) > 0 {
			var sum float64
			for _, tm := range timeMatches {
				if v, err := strconv.ParseFloat(tm[1], 64); err == nil {
					sum += v
				}
			}
			hop.LatencyMs = math.Round(sum/float64(len(timeMatches))*100) / 100
		}

		hops = append(hops, hop)
	}

	return hops
}

// parseTracerouteLinux parses Linux traceroute output.
// Format:
//
//	1  192.168.1.1  0.543 ms  0.432 ms  0.389 ms
//	2  * * *
//	3  10.0.0.1  12.345 ms  11.234 ms  12.567 ms
func parseTracerouteLinux(output string) []types.HopInfo {
	var hops []types.HopInfo

	hopRe := regexp.MustCompile(`^\s*(\d+)\s+(.+)$`)
	timeRe := regexp.MustCompile(`(\d+(?:\.\d+)?)\s*ms`)
	ipRe := regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		m := hopRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		hopNum, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}

		hop := types.HopInfo{
			Number: hopNum,
		}

		rest := m[2]

		// Check for complete timeout (all asterisks)
		cleaned := strings.ReplaceAll(rest, " ", "")
		if cleaned == "***" || strings.TrimSpace(rest) == "* * *" {
			hop.Loss = true
			hop.Address = "*"
			hops = append(hops, hop)
			continue
		}

		// Extract first IP address
		if ipMatch := ipRe.FindStringSubmatch(rest); ipMatch != nil {
			hop.Address = ipMatch[1]
		}

		// Extract time values and compute average
		timeMatches := timeRe.FindAllStringSubmatch(rest, -1)
		if len(timeMatches) > 0 {
			var sum float64
			for _, tm := range timeMatches {
				if v, err := strconv.ParseFloat(tm[1], 64); err == nil {
					sum += v
				}
			}
			hop.LatencyMs = math.Round(sum/float64(len(timeMatches))*100) / 100
		}

		// If no IP and has asterisks, mark as loss
		if hop.Address == "" && strings.Contains(rest, "*") {
			hop.Loss = true
			hop.Address = "*"
		}

		hops = append(hops, hop)
	}

	return hops
}

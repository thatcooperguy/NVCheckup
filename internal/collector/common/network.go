package common

import (
	"context"
	"fmt"
	"math"
	"net"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// CollectNetworkInfo gathers network diagnostic data including interface detection,
// latency, jitter, packet loss, DNS resolution time, and traceroute hops.
//
// This is the only collector that sends packets off the machine (ICMP ping and
// traceroute to 1.1.1.1, DNS lookup of google.com). The runner calls it only
// when the user opts in with --network.
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

// defaultRouteAliasPS prints the alias of the adapter that owns the lowest-metric
// default route, i.e. the one internet traffic actually leaves through.
const defaultRouteAliasPS = "Get-NetRoute -DestinationPrefix 0.0.0.0/0 | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1 -ExpandProperty InterfaceAlias"

// detectActiveInterfaceWindows picks the default-route adapter, falling back to
// netsh. The first "Connected" row of "netsh interface show interface" is often a
// VPN or virtual adapter (Tailscale, Hyper-V), which is why the route is preferred.
func detectActiveInterfaceWindows(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	r := util.RunCommand(timeout, "powershell", "-NoProfile", "-Command", defaultRouteAliasPS)
	if r.Err == nil {
		if name := firstLine(r.Stdout); name != "" {
			info.InterfaceName = name
			return
		}
	}

	r = util.RunCommand(timeout, "netsh", "interface", "show", "interface")
	if r.Err != nil {
		*errs = append(*errs, types.CollectorError{
			Collector: "network.interface",
			Error:     fmt.Sprintf("netsh interface show failed: %v", r.Err),
		})
		return
	}
	info.InterfaceName = parseNetshConnectedInterface(r.Stdout)
}

// netshColumnRe splits netsh table rows on runs of two or more spaces.
var netshColumnRe = regexp.MustCompile(`\s{2,}`)

// parseNetshConnectedInterface returns the Interface Name of the first row of
// "netsh interface show interface" whose State column is exactly "Connected".
// Columns: Admin State, State, Type, Interface Name. A substring match would
// also accept "Disconnected", so the column value is compared whole.
func parseNetshConnectedInterface(output string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := netshColumnRe.Split(strings.TrimSpace(line), -1)
		if len(parts) < 4 {
			continue
		}
		if strings.EqualFold(parts[1], "Connected") {
			return strings.TrimSpace(strings.Join(parts[3:], " "))
		}
	}
	return ""
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
	info.InterfaceName = parseDefaultRouteDev(r.Stdout)
}

// parseDefaultRouteDev extracts the "dev" field from the first line of
// "ip route show default" ("default via 192.168.1.1 dev eth0 proto dhcp ...").
// Several default routes may be listed; the first is the preferred one.
func parseDefaultRouteDev(output string) string {
	parts := strings.Fields(firstLine(output))
	for i, p := range parts {
		if p == "dev" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// detectInterfaceType determines if the active interface is wifi or ethernet.
func detectInterfaceType(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	if runtime.GOOS == "windows" {
		detectInterfaceTypeWindows(info, errs, timeout)
	} else {
		detectInterfaceTypeLinux(info, errs, timeout)
	}
}

// wlanInterface is one block of "netsh wlan show interfaces" output.
type wlanInterface struct {
	Name      string
	State     string
	RadioType string
	SignalPct int
	HasSignal bool
}

// parseWlanInterfaces parses the key/value blocks printed by
// "netsh wlan show interfaces" (one block per adapter, each starting with Name).
func parseWlanInterfaces(output string) []wlanInterface {
	var ifaces []wlanInterface
	for _, line := range strings.Split(output, "\n") {
		key, val := util.ParseKeyValue(strings.TrimSpace(line), ":")
		if key == "" {
			continue
		}
		if strings.EqualFold(key, "Name") {
			ifaces = append(ifaces, wlanInterface{Name: val})
			continue
		}
		if len(ifaces) == 0 {
			continue
		}
		cur := &ifaces[len(ifaces)-1]
		switch strings.ToLower(key) {
		case "state":
			cur.State = val
		case "radio type":
			cur.RadioType = val
		case "signal":
			if pct, err := strconv.Atoi(strings.TrimSuffix(val, "%")); err == nil {
				cur.SignalPct = pct
				cur.HasSignal = true
			}
		}
	}
	return ifaces
}

// activeWlan returns the wlan adapter that is connected AND is the active
// (default-route) adapter. Both conditions matter: a laptop on a dock can have
// a Wi-Fi adapter that is connected but idle while Ethernet carries traffic,
// and a substring check on "connected" also matched "disconnected".
func activeWlan(ifaces []wlanInterface, activeName string) (wlanInterface, bool) {
	if activeName == "" {
		return wlanInterface{}, false
	}
	for _, w := range ifaces {
		if strings.EqualFold(w.State, "connected") && strings.EqualFold(w.Name, activeName) {
			return w, true
		}
	}
	return wlanInterface{}, false
}

// detectInterfaceTypeWindows uses netsh wlan to check whether the active
// interface is a connected Wi-Fi adapter.
func detectInterfaceTypeWindows(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	if info.InterfaceName == "" {
		info.InterfaceType = "unknown"
		return
	}

	r := util.RunCommand(timeout, "netsh", "wlan", "show", "interfaces")
	if r.Err != nil {
		// netsh wlan fails when there is no wifi adapter or the WLAN service
		// is stopped; either way the active adapter is not wifi.
		info.InterfaceType = "ethernet"
		return
	}

	w, ok := activeWlan(parseWlanInterfaces(r.Stdout), info.InterfaceName)
	if !ok {
		info.InterfaceType = "ethernet"
		return
	}

	info.InterfaceType = "wifi"
	if w.HasSignal {
		// Windows reports quality 0-100%; rough mapping: dBm = (quality/2) - 100
		info.WifiSignalDBM = (w.SignalPct / 2) - 100
	}
	info.WifiBand = w.RadioType
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

// pingTarget is the anycast resolver used for latency probes.
const pingTarget = "1.1.1.1"

// collectPingStats runs ping to 1.1.1.1 and computes latency, jitter, packet loss.
func collectPingStats(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	pingTimeout := timeout * 2

	var r util.CommandResult
	if runtime.GOOS == "windows" {
		r = util.RunCommand(pingTimeout, "ping", "-n", "10", pingTarget)
	} else {
		r = util.RunCommand(pingTimeout, "ping", "-c", "10", "-i", "0.5", pingTarget)
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

	var rtts []float64
	if runtime.GOOS == "windows" {
		rtts = parsePingTimesWindows(r.Stdout)
	} else {
		rtts = parsePingTimesLinux(r.Stdout)
	}

	loss, lossFound := parsePingLossFound(r.Stdout)
	if lossFound {
		info.PacketLossPct = loss
	}

	if len(rtts) == 0 {
		// No samples: either every echo was lost (a real result) or the output
		// is in a format we do not understand. Silently reporting 0 ms / 0 %
		// would hide both, so distinguish them.
		if lossFound && loss >= 100 {
			*errs = append(*errs, types.CollectorError{
				Collector: "network.ping",
				Error:     "ping received no replies (100% loss)",
			})
		} else {
			*errs = append(*errs, types.CollectorError{
				Collector: "network.ping",
				Error:     "ping output could not be parsed",
			})
		}
		return
	}

	info.LatencyMs = roundMs(meanFloat(rtts))
	info.JitterMs = roundMs(jitterFloat(rtts))
}

// meanFloat returns the arithmetic mean of v (0 for empty input).
func meanFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

// jitterFloat is the standard deviation of consecutive RTT differences.
func jitterFloat(rtts []float64) float64 {
	if len(rtts) < 2 {
		return 0
	}
	deltas := make([]float64, 0, len(rtts)-1)
	for i := 1; i < len(rtts); i++ {
		deltas = append(deltas, math.Abs(rtts[i]-rtts[i-1]))
	}
	mean := meanFloat(deltas)
	var variance float64
	for _, d := range deltas {
		diff := d - mean
		variance += diff * diff
	}
	return math.Sqrt(variance / float64(len(deltas)))
}

// medianFloat returns the median of v (0 for empty input).
func medianFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := make([]float64, len(v))
	copy(s, v)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

// roundMs rounds a millisecond value to two decimal places.
func roundMs(ms float64) float64 {
	return math.Round(ms*100) / 100
}

// pingTimeWindowsRe matches English Windows replies: "time=12ms" / "time<1ms".
var pingTimeWindowsRe = regexp.MustCompile(`time[=<](\d+(?:\.\d+)?)ms`)

// pingTimeLinuxRe matches iputils replies: "time=12.3 ms".
var pingTimeLinuxRe = regexp.MustCompile(`time=(\d+(?:\.\d+)?)\s*ms`)

// pingTimeFallbackRe matches a number immediately followed by "ms" regardless
// of the label in front of it ("Zeit=11ms", "temps=11 ms", "tempo=11ms").
var pingTimeFallbackRe = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s?ms\b`)

// pingLossRe matches English loss summaries: "(0% loss)" or "0% packet loss".
var pingLossRe = regexp.MustCompile(`(\d+(?:\.\d+)?)%\s*(?:loss|packet loss)`)

// pingLossFallbackRe matches the first percentage anywhere, for localized
// summaries such as "(0% Verlust)" or "(0% perdidos)".
var pingLossFallbackRe = regexp.MustCompile(`(\d+(?:[.,]\d+)?)%`)

// parsePingTimesWindows extracts RTT values from Windows ping output.
// Windows format: "Reply from 1.1.1.1: bytes=32 time=12ms TTL=57"
func parsePingTimesWindows(output string) []float64 {
	rtts := parsePingTimesWith(pingTimeWindowsRe, output)
	if len(rtts) == 0 {
		rtts = parsePingTimesFallback(output)
	}
	return rtts
}

// parsePingTimesLinux extracts RTT values from Linux ping output.
// Linux format: "64 bytes from 1.1.1.1: icmp_seq=1 ttl=57 time=12.3 ms"
func parsePingTimesLinux(output string) []float64 {
	rtts := parsePingTimesWith(pingTimeLinuxRe, output)
	if len(rtts) == 0 {
		rtts = parsePingTimesFallback(output)
	}
	return rtts
}

// parsePingTimesWith collects one RTT per line matching re.
func parsePingTimesWith(re *regexp.Regexp, output string) []float64 {
	var rtts []float64
	for _, line := range strings.Split(output, "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			if v, err := parseLocaleFloat(m[1]); err == nil {
				rtts = append(rtts, v)
			}
		}
	}
	return rtts
}

// parsePingTimesFallback handles localized ping output. Only reply lines carry
// a TTL; summary lines ("Minimum = 11ms, Maximum = 14ms") do not, and must not
// be mistaken for samples.
func parsePingTimesFallback(output string) []float64 {
	var rtts []float64
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(strings.ToLower(line), "ttl=") {
			continue
		}
		if m := pingTimeFallbackRe.FindStringSubmatch(line); m != nil {
			if v, err := parseLocaleFloat(m[1]); err == nil {
				rtts = append(rtts, v)
			}
		}
	}
	return rtts
}

// parseLocaleFloat parses a decimal that may use a comma separator.
func parseLocaleFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
}

// parsePingLoss extracts packet loss percentage from ping summary output,
// returning 0 when no percentage is found.
func parsePingLoss(output string) float64 {
	loss, _ := parsePingLossFound(output)
	return loss
}

// parsePingLossFound extracts the packet loss percentage and reports whether
// one was actually present in the output.
func parsePingLossFound(output string) (float64, bool) {
	m := pingLossRe.FindStringSubmatch(output)
	if m == nil {
		m = pingLossFallbackRe.FindStringSubmatch(output)
	}
	if m == nil {
		return 0, false
	}
	v, err := parseLocaleFloat(m[1])
	if err != nil {
		return 0, false
	}
	return v, true
}

// dnsLookupHost is the name resolved for the DNS timing probe.
const dnsLookupHost = "google.com"

// dnsLookupAttempts is how many lookups are timed; the median is reported so a
// single cold-cache miss does not dominate.
const dnsLookupAttempts = 3

// ipLookup is the part of net.Resolver used by measureDNS, so tests can stub it.
type ipLookup interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// collectDNSTime measures DNS resolution time in-process. It used to time an
// nslookup child process, but nslookup also performs a reverse (PTR) lookup of
// the resolver itself, so it measured 4.4 s where the real lookup took 58 ms.
func collectDNSTime(info *types.NetworkInfo, errs *[]types.CollectorError, timeout int) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	samples, lastErr := measureDNS(ctx, net.DefaultResolver, dnsLookupHost, dnsLookupAttempts)
	if len(samples) == 0 {
		*errs = append(*errs, types.CollectorError{
			Collector: "network.dns",
			Error:     fmt.Sprintf("DNS lookup of %s failed: %v", dnsLookupHost, lastErr),
		})
		return
	}
	info.DNSTimeMs = roundMs(medianFloat(samples))
}

// measureDNS times attempts lookups of host and returns the successful samples
// in milliseconds plus the last error seen (nil if every attempt succeeded).
func measureDNS(ctx context.Context, r ipLookup, host string, attempts int) ([]float64, error) {
	var samples []float64
	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		start := time.Now()
		_, err := r.LookupIPAddr(ctx, host)
		elapsed := time.Since(start)
		if err != nil {
			lastErr = err
			continue
		}
		samples = append(samples, float64(elapsed.Microseconds())/1000)
	}
	return samples, lastErr
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

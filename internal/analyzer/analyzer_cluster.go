package analyzer

// ConnectX-7 clustering rules (docs/roadmap/spark-support.md sections 5 and
// 9). They read Report.Cluster as filled by the read-only cx7 collector and
// run in ai and full modes only (spec 5 "Modes": cx7-*/nccl-*).

import (
	"fmt"
	"sort"
	"strings"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// cx7ExpectedSpeedMbps is the healthy per-twin link speed (spec 9: 200000 Mb/s).
const cx7ExpectedSpeedMbps = 200000

// cx7HealthyMTU is the MTU the playbooks require on every twin (spec 9).
const cx7HealthyMTU = 9000

// ncclMinVersion: NCCL < 2.28 lacks sm_121 support (spec 5, nccl-env-misconfigured).
const ncclMinVersion = "2.28"

// portUp reports whether a ConnectX-7 twin is ACTIVE / LinkUp.
func portUp(p types.FabricPort) bool {
	return strings.Contains(strings.ToUpper(p.State), "ACTIVE") || strings.Contains(p.PhysState, "LinkUp")
}

// portDown reports whether a twin is explicitly DOWN.
func portDown(p types.FabricPort) bool {
	return strings.Contains(strings.ToUpper(p.State), "DOWN") || strings.Contains(p.PhysState, "Down") || strings.Contains(p.PhysState, "Polling")
}

// portName renders "enp1s0f0np0 (rocep1s0f0)".
func portName(p types.FabricPort) string {
	switch {
	case p.Netdev != "" && p.RDMADev != "":
		return p.Netdev + " (" + p.RDMADev + ")"
	case p.Netdev != "":
		return p.Netdev
	case p.RDMADev != "":
		return p.RDMADev
	}
	return orNA(p.PCIAddr)
}

// subnet24 returns the /24 prefix of a dotted IPv4 ("192.168.100.1/24" ->
// "192.168.100"), or "" for redacted or malformed values.
func subnet24(ip string) string {
	ip = strings.TrimSpace(ip)
	if i := strings.Index(ip, "/"); i > 0 {
		ip = ip[:i]
	}
	parts := strings.Split(ip, ".")
	if len(parts) != 4 || strings.HasPrefix(ip, "<") {
		return ""
	}
	return strings.Join(parts[:3], ".")
}

// cagesOf groups ports by cage in ascending cage order.
func cagesOf(ports []types.FabricPort) (order []int, byCage map[int][]types.FabricPort) {
	byCage = map[int][]types.FabricPort{}
	for _, p := range ports {
		if _, ok := byCage[p.Cage]; !ok {
			order = append(order, p.Cage)
		}
		byCage[p.Cage] = append(byCage[p.Cage], p)
	}
	sort.Ints(order)
	return order, byCage
}

// analyzeCluster runs every cx7-* and nccl-* rule.
func analyzeCluster(r *types.Report) []types.Finding {
	var findings []types.Finding
	if !isDGXSpark(r) {
		return findings
	}
	logs := logText(r)
	c := r.Cluster
	var ports []types.FabricPort
	if c != nil {
		ports = c.Ports
	}

	// Rule row cx7-not-enumerated (spec 5): CRIT when no 15b3 function is
	// present and dmesg shows the hotplug removal / retraining failure; WARN
	// variant on the 6.17.0-1021/1029-nvidia regression kernels even when
	// the NIC is present.
	hotplugLine := firstLineContaining(logs, "cx7-pcie-hotplug", "Cable removal", "retraining non-functional downstream link")
	switch {
	case len(ports) == 0 && hotplugLine != "":
		findings = append(findings, sparkFinding("cx7-not-enumerated", fmt.Sprintf("ConnectX-7 not enumerated: 0 15b3 devices; kernel %s; dmesg '%s'.", orNA(r.System.KernelVersion), hotplugLine)))
	case cx7RegressionKernel(r.System.KernelVersion):
		f := sparkFinding("cx7-not-enumerated", fmt.Sprintf("ConnectX-7 not enumerated: %d 15b3 devices; kernel %s has the hotplug regression that drops the NIC 5-22 s after boot; dmesg '%s'.", len(ports), r.System.KernelVersion, orNA(hotplugLine)))
		f.Severity = types.SeverityWarn
		findings = append(findings, f)
	}
	if c == nil {
		return findings
	}

	order, byCage := cagesOf(ports)
	anyUp := false
	var activeTwins, activeNetdevs []string
	for _, p := range ports {
		if portUp(p) {
			anyUp = true
			if p.RDMADev != "" {
				activeTwins = append(activeTwins, p.RDMADev)
			}
			if p.Netdev != "" {
				activeNetdevs = append(activeNetdevs, p.Netdev)
			}
		}
	}

	// Rule row cx7-twin-link-mismatch (spec 5): within one cage one twin
	// ACTIVE and the other DOWN, or the QSFP/cable dmesg lines.
	qsfpLine := firstLineContaining(logs, "QSFP module not powered", "Cable unplugged")
	for _, cage := range order {
		twins := byCage[cage]
		if len(twins) < 2 {
			continue
		}
		var up, down *types.FabricPort
		for i := range twins {
			switch {
			case portUp(twins[i]) && up == nil:
				up = &twins[i]
			case portDown(twins[i]) && down == nil:
				down = &twins[i]
			}
		}
		if up != nil && down != nil {
			findings = append(findings, sparkFinding("cx7-twin-link-mismatch", fmt.Sprintf("Port %d: %s %s but %s %s; /etc/nvidia/cx7-hotplug-enabled %s.",
				cage, portName(*up), orNA(up.State), portName(*down), orNA(down.State), boolWord(c.HotplugFileEnabled, "present", "absent"))))
			qsfpLine = ""
		}
	}
	if qsfpLine != "" {
		findings = append(findings, sparkFinding("cx7-twin-link-mismatch", fmt.Sprintf("dmesg '%s'; /etc/nvidia/cx7-hotplug-enabled %s.", qsfpLine, boolWord(c.HotplugFileEnabled, "present", "absent"))))
	}

	// Rule row cx7-link-speed-degraded (spec 5): an Up twin below 200000 Mb/s.
	for _, p := range ports {
		if portUp(p) && p.SpeedMbps > 0 && p.SpeedMbps != cx7ExpectedSpeedMbps {
			findings = append(findings, sparkFinding("cx7-link-speed-degraded", fmt.Sprintf("%s negotiated %d Mb/s; expected %d; CX-7 firmware %s.",
				portName(p), p.SpeedMbps, cx7ExpectedSpeedMbps, firmwareVersionOf(r, "cx7"))))
		}
	}

	// Rule row cx7-up-no-ip (spec 5): Up twin without IPv4 (WARN) or with an
	// address that is not persisted (INFO).
	for _, p := range ports {
		if !portUp(p) {
			continue
		}
		switch {
		case len(p.IPv4) == 0:
			findings = append(findings, sparkFinding("cx7-up-no-ip", fmt.Sprintf("%s Up at %d Mb/s; IPv4 none; persistent config %s.", portName(p), p.SpeedMbps, boolWord(p.Persistent, "yes", "no"))))
		case !p.Persistent:
			f := sparkFinding("cx7-up-no-ip", fmt.Sprintf("%s Up at %d Mb/s; IPv4 %s; persistent config no (ip addr add is lost at reboot).", portName(p), p.SpeedMbps, strings.Join(p.IPv4, ", ")))
			f.Severity = types.SeverityInfo
			findings = append(findings, f)
		}
	}

	// Rule row cx7-twins-same-subnet (spec 5): both twins of a cage in one
	// subnet, or a twin enslaved to a bond.
	for _, cage := range order {
		twins := byCage[cage]
		for i := 0; i < len(twins); i++ {
			if twins[i].Bond != "" {
				findings = append(findings, sparkFinding("cx7-twins-same-subnet", fmt.Sprintf("%s is enslaved to bond %s; twins must stay separate interfaces.", portName(twins[i]), twins[i].Bond)))
				break
			}
			for j := i + 1; j < len(twins); j++ {
				for _, a := range twins[i].IPv4 {
					for _, b := range twins[j].IPv4 {
						if s := subnet24(a); s != "" && s == subnet24(b) {
							findings = append(findings, sparkFinding("cx7-twins-same-subnet", fmt.Sprintf("%s=%s and %s=%s share %s.0/24; bond %s.",
								portName(twins[i]), a, portName(twins[j]), b, s, orNA(twins[i].Bond+twins[j].Bond))))
						}
					}
				}
			}
		}
	}

	// Rule row cx7-mtu-mismatch (spec 5): MTU differs between twins of a cage,
	// or an addressed twin is 1500 while netplan/peer uses 9000.
	for _, cage := range order {
		twins := byCage[cage]
		for i := 1; i < len(twins); i++ {
			if twins[0].MTU > 0 && twins[i].MTU > 0 && twins[0].MTU != twins[i].MTU {
				findings = append(findings, sparkFinding("cx7-mtu-mismatch", fmt.Sprintf("MTU %s=%d, %s=%d; netplan mtu %s.", portName(twins[0]), twins[0].MTU, portName(twins[i]), twins[i].MTU, mtuOrUnset(c.NetplanMTU))))
			}
		}
	}
	for _, p := range ports {
		if len(p.IPv4) > 0 && p.MTU > 0 && p.MTU < cx7HealthyMTU && c.NetplanMTU == cx7HealthyMTU {
			findings = append(findings, sparkFinding("cx7-mtu-mismatch", fmt.Sprintf("MTU %s=%d while netplan mtu %d.", portName(p), p.MTU, c.NetplanMTU)))
		}
	}

	// Rule row nccl-env-misconfigured (spec 5): only meaningful with twins Up.
	if anyUp {
		env := c.NCCLEnv
		hca := env["NCCL_IB_HCA"]
		var problems []string
		if hca != "" {
			names := strings.Split(hca, ",")
			cageOf := map[string]int{}
			for _, p := range ports {
				if p.RDMADev != "" {
					cageOf[p.RDMADev] = p.Cage
				}
			}
			perCage := map[int]int{}
			for _, n := range names {
				n = strings.TrimSpace(strings.TrimSuffix(n, ":1"))
				for _, p := range ports {
					if p.Netdev != "" && n == p.Netdev {
						problems = append(problems, "NCCL_IB_HCA names netdev "+n)
					}
				}
				if cage, ok := cageOf[n]; ok {
					perCage[cage]++
				}
			}
			for cage, n := range perCage {
				if n == 1 && len(byCage[cage]) > 1 {
					problems = append(problems, fmt.Sprintf("NCCL_IB_HCA lists one twin of cage %d", cage))
				}
			}
		}
		if env["NCCL_IB_DISABLE"] == "1" {
			problems = append(problems, "NCCL_IB_DISABLE=1")
		}
		if c.NCCLPluginLib != "" && env["NCCL_NET_PLUGIN"] != "none" {
			problems = append(problems, "net plugin present without NCCL_NET_PLUGIN=none")
		}
		if c.NCCLVersion != "" && versionLess(c.NCCLVersion, ncclMinVersion) {
			problems = append(problems, "libnccl "+c.NCCLVersion+" < "+ncclMinVersion)
		}
		ncclLine := firstLineContaining(logs, "Using network Socket", "ibv_modify_qp failed with 110")
		if ncclLine != "" {
			problems = append(problems, "log shows fallback")
		}
		if len(problems) > 0 {
			f := sparkFinding("nccl-env-misconfigured", fmt.Sprintf("NCCL_IB_HCA=%s; NCCL_SOCKET_IFNAME=%s; NCCL_IB_DISABLE=%s; NCCL_NET_PLUGIN=%s (lib %s); libnccl %s; log '%s'. Problems: %s.",
				orNA(hca), orNA(env["NCCL_SOCKET_IFNAME"]), orNA(env["NCCL_IB_DISABLE"]), orNA(env["NCCL_NET_PLUGIN"]), orNA(c.NCCLPluginLib), orNA(c.NCCLVersion), orNA(ncclLine), strings.Join(problems, "; ")))
			// The HCA list is templated from the twins the collector saw
			// ACTIVE (spec 9: never a hard-coded port).
			for i, step := range f.NextSteps {
				step = strings.ReplaceAll(step, "{active_twins}", orNA(strings.Join(activeTwins, ",")))
				step = strings.ReplaceAll(step, "{cabled_netdev}", orNA(strings.Join(activeNetdevs, ",")))
				f.NextSteps[i] = step
			}
			findings = append(findings, f)
		}
	}

	// Rule row nccl-gdr-assumed (spec 5), INFO.
	gdr := c.NCCLEnv["NCCL_NET_GDR_LEVEL"]
	gdrSet := gdr != "" && gdr != "0" && !strings.EqualFold(gdr, "LOC")
	dmabuf := c.NCCLEnv["NCCL_DMABUF_ENABLE"] == "1"
	peermemLine := firstLineContaining(logs, "nvidia_peermem: Unknown symbol ib_register_peer_memory_client")
	if gdrSet || dmabuf || c.PeermemAttempted || peermemLine != "" {
		varName, value := "NCCL_NET_GDR_LEVEL", gdr
		if !gdrSet && dmabuf {
			varName, value = "NCCL_DMABUF_ENABLE", "1"
		}
		findings = append(findings, sparkFinding("nccl-gdr-assumed", fmt.Sprintf("%s=%s; nvidia_peermem load attempted %s. NCCL logs 'GPU Direct RDMA Disabled for HCA' / 'GDR 0' on Spark.",
			varName, orNA(value), boolWord(c.PeermemAttempted || peermemLine != "", "yes", "no"))))
	}

	// Rule row cx7-mdns-hostname-conflict (spec 5): WARN on avahi conflicts
	// or a -2/-3 hostname suffix, INFO when twins are Up but avahi is absent.
	host := strings.TrimSpace(r.System.Hostname)
	renamed := !strings.HasPrefix(host, "<") && (strings.HasSuffix(host, "-2") || strings.HasSuffix(host, "-3"))
	conflictLine := firstLineContaining(logs, "Host name conflict, retrying with")
	switch {
	case c.AvahiConflicts > 0 || renamed || conflictLine != "":
		newName := "n/a"
		if renamed {
			newName = host
		}
		findings = append(findings, sparkFinding("cx7-mdns-hostname-conflict", fmt.Sprintf("avahi %s: renamed host to %s ('%s'); %d conflicts in the journal; avahi-utils %s.",
			boolWord(c.AvahiActive, "active", "inactive"), newName, orNA(conflictLine), c.AvahiConflicts, boolWord(hasRDMATool(c, "avahi-browse"), "present", "not detected"))))
	case anyUp && !c.AvahiActive:
		f := sparkFinding("cx7-mdns-hostname-conflict", fmt.Sprintf("avahi inactive while %d twin(s) are Up: discover-sparks and NVIDIA Sync need avahi-daemon and avahi-browse; avahi-utils %s.",
			len(activeTwins), boolWord(hasRDMATool(c, "avahi-browse"), "present", "not detected")))
		f.Severity = types.SeverityInfo
		findings = append(findings, f)
	}

	// Rule row cx7-firewall-blocks-cluster (spec 5): ufw enabled with
	// addressed twins. Allow rules are not collected, so the finding says so.
	var subnets []string
	for _, p := range ports {
		for _, ip := range p.IPv4 {
			if s := subnet24(ip); s != "" {
				subnets = append(subnets, s+".0/24")
			}
		}
	}
	if c.UfwEnabled && len(subnets) > 0 {
		findings = append(findings, sparkFinding("cx7-firewall-blocks-cluster", fmt.Sprintf("ufw enabled, default policy not collected; allow rules for 5353/udp, 22/tcp, 29500/tcp on %s not verified (rules are not read).", strings.Join(subnets, ", "))))
	}
	return findings
}

func mtuOrUnset(mtu int) string {
	if mtu <= 0 {
		return "unset"
	}
	return fmt.Sprintf("%d", mtu)
}

// hasRDMATool reports whether the collector saw the named tool on PATH.
func hasRDMATool(c *types.ClusterInfo, name string) bool {
	for _, t := range c.RDMATools {
		if strings.EqualFold(strings.TrimSpace(t), name) {
			return true
		}
	}
	return false
}

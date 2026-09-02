package linux

// ConnectX-7 clustering collector for DGX Spark (spec section 9 and WP1 item
// 7). Read-only: sysfs, /etc files, the process environment and a handful of
// query-only tools. No socket is ever opened and no peer is probed. The file
// carries no build tag so the parsers are tested on every OS; the runner
// calls it on Linux for dgx-spark only.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thatcooperguy/nvcheckup/internal/util"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

const (
	// mellanoxVendorID is the PCI vendor of the ConnectX-7 functions
	// (spec 2.1 "Networking": 15b3:1021).
	mellanoxVendorID = "0x15b3"
	sysInfiniband    = "/sys/class/infiniband"
	sysNet           = "/sys/class/net"
	// cx7HotplugFile is the persistence/hotplug marker (spec section 9).
	cx7HotplugFile = "/etc/nvidia/cx7-hotplug-enabled"
	// netplanDir holds the persisted interface configuration (spec section 9).
	netplanDir = "/etc/netplan"
	// nmConnectionsDir is NetworkManager's persisted profiles (rule cx7-up-no-ip
	// mentions netplan/NM).
	nmConnectionsDir = "/etc/NetworkManager/system-connections"
	// ufwConf carries ENABLED=yes|no (spec section 9).
	ufwConf = "/etc/ufw/ufw.conf"
	// avahiUnit is the mDNS daemon whose hostname conflicts rename Sparks
	// (rule cx7-mdns-hostname-conflict, S70).
	avahiUnit = "avahi-daemon.service"
	// avahiConflictMarker is the journal text of a rename (S70).
	avahiConflictMarker = "Host name conflict"
	// peermemModule is the module whose load attempt nccl-gdr-assumed reports (S69).
	peermemModule = "nvidia_peermem"
	// maxImagesInspected bounds docker/ldconfig style enumerations elsewhere;
	// here it bounds the ports we enumerate (defensive).
	maxFabricPorts = 16
)

// rdmaToolCandidates are the rdma-core tools whose presence is recorded
// (spec section 9 "rdma tools"; S17 S18 use ibstat and ibdev2netdev).
var rdmaToolCandidates = []string{"ibstat", "ibdev2netdev", "rdma", "ibv_devinfo", "ibv_devices", "ib_write_bw", "ucx_info"}

// ncclEnvPrefixes select the environment variables kept in NCCLEnv (spec
// section 9: NCCL_* and UCX_NET_DEVICES; WP1 item 7: NCCL_*/UCX).
var ncclEnvPrefixes = []string{"NCCL_", "UCX_"}

// CollectCluster gathers ConnectX-7 fabric, NCCL and neighbour-discovery
// state. Missing sysfs entries or tools leave fields empty; errors are only
// recorded for tools that exist but fail.
func CollectCluster(timeout int) (types.ClusterInfo, []types.CollectorError) {
	var info types.ClusterInfo
	var errs []types.CollectorError

	info.Ports = discoverFabricPorts(timeout)
	info.HotplugFileEnabled = simFileExists(cx7HotplugFile)
	applyNetplan(&info)
	info.NCCLEnv = ncclEnvironment(os.Environ())
	collectNCCLLibs(&info, timeout)
	info.PeermemAttempted = peermemAttempted(timeout)
	collectAvahi(&info, timeout)
	info.UfwEnabled = parseUfwEnabled(readSimFile(ufwConf))
	for _, tool := range rdmaToolCandidates {
		if util.CommandExists(tool) {
			info.RDMATools = append(info.RDMATools, tool)
		}
	}
	return info, errs
}

// ---------------------------------------------------------------------------
// Port discovery
// ---------------------------------------------------------------------------

// bdfRe matches a full PCI address with domain, e.g. 0002:01:00.1.
var bdfRe = regexp.MustCompile(`^([0-9a-fA-F]{4}):([0-9a-fA-F]{2}):([0-9a-fA-F]{2})\.([0-7])$`)

// netdevFunctionRe extracts the function index from a predictable netdev name
// such as enp1s0f0np0 or enP2p1s0f1np1 (spec 2.1 "Networking").
var netdevFunctionRe = regexp.MustCompile(`f(\d)(np\d)?$`)

// cageOf groups the twin functions of one QSFP cage: function .0 of domains
// 0000 and 0002 is port/cage 0, function .1 is cage 1 (spec section 9: group
// by function index across domains, never by stripping the function). It
// falls back to the netdev name and returns -1 when neither is known.
func cageOf(pciAddr, netdev string) int {
	if m := bdfRe.FindStringSubmatch(pciAddr); m != nil {
		n, _ := strconv.Atoi(m[4])
		return n
	}
	if m := netdevFunctionRe.FindStringSubmatch(netdev); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return -1
}

// pciAddrOfDevice returns the BDF of a sysfs "device" link: the symlink target
// basename, or PCI_SLOT_NAME from device/uevent when the tree is a plain copy
// (fixtures under NVC_SIM_ROOT).
func pciAddrOfDevice(devicePath string) string {
	if target, err := os.Readlink(devicePath); err == nil {
		if base := filepath.Base(target); bdfRe.MatchString(base) {
			return base
		}
	}
	if resolved, err := filepath.EvalSymlinks(devicePath); err == nil {
		if base := filepath.Base(resolved); bdfRe.MatchString(base) {
			return base
		}
	}
	if data, err := os.ReadFile(filepath.Join(devicePath, "uevent")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if k, v := util.ParseKeyValue(line, "="); k == "PCI_SLOT_NAME" && bdfRe.MatchString(v) {
				return v
			}
		}
	}
	return ""
}

// readSysAttr reads a small sysfs attribute (already simPath-resolved base).
func readSysAttr(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// discoverFabricPorts enumerates the ConnectX-7 functions from
// /sys/class/net (vendor 0x15b3) and /sys/class/infiniband (port state, rate,
// device/net mapping), then fills gaps from ibdev2netdev when present, and
// finally attaches operstate/speed/mtu/bond/IPv4 per netdev.
func discoverFabricPorts(timeout int) []types.FabricPort {
	byKey := map[string]*types.FabricPort{}
	var order []string
	add := func(netdev, rdma, pci string) *types.FabricPort {
		for _, k := range order {
			p := byKey[k]
			if (netdev != "" && p.Netdev == netdev) || (rdma != "" && p.RDMADev == rdma) || (pci != "" && p.PCIAddr == pci && netdev == "" && rdma == "") {
				if p.Netdev == "" {
					p.Netdev = netdev
				}
				if p.RDMADev == "" {
					p.RDMADev = rdma
				}
				if p.PCIAddr == "" {
					p.PCIAddr = pci
				}
				return p
			}
		}
		if len(order) >= maxFabricPorts {
			return &types.FabricPort{}
		}
		key := netdev + "|" + rdma + "|" + pci
		p := &types.FabricPort{Netdev: netdev, RDMADev: rdma, PCIAddr: pci}
		byKey[key] = p
		order = append(order, key)
		return p
	}

	// 1. Netdevs backed by a Mellanox function.
	netRoot := simPath(sysNet)
	if entries, err := os.ReadDir(netRoot); err == nil {
		for _, e := range entries {
			dev := filepath.Join(netRoot, e.Name(), "device")
			if readSysAttr(filepath.Join(dev, "vendor")) != mellanoxVendorID {
				continue
			}
			add(e.Name(), "", pciAddrOfDevice(dev))
		}
	}

	// 2. RDMA devices: state/phys_state/rate and the device/net mapping.
	ibRoot := simPath(sysInfiniband)
	if entries, err := os.ReadDir(ibRoot); err == nil {
		for _, e := range entries {
			devDir := filepath.Join(ibRoot, e.Name())
			pci := pciAddrOfDevice(filepath.Join(devDir, "device"))
			var netdev string
			if nets, err := os.ReadDir(filepath.Join(devDir, "device", "net")); err == nil && len(nets) > 0 {
				netdev = nets[0].Name()
			}
			p := add(netdev, e.Name(), pci)
			portDirs, _ := filepath.Glob(filepath.Join(devDir, "ports", "*"))
			sort.Strings(portDirs)
			if len(portDirs) > 0 {
				p.State = readSysAttr(filepath.Join(portDirs[0], "state"))
				p.PhysState = readSysAttr(filepath.Join(portDirs[0], "phys_state"))
				if p.SpeedMbps == 0 {
					p.SpeedMbps = rateToMbps(readSysAttr(filepath.Join(portDirs[0], "rate")))
				}
			}
		}
	}

	// 3. ibdev2netdev fills the mapping when sysfs lacks it (and answers in
	//    the simulated scenario, spec section 10).
	if util.CommandExists("ibdev2netdev") {
		r := util.RunCommand(timeout, "ibdev2netdev")
		for _, m := range parseIbdev2netdev(r.Stdout) {
			p := add(m.Netdev, m.RDMADev, "")
			if p.State == "" && m.State != "" {
				p.State = m.State
			}
		}
	}

	// 4. Netdev attributes.
	ipv4 := map[string][]string{}
	if util.CommandExists("ip") {
		r := util.RunCommand(timeout, "ip", "-4", "-o", "addr", "show")
		ipv4 = parseIPAddrShow(r.Stdout)
	}
	ports := make([]types.FabricPort, 0, len(order))
	for _, k := range order {
		p := byKey[k]
		p.Cage = cageOf(p.PCIAddr, p.Netdev)
		if p.Netdev != "" {
			base := filepath.Join(netRoot, p.Netdev)
			if p.State == "" {
				p.State = readSysAttr(filepath.Join(base, "operstate"))
			}
			if speed, err := strconv.Atoi(readSysAttr(filepath.Join(base, "speed"))); err == nil && speed > 0 {
				p.SpeedMbps = speed
			}
			if mtu, err := strconv.Atoi(readSysAttr(filepath.Join(base, "mtu"))); err == nil {
				p.MTU = mtu
			}
			if master, err := os.Readlink(filepath.Join(base, "master")); err == nil {
				p.Bond = filepath.Base(master)
			} else if _, err := os.Stat(filepath.Join(base, "bonding_slave")); err == nil && p.Bond == "" {
				p.Bond = "bond"
			}
			p.IPv4 = ipv4[p.Netdev]
		}
		ports = append(ports, *p)
	}
	sort.SliceStable(ports, func(i, j int) bool {
		if ports[i].Cage != ports[j].Cage {
			return ports[i].Cage < ports[j].Cage
		}
		return ports[i].PCIAddr+ports[i].Netdev < ports[j].PCIAddr+ports[j].Netdev
	})
	return ports
}

// rateRe matches "200 Gb/sec (4X NDR)" style /sys/class/infiniband rate values.
var rateRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([GM])b/sec`)

// rateToMbps converts an InfiniBand "rate" attribute to Mb/s (200 Gb/sec ->
// 200000, the healthy value of spec section 9).
func rateToMbps(rate string) int {
	m := rateRe.FindStringSubmatch(strings.TrimSpace(rate))
	if m == nil {
		return 0
	}
	f, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	if m[2] == "G" {
		f *= 1000
	}
	return int(f + 0.5)
}

// ibdevMapping is one "rdma port N ==> netdev (State)" row.
type ibdevMapping struct {
	RDMADev string
	Netdev  string
	State   string
}

// ibdev2netdevRe matches "rocep1s0f0 port 1 ==> enp1s0f0np0 (Down)" (spec
// section 10 fixture; S17).
var ibdev2netdevRe = regexp.MustCompile(`^(\S+)\s+port\s+\d+\s+==>\s+(\S+)\s+\((\w+)\)`)

// parseIbdev2netdev parses ibdev2netdev output rows.
func parseIbdev2netdev(out string) []ibdevMapping {
	var rows []ibdevMapping
	for _, line := range strings.Split(out, "\n") {
		if m := ibdev2netdevRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			rows = append(rows, ibdevMapping{RDMADev: m[1], Netdev: m[2], State: m[3]})
		}
	}
	return rows
}

// parseIPAddrShow parses "ip -4 -o addr show" rows
// ("2: enp1s0f0np0    inet 192.168.100.10/24 brd ... scope global ...")
// into interface -> CIDR list. The prefix length is kept so the analyzer can
// compare subnets; the address itself is redacted downstream.
func parseIPAddrShow(out string) map[string][]string {
	res := map[string][]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		iface := strings.TrimSuffix(fields[1], ":")
		for i := 2; i+1 < len(fields); i++ {
			if fields[i] == "inet" {
				res[iface] = append(res[iface], fields[i+1])
				break
			}
		}
	}
	return res
}

// ---------------------------------------------------------------------------
// Persistence (netplan / NetworkManager)
// ---------------------------------------------------------------------------

// netplanIface is what the minimal netplan scanner extracts per interface.
type netplanIface struct {
	MTU          int
	HasAddresses bool
	DHCP4        bool
}

// yamlKeyRe matches "key:" or "key: value" lines and captures indentation.
var yamlKeyRe = regexp.MustCompile(`^(\s*)([A-Za-z0-9_.\-]+):\s*(.*)$`)

// parseNetplan is a deliberately small YAML scanner for netplan files: it
// tracks the key path by indentation and records mtu / addresses / dhcp4 for
// every interface under "ethernets" or "bonds". Anchors, flow mappings and
// match-by-pattern stanzas are out of scope.
func parseNetplan(content string) map[string]netplanIface {
	res := map[string]netplanIface{}
	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	ifaceOf := func() string {
		for i, f := range stack {
			if (f.key == "ethernets" || f.key == "bonds") && i+1 < len(stack) {
				return stack[i+1].key
			}
		}
		return ""
	}
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			if iface := ifaceOf(); iface != "" && len(stack) > 0 && stack[len(stack)-1].key == "addresses" {
				e := res[iface]
				e.HasAddresses = true
				res[iface] = e
			}
			continue
		}
		m := yamlKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent, key, value := len(m[1]), m[2], strings.TrimSpace(m[3])
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, frame{indent: indent, key: key})
		iface := ifaceOf()
		if iface == "" || iface == key {
			if iface == key {
				if _, ok := res[iface]; !ok {
					res[iface] = netplanIface{}
				}
			}
			continue
		}
		e := res[iface]
		switch key {
		case "mtu":
			if n, err := strconv.Atoi(value); err == nil {
				e.MTU = n
			}
		case "addresses":
			if strings.HasPrefix(value, "[") && strings.TrimSpace(strings.Trim(value, "[]")) != "" {
				e.HasAddresses = true
			}
		case "dhcp4":
			e.DHCP4 = strings.EqualFold(value, "true") || value == "yes"
		}
		res[iface] = e
	}
	return res
}

// loadNetplan merges every /etc/netplan/*.yaml (through simPath).
func loadNetplan() map[string]netplanIface {
	merged := map[string]netplanIface{}
	files, _ := filepath.Glob(filepath.Join(simPath(netplanDir), "*.yaml"))
	more, _ := filepath.Glob(filepath.Join(simPath(netplanDir), "*.yml"))
	files = append(files, more...)
	sort.Strings(files)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for k, v := range parseNetplan(string(data)) {
			cur := merged[k]
			if v.MTU != 0 {
				cur.MTU = v.MTU
			}
			cur.HasAddresses = cur.HasAddresses || v.HasAddresses
			cur.DHCP4 = cur.DHCP4 || v.DHCP4
			merged[k] = cur
		}
	}
	return merged
}

// nmInterfaceNames returns the interface-name values of NetworkManager
// keyfiles (through simPath), the other place an address can be persisted.
func nmInterfaceNames() map[string]bool {
	names := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(simPath(nmConnectionsDir), "*"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if k, v := util.ParseKeyValue(line, "="); k == "interface-name" && v != "" {
				names[v] = true
			}
		}
	}
	return names
}

// applyNetplan marks ports whose configuration is persisted and records the
// netplan MTU of the fabric ports (first non-zero value).
func applyNetplan(info *types.ClusterInfo) {
	np := loadNetplan()
	nm := nmInterfaceNames()
	for i := range info.Ports {
		p := &info.Ports[i]
		if p.Netdev == "" {
			continue
		}
		if e, ok := np[p.Netdev]; ok {
			p.Persistent = e.HasAddresses || e.DHCP4 || e.MTU != 0
			if info.NetplanMTU == 0 && e.MTU != 0 {
				info.NetplanMTU = e.MTU
			}
		}
		if nm[p.Netdev] {
			p.Persistent = true
		}
	}
}

// ---------------------------------------------------------------------------
// NCCL
// ---------------------------------------------------------------------------

// ncclEnvironment keeps the NCCL_* / UCX_* variables of an environment list.
func ncclEnvironment(environ []string) map[string]string {
	env := map[string]string{}
	for _, kv := range environ {
		k, v := util.ParseKeyValue(kv, "=")
		for _, prefix := range ncclEnvPrefixes {
			if strings.HasPrefix(k, prefix) {
				env[k] = v
			}
		}
	}
	if len(env) == 0 {
		return nil
	}
	return env
}

// libDirs are the library locations searched when ldconfig is unavailable.
var libDirs = []string{"/usr/lib/aarch64-linux-gnu", "/usr/lib/x86_64-linux-gnu", "/usr/lib64", "/usr/lib", "/usr/local/lib", "/usr/local/cuda/lib64"}

// ncclVersionRe extracts the version from a resolved libnccl.so.2.28.3 name.
var ncclVersionRe = regexp.MustCompile(`libnccl\.so\.(\d+(?:\.\d+)+)`)

// parseLdconfigPaths returns the paths of ldconfig -p entries whose library
// name contains needle.
func parseLdconfigPaths(out, needle string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && strings.HasPrefix(fields[len(fields)-1], "/") {
			paths = append(paths, fields[len(fields)-1])
		}
	}
	return paths
}

// ncclVersionFromPath resolves a libnccl.so.2 path through its symlinks and
// reads the version from the final file name.
func ncclVersionFromPath(path string) string {
	candidates := []string{path}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		candidates = append([]string{resolved}, candidates...)
	}
	for _, c := range candidates {
		if m := ncclVersionRe.FindStringSubmatch(filepath.Base(c)); m != nil {
			return m[1]
		}
	}
	return ""
}

// collectNCCLLibs records the libnccl.so.2 version and the first NCCL net
// plugin (libnccl-net*.so) found via ldconfig or the usual library dirs
// (rule nccl-env-misconfigured: plugin present without NCCL_NET_PLUGIN=none).
func collectNCCLLibs(info *types.ClusterInfo, timeout int) {
	var ncclPaths, pluginPaths []string
	if util.CommandExists("ldconfig") {
		r := util.RunCommand(timeout, "ldconfig", "-p")
		ncclPaths = parseLdconfigPaths(r.Stdout, "libnccl.so.2")
		pluginPaths = parseLdconfigPaths(r.Stdout, "libnccl-net")
	}
	if len(ncclPaths) == 0 || len(pluginPaths) == 0 {
		for _, dir := range libDirs {
			if len(ncclPaths) == 0 {
				m, _ := filepath.Glob(filepath.Join(simPath(dir), "libnccl.so.2*"))
				ncclPaths = append(ncclPaths, m...)
			}
			if len(pluginPaths) == 0 {
				m, _ := filepath.Glob(filepath.Join(simPath(dir), "libnccl-net*.so*"))
				pluginPaths = append(pluginPaths, m...)
			}
		}
	}
	for _, p := range ncclPaths {
		if v := ncclVersionFromPath(p); v != "" {
			info.NCCLVersion = v
			break
		}
	}
	if len(pluginPaths) > 0 {
		sort.Strings(pluginPaths)
		info.NCCLPluginLib = pluginPaths[0]
	}
}

// peermemAttempted reports whether nvidia_peermem was loaded or its load was
// attempted (sysfs module dir, or a dmesg line naming it; S69).
func peermemAttempted(timeout int) bool {
	if simFileExists("/sys/module/" + peermemModule) {
		return true
	}
	if !util.CommandExists("dmesg") {
		return false
	}
	r := util.RunCommand(timeout, "dmesg")
	return strings.Contains(r.Stdout, peermemModule)
}

// ---------------------------------------------------------------------------
// avahi / ufw
// ---------------------------------------------------------------------------

// countAvahiConflicts counts journal lines carrying the hostname-conflict
// marker.
func countAvahiConflicts(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, avahiConflictMarker) {
			n++
		}
	}
	return n
}

func collectAvahi(info *types.ClusterInfo, timeout int) {
	info.AvahiActive = unitState(timeout, avahiUnit) == "active"
	if !util.CommandExists("journalctl") {
		return
	}
	r := util.RunCommand(timeout, "journalctl", "-u", avahiUnit, "-b", "--no-pager", "-q", "-g", avahiConflictMarker)
	info.AvahiConflicts = countAvahiConflicts(r.Stdout)
}

// parseUfwEnabled reads ENABLED=yes from /etc/ufw/ufw.conf contents.
func parseUfwEnabled(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if k, v := util.ParseKeyValue(line, "="); k == "ENABLED" {
			return strings.EqualFold(strings.TrimSpace(v), "yes")
		}
	}
	return false
}

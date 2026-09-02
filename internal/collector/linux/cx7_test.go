package linux

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCageOf(t *testing.T) {
	cases := []struct {
		pci, netdev string
		want        int
	}{
		// spec 2.1 Networking: port 0 = function .0 of domains 0000 and 0002
		{"0000:01:00.0", "enp1s0f0np0", 0},
		{"0002:01:00.0", "enP2p1s0f0np0", 0},
		{"0000:01:00.1", "enp1s0f1np1", 1},
		{"0002:01:00.1", "enP2p1s0f1np1", 1},
		{"", "enP2p1s0f1np1", 1},
		{"", "rocep1s0f0", 0},
		{"", "eth0", -1},
	}
	for _, c := range cases {
		if got := cageOf(c.pci, c.netdev); got != c.want {
			t.Errorf("cageOf(%q,%q) = %d, want %d", c.pci, c.netdev, got, c.want)
		}
	}
}

func TestRateToMbps(t *testing.T) {
	cases := map[string]int{
		"200 Gb/sec (4X NDR)": 200000, // spec 9 healthy rate
		"100 Gb/sec (4X EDR)": 100000,
		"2.5 Gb/sec (1X SDR)": 2500,
		"":                    0,
		"unknown":             0,
	}
	for in, want := range cases {
		if got := rateToMbps(in); got != want {
			t.Errorf("rateToMbps(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseIbdev2netdev(t *testing.T) {
	// gb10 scenario ibdev2netdev_lines (spec 2.1 device names)
	out := "rocep1s0f0 port 1 ==> enp1s0f0np0 (Down)\n" +
		"rocep1s0f1 port 1 ==> enp1s0f1np1 (Down)\n" +
		"roceP2p1s0f0 port 1 ==> enP2p1s0f0np0 (Up)\n" +
		"roceP2p1s0f1 port 1 ==> enP2p1s0f1np1 (Down)\n"
	rows := parseIbdev2netdev(out)
	if len(rows) != 4 {
		t.Fatalf("parsed %d rows, want 4", len(rows))
	}
	if rows[2] != (ibdevMapping{RDMADev: "roceP2p1s0f0", Netdev: "enP2p1s0f0np0", State: "Up"}) {
		t.Errorf("row 2 = %+v", rows[2])
	}
}

func TestParseIPAddrShow(t *testing.T) {
	out := "1: lo    inet 127.0.0.1/8 scope host lo\\       valid_lft forever preferred_lft forever\n" +
		"2: enp1s0f0np0    inet 192.168.100.10/24 brd 192.168.100.255 scope global enp1s0f0np0\\       valid_lft forever preferred_lft forever\n" +
		"3: enP2p1s0f0np0    inet 192.168.101.10/24 brd 192.168.101.255 scope global enP2p1s0f0np0\\       valid_lft forever preferred_lft forever\n" +
		"3: enP2p1s0f0np0    inet 192.168.101.11/24 scope global secondary enP2p1s0f0np0\\       valid_lft forever preferred_lft forever\n"
	got := parseIPAddrShow(out)
	want := map[string][]string{
		"lo":            {"127.0.0.1/8"},
		"enp1s0f0np0":   {"192.168.100.10/24"},
		"enP2p1s0f0np0": {"192.168.101.10/24", "192.168.101.11/24"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseIPAddrShow = %v, want %v", got, want)
	}
}

const netplanFixture = `network:
  version: 2
  renderer: networkd
  ethernets:
    enp1s0f0np0:
      mtu: 9000
      addresses:
        - 192.168.100.10/24
    enP2p1s0f0np0:
      mtu: 9000
      addresses: [192.168.101.10/24]
    enP7s7:
      dhcp4: true
  bonds:
    bond0:
      interfaces: [enp1s0f1np1, enP2p1s0f1np1]
      mtu: 1500
`

func TestParseNetplan(t *testing.T) {
	got := parseNetplan(netplanFixture)
	want := map[string]netplanIface{
		"enp1s0f0np0":   {MTU: 9000, HasAddresses: true},
		"enP2p1s0f0np0": {MTU: 9000, HasAddresses: true},
		"enP7s7":        {DHCP4: true},
		"bond0":         {MTU: 1500},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseNetplan = %+v, want %+v", got, want)
	}
}

func TestNcclEnvironment(t *testing.T) {
	env := ncclEnvironment([]string{
		"PATH=/usr/bin",
		"NCCL_IB_HCA=rocep1s0f0,roceP2p1s0f0", // spec 9 healthy NCCL_IB_HCA
		"NCCL_NET_PLUGIN=none",
		"UCX_NET_DEVICES=rocep1s0f0:1",
		"HOME=/home/x",
	})
	want := map[string]string{
		"NCCL_IB_HCA":     "rocep1s0f0,roceP2p1s0f0",
		"NCCL_NET_PLUGIN": "none",
		"UCX_NET_DEVICES": "rocep1s0f0:1",
	}
	if !reflect.DeepEqual(env, want) {
		t.Errorf("ncclEnvironment = %v, want %v", env, want)
	}
	if ncclEnvironment([]string{"PATH=/usr/bin"}) != nil {
		t.Error("no NCCL vars must give nil")
	}
}

func TestParseLdconfigPathsAndNCCLVersion(t *testing.T) {
	out := "\tlibnccl.so.2 (libc6,AArch64) => /usr/lib/aarch64-linux-gnu/libnccl.so.2\n" +
		"\tlibnccl-net-ibext.so (libc6,AArch64) => /usr/lib/aarch64-linux-gnu/libnccl-net-ibext.so\n" +
		"\tlibcuda.so.1 (libc6,AArch64) => /usr/lib/aarch64-linux-gnu/libcuda.so.1\n"
	if got := parseLdconfigPaths(out, "libnccl.so.2"); !reflect.DeepEqual(got, []string{"/usr/lib/aarch64-linux-gnu/libnccl.so.2"}) {
		t.Errorf("libnccl paths = %v", got)
	}
	if got := parseLdconfigPaths(out, "libnccl-net"); !reflect.DeepEqual(got, []string{"/usr/lib/aarch64-linux-gnu/libnccl-net-ibext.so"}) {
		t.Errorf("plugin paths = %v", got)
	}
	if got := ncclVersionFromPath("/nonexistent/libnccl.so.2.28.3"); got != "2.28.3" {
		t.Errorf("nccl version from name = %q", got)
	}
}

func TestParseUfwEnabledAndAvahi(t *testing.T) {
	if !parseUfwEnabled("# /etc/ufw/ufw.conf\nENABLED=yes\nLOGLEVEL=low\n") {
		t.Error("ENABLED=yes must be true")
	}
	if parseUfwEnabled("ENABLED=no\n") || parseUfwEnabled("") {
		t.Error("ENABLED=no / empty must be false")
	}
	journal := "Sep 01 10:00:00 spark avahi-daemon[900]: Host name conflict, retrying with spark-2\n" +
		"Sep 01 10:00:05 spark avahi-daemon[900]: Host name conflict, retrying with spark-3\n"
	if got := countAvahiConflicts(journal); got != 2 {
		t.Errorf("avahi conflicts = %d, want 2", got)
	}
}

func TestDiscoverFabricPortsFromSimRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv(simRootEnv, root)

	// Twin 0 of cage 0 (domain 0000), Up with RDMA device (spec 2.1 names).
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/operstate", "up\n")
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/speed", "200000\n")
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/mtu", "9000\n")
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/device/vendor", "0x15b3\n")
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/device/device", "0x1021\n")
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/device/uevent", "DRIVER=mlx5_core\nPCI_SLOT_NAME=0000:01:00.0\n")
	writeFixture(t, root, "sys/class/infiniband/rocep1s0f0/ports/1/state", "4: ACTIVE\n")
	writeFixture(t, root, "sys/class/infiniband/rocep1s0f0/ports/1/phys_state", "5: LinkUp\n")
	writeFixture(t, root, "sys/class/infiniband/rocep1s0f0/ports/1/rate", "200 Gb/sec (4X NDR)\n")
	writeFixture(t, root, "sys/class/infiniband/rocep1s0f0/device/uevent", "PCI_SLOT_NAME=0000:01:00.0\n")
	if err := os.MkdirAll(filepath.Join(root, "sys/class/infiniband/rocep1s0f0/device/net/enp1s0f0np0"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Twin 1 of cage 0 (domain 0002), Down, 1500 MTU, no RDMA device.
	writeFixture(t, root, "sys/class/net/enP2p1s0f0np0/operstate", "down\n")
	writeFixture(t, root, "sys/class/net/enP2p1s0f0np0/mtu", "1500\n")
	writeFixture(t, root, "sys/class/net/enP2p1s0f0np0/device/vendor", "0x15b3\n")
	writeFixture(t, root, "sys/class/net/enP2p1s0f0np0/device/uevent", "PCI_SLOT_NAME=0002:01:00.0\n")
	// Management NIC (Realtek) must be ignored.
	writeFixture(t, root, "sys/class/net/enP7s7/operstate", "up\n")
	writeFixture(t, root, "sys/class/net/enP7s7/device/vendor", "0x10ec\n")
	writeFixture(t, root, "etc/nvidia/cx7-hotplug-enabled", "")
	writeFixture(t, root, "etc/ufw/ufw.conf", "ENABLED=yes\n")
	writeFixture(t, root, "etc/netplan/50-cloud-init.yaml", netplanFixture)

	info, _ := CollectCluster(5)
	if len(info.Ports) != 2 {
		t.Fatalf("ports = %+v, want 2", info.Ports)
	}
	p0, p1 := info.Ports[0], info.Ports[1]
	if p0.Netdev != "enp1s0f0np0" || p0.RDMADev != "rocep1s0f0" || p0.PCIAddr != "0000:01:00.0" || p0.Cage != 0 {
		t.Errorf("port 0 identity = %+v", p0)
	}
	if p0.State != "4: ACTIVE" || p0.PhysState != "5: LinkUp" || p0.SpeedMbps != 200000 || p0.MTU != 9000 {
		t.Errorf("port 0 link = %+v", p0)
	}
	if !p0.Persistent {
		t.Error("port 0 is in netplan and must be Persistent")
	}
	if p1.Netdev != "enP2p1s0f0np0" || p1.PCIAddr != "0002:01:00.0" || p1.Cage != 0 || p1.State != "down" || p1.MTU != 1500 {
		t.Errorf("port 1 = %+v", p1)
	}
	if !info.HotplugFileEnabled || !info.UfwEnabled {
		t.Errorf("hotplug=%v ufw=%v, want both true", info.HotplugFileEnabled, info.UfwEnabled)
	}
	if info.NetplanMTU != 9000 {
		t.Errorf("NetplanMTU = %d, want 9000", info.NetplanMTU)
	}
}

// TestDiscoverFabricPortsWithoutDeviceNet: when /sys/class/infiniband/<dev>/
// device/net is absent (and ibdev2netdev is not on PATH) the netdev and the
// RDMA device of one function must still merge into one FabricPort by PCI
// address, or cx7-twin-link-mismatch would see a phantom third port.
func TestDiscoverFabricPortsWithoutDeviceNet(t *testing.T) {
	root := t.TempDir()
	t.Setenv(simRootEnv, root)
	t.Setenv("PATH", "")

	writeFixture(t, root, "sys/class/net/enp1s0f0np0/operstate", "up\n")
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/device/vendor", "0x15b3\n")
	writeFixture(t, root, "sys/class/net/enp1s0f0np0/device/uevent", "PCI_SLOT_NAME=0000:01:00.0\n")
	writeFixture(t, root, "sys/class/infiniband/rocep1s0f0/ports/1/state", "4: ACTIVE\n")
	writeFixture(t, root, "sys/class/infiniband/rocep1s0f0/ports/1/rate", "200 Gb/sec (4X NDR)\n")
	writeFixture(t, root, "sys/class/infiniband/rocep1s0f0/device/uevent", "PCI_SLOT_NAME=0000:01:00.0\n")
	// Second function of the same cage keeps its own entry.
	writeFixture(t, root, "sys/class/net/enP2p1s0f0np0/operstate", "down\n")
	writeFixture(t, root, "sys/class/net/enP2p1s0f0np0/device/vendor", "0x15b3\n")
	writeFixture(t, root, "sys/class/net/enP2p1s0f0np0/device/uevent", "PCI_SLOT_NAME=0002:01:00.0\n")
	writeFixture(t, root, "sys/class/infiniband/rocep2p1s0f0/ports/1/state", "1: DOWN\n")
	writeFixture(t, root, "sys/class/infiniband/rocep2p1s0f0/device/uevent", "PCI_SLOT_NAME=0002:01:00.0\n")

	ports := discoverFabricPorts(5)
	if len(ports) != 2 {
		t.Fatalf("ports = %+v, want 2 (one per PCI function)", ports)
	}
	if ports[0].Netdev != "enp1s0f0np0" || ports[0].RDMADev != "rocep1s0f0" || ports[0].PCIAddr != "0000:01:00.0" || ports[0].State != "4: ACTIVE" {
		t.Errorf("port 0 = %+v", ports[0])
	}
	if ports[1].Netdev != "enP2p1s0f0np0" || ports[1].RDMADev != "rocep2p1s0f0" || ports[1].PCIAddr != "0002:01:00.0" || ports[1].State != "1: DOWN" {
		t.Errorf("port 1 = %+v", ports[1])
	}
}

package monitor

import (
	"testing"

	gopsnet "github.com/shirou/gopsutil/v4/net"
)

func TestMergeNetworkStatsIncludesInterfaceAddresses(t *testing.T) {
	t.Parallel()

	stats := mergeNetworkStats(
		[]gopsnet.IOCountersStat{{
			Name:        "Ethernet",
			BytesSent:   100,
			BytesRecv:   200,
			PacketsSent: 3,
			PacketsRecv: 4,
		}},
		[]gopsnet.InterfaceStat{{
			Name:         "Ethernet",
			HardwareAddr: "00-11-22-33-44-55",
			MTU:          1500,
			Flags:        []string{"up", "broadcast", "multicast"},
			Addrs: []gopsnet.InterfaceAddr{
				{Addr: "192.168.1.10/24"},
				{Addr: "fe80::1234/64"},
			},
		}},
	)

	if len(stats.Interfaces) != 1 {
		t.Fatalf("interfaces = %+v", stats.Interfaces)
	}
	iface := stats.Interfaces[0]
	if iface.Name != "Ethernet" || iface.HardwareAddr != "00-11-22-33-44-55" || !iface.IsUp || iface.MTU != 1500 {
		t.Fatalf("interface metadata = %+v", iface)
	}
	if iface.BytesSent != 100 || iface.BytesRecv != 200 || iface.PacketsSent != 3 || iface.PacketsRecv != 4 {
		t.Fatalf("interface counters = %+v", iface)
	}
	if len(iface.IPAddresses) != 2 {
		t.Fatalf("ip addresses = %+v", iface.IPAddresses)
	}
	if iface.IPAddresses[0].Address != "192.168.1.10" || iface.IPAddresses[0].Family != "ipv4" || iface.IPAddresses[0].CIDR != "192.168.1.10/24" {
		t.Fatalf("ipv4 address = %+v", iface.IPAddresses[0])
	}
	if iface.IPAddresses[1].Address != "fe80::1234" || iface.IPAddresses[1].Family != "ipv6" || iface.IPAddresses[1].CIDR != "fe80::1234/64" {
		t.Fatalf("ipv6 address = %+v", iface.IPAddresses[1])
	}
}

func TestMergeNetworkStatsIncludesInterfacesWithoutCounters(t *testing.T) {
	t.Parallel()

	stats := mergeNetworkStats(nil, []gopsnet.InterfaceStat{{
		Name:  "Loopback",
		Flags: []string{"up", "loopback"},
		Addrs: []gopsnet.InterfaceAddr{{Addr: "127.0.0.1/8"}},
	}})

	if len(stats.Interfaces) != 1 {
		t.Fatalf("interfaces = %+v", stats.Interfaces)
	}
	iface := stats.Interfaces[0]
	if iface.Name != "Loopback" || !iface.IsUp {
		t.Fatalf("interface = %+v", iface)
	}
	if len(iface.IPAddresses) != 1 || iface.IPAddresses[0].Address != "127.0.0.1" {
		t.Fatalf("ip addresses = %+v", iface.IPAddresses)
	}
	if iface.BytesRecv != 0 || iface.BytesSent != 0 {
		t.Fatalf("counters = %+v", iface)
	}
}

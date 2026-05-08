package app

import (
	"net"
	"slices"
	"strconv"
	"strings"
)

type AccessEntry struct {
	Label string
	Host  string
	URL   string
}

func buildAccessEntries(bindHost string, port int, detected []string) []AccessEntry {
	host := strings.TrimSpace(bindHost)
	if host == "" || host == "0.0.0.0" || host == "::" {
		hosts := make([]string, 0, len(detected)+1)
		hosts = append(hosts, "127.0.0.1")
		hosts = append(hosts, detected...)
		return entriesForHosts(hosts, port)
	}
	return entriesForHosts([]string{host}, port)
}

func entriesForHosts(hosts []string, port int) []AccessEntry {
	seen := map[string]struct{}{}
	entries := make([]AccessEntry, 0, len(hosts))

	for _, host := range hosts {
		normalized := strings.TrimSpace(strings.Trim(host, "[]"))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}

		label := "局域网地址"
		if normalized == "127.0.0.1" || normalized == "localhost" {
			label = "本地地址"
		}
		entries = append(entries, AccessEntry{
			Label: label,
			Host:  normalized,
			URL:   "http://" + net.JoinHostPort(normalized, strconv.Itoa(port)),
		})
	}

	slices.SortFunc(entries, func(a, b AccessEntry) int {
		if a.Host == "127.0.0.1" && b.Host != "127.0.0.1" {
			return -1
		}
		if b.Host == "127.0.0.1" && a.Host != "127.0.0.1" {
			return 1
		}
		return strings.Compare(a.Host, b.Host)
	})

	return entries
}

func listIPv4Addresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	hosts := make([]string, 0)
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP == nil {
			continue
		}
		ip := ipNet.IP.To4()
		if ip == nil || ip.IsLoopback() {
			continue
		}
		hosts = append(hosts, ip.String())
	}

	slices.Sort(hosts)
	return hosts
}

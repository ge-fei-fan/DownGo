package monitor

import (
	"context"
	stdnet "net"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	gopsnet "github.com/shirou/gopsutil/v4/net"
)

type Collector interface {
	Snapshot(ctx context.Context) (Metrics, error)
}

type Metrics struct {
	Timestamp time.Time         `json:"timestamp"`
	CPU       *CPUStats         `json:"cpu,omitempty"`
	Memory    *MemoryStats      `json:"memory,omitempty"`
	Disks     []DiskStats       `json:"disks,omitempty"`
	Network   *NetworkStats     `json:"network,omitempty"`
	Host      *HostStats        `json:"host,omitempty"`
	Process   ProcessStats      `json:"process"`
	Errors    map[string]string `json:"errors,omitempty"`
}

type CPUStats struct {
	UsagePercent  float64 `json:"usagePercent"`
	LogicalCores  int     `json:"logicalCores"`
	PhysicalCores int     `json:"physicalCores"`
	ModelName     string  `json:"modelName"`
}

type MemoryStats struct {
	TotalBytes     uint64  `json:"totalBytes"`
	UsedBytes      uint64  `json:"usedBytes"`
	AvailableBytes uint64  `json:"availableBytes"`
	UsedPercent    float64 `json:"usedPercent"`
}

type DiskStats struct {
	Path        string  `json:"path"`
	FSType      string  `json:"fstype"`
	TotalBytes  uint64  `json:"totalBytes"`
	UsedBytes   uint64  `json:"usedBytes"`
	FreeBytes   uint64  `json:"freeBytes"`
	UsedPercent float64 `json:"usedPercent"`
}

type NetworkStats struct {
	Interfaces []NetworkInterfaceStats `json:"interfaces"`
}

type NetworkInterfaceStats struct {
	Name         string                `json:"name"`
	HardwareAddr string                `json:"hardwareAddr"`
	MTU          int                   `json:"mtu"`
	Flags        []string              `json:"flags"`
	IsUp         bool                  `json:"isUp"`
	IPAddresses  []NetworkAddressStats `json:"ipAddresses"`
	BytesSent    uint64                `json:"bytesSent"`
	BytesRecv    uint64                `json:"bytesRecv"`
	PacketsSent  uint64                `json:"packetsSent"`
	PacketsRecv  uint64                `json:"packetsRecv"`
}

type NetworkAddressStats struct {
	Address string `json:"address"`
	Family  string `json:"family"`
	CIDR    string `json:"cidr"`
}

type HostStats struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Platform      string `json:"platform"`
	UptimeSeconds uint64 `json:"uptimeSeconds"`
}

type ProcessStats struct {
	PID           int    `json:"pid"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	Goroutines    int    `json:"goroutines"`
	AllocBytes    uint64 `json:"allocBytes"`
	SysBytes      uint64 `json:"sysBytes"`
}

type GopsutilCollector struct {
	startedAt time.Time
}

func NewCollector(startedAt time.Time) *GopsutilCollector {
	return &GopsutilCollector{startedAt: startedAt}
}

func (c *GopsutilCollector) Snapshot(ctx context.Context) (Metrics, error) {
	result := Metrics{
		Timestamp: time.Now().UTC(),
		Process:   c.processStats(),
		Errors:    map[string]string{},
	}

	c.collectCPU(ctx, &result)
	c.collectMemory(ctx, &result)
	c.collectDisks(ctx, &result)
	c.collectNetwork(ctx, &result)
	c.collectHost(ctx, &result)

	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

func (c *GopsutilCollector) collectCPU(ctx context.Context, result *Metrics) {
	usagePercent := 0.0
	if percents, err := cpu.PercentWithContext(ctx, 200*time.Millisecond, false); err != nil {
		result.Errors["cpu"] = err.Error()
	} else if len(percents) > 0 {
		usagePercent = percents[0]
	}

	logicalCores, err := cpu.CountsWithContext(ctx, true)
	if err != nil {
		result.Errors["cpuLogicalCores"] = err.Error()
	}
	physicalCores, err := cpu.CountsWithContext(ctx, false)
	if err != nil {
		result.Errors["cpuPhysicalCores"] = err.Error()
	}

	modelName := ""
	if infos, err := cpu.InfoWithContext(ctx); err != nil {
		result.Errors["cpuInfo"] = err.Error()
	} else if len(infos) > 0 {
		modelName = infos[0].ModelName
	}

	if _, failed := result.Errors["cpu"]; !failed || logicalCores > 0 || physicalCores > 0 || modelName != "" {
		result.CPU = &CPUStats{
			UsagePercent:  usagePercent,
			LogicalCores:  logicalCores,
			PhysicalCores: physicalCores,
			ModelName:     modelName,
		}
	}
}

func (c *GopsutilCollector) collectMemory(ctx context.Context, result *Metrics) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		result.Errors["memory"] = err.Error()
		return
	}
	result.Memory = &MemoryStats{
		TotalBytes:     vm.Total,
		UsedBytes:      vm.Used,
		AvailableBytes: vm.Available,
		UsedPercent:    vm.UsedPercent,
	}
}

func (c *GopsutilCollector) collectDisks(ctx context.Context, result *Metrics) {
	items, errorsByGroup := collectPartitionsNative(ctx)
	result.Disks = items
	for key, value := range errorsByGroup {
		result.Errors[key] = value
	}
}

func (c *GopsutilCollector) collectNetwork(ctx context.Context, result *Metrics) {
	counters, err := gopsnet.IOCountersWithContext(ctx, true)
	if err != nil {
		result.Errors["network"] = err.Error()
	}
	interfaces, interfaceErr := gopsnet.InterfacesWithContext(ctx)
	if interfaceErr != nil {
		result.Errors["networkInterfaces"] = interfaceErr.Error()
	}
	stats := mergeNetworkStats(counters, interfaces)
	if len(stats.Interfaces) == 0 && err != nil && interfaceErr != nil {
		return
	}
	result.Network = &stats
}

func mergeNetworkStats(counters []gopsnet.IOCountersStat, interfaces []gopsnet.InterfaceStat) NetworkStats {
	byName := map[string]*NetworkInterfaceStats{}
	for _, counter := range counters {
		item := ensureNetworkInterface(byName, counter.Name)
		item.BytesSent = counter.BytesSent
		item.BytesRecv = counter.BytesRecv
		item.PacketsSent = counter.PacketsSent
		item.PacketsRecv = counter.PacketsRecv
	}
	for _, iface := range interfaces {
		item := ensureNetworkInterface(byName, iface.Name)
		item.HardwareAddr = iface.HardwareAddr
		item.MTU = iface.MTU
		item.Flags = append([]string(nil), iface.Flags...)
		item.IsUp = hasFlag(iface.Flags, "up")
		item.IPAddresses = networkAddresses(iface.Addrs)
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	stats := NetworkStats{Interfaces: make([]NetworkInterfaceStats, 0, len(names))}
	for _, name := range names {
		stats.Interfaces = append(stats.Interfaces, *byName[name])
	}
	return stats
}

func ensureNetworkInterface(byName map[string]*NetworkInterfaceStats, name string) *NetworkInterfaceStats {
	if item, ok := byName[name]; ok {
		return item
	}
	item := &NetworkInterfaceStats{Name: name, IPAddresses: []NetworkAddressStats{}}
	byName[name] = item
	return item
}

func networkAddresses(addrs []gopsnet.InterfaceAddr) []NetworkAddressStats {
	result := make([]NetworkAddressStats, 0, len(addrs))
	for _, addr := range addrs {
		ip := stdnet.ParseIP(addr.Addr)
		if ip == nil {
			ip, _, _ = stdnet.ParseCIDR(addr.Addr)
		}
		address := addr.Addr
		family := ""
		if ip != nil {
			address = ip.String()
			if ip.To4() != nil {
				family = "ipv4"
			} else {
				family = "ipv6"
			}
		}
		result = append(result, NetworkAddressStats{
			Address: address,
			Family:  family,
			CIDR:    addr.Addr,
		})
	}
	return result
}

func hasFlag(flags []string, target string) bool {
	for _, flag := range flags {
		if strings.EqualFold(flag, target) {
			return true
		}
	}
	return false
}

func (c *GopsutilCollector) collectHost(ctx context.Context, result *Metrics) {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		result.Errors["host"] = err.Error()
		return
	}
	result.Host = &HostStats{
		Hostname:      info.Hostname,
		OS:            info.OS,
		Platform:      info.Platform,
		UptimeSeconds: info.Uptime,
	}
}

func (c *GopsutilCollector) processStats() ProcessStats {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	return ProcessStats{
		PID:           pid(),
		UptimeSeconds: int64(time.Since(c.startedAt).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		AllocBytes:    memStats.Alloc,
		SysBytes:      memStats.Sys,
	}
}

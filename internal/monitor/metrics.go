package monitor

import (
	"context"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
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
	Name        string `json:"name"`
	BytesSent   uint64 `json:"bytesSent"`
	BytesRecv   uint64 `json:"bytesRecv"`
	PacketsSent uint64 `json:"packetsSent"`
	PacketsRecv uint64 `json:"packetsRecv"`
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
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		result.Errors["disks"] = err.Error()
		return
	}
	for _, partition := range partitions {
		usage, err := disk.UsageWithContext(ctx, partition.Mountpoint)
		if err != nil {
			result.Errors["disk:"+partition.Mountpoint] = err.Error()
			continue
		}
		result.Disks = append(result.Disks, DiskStats{
			Path:        partition.Mountpoint,
			FSType:      partition.Fstype,
			TotalBytes:  usage.Total,
			UsedBytes:   usage.Used,
			FreeBytes:   usage.Free,
			UsedPercent: usage.UsedPercent,
		})
	}
}

func (c *GopsutilCollector) collectNetwork(ctx context.Context, result *Metrics) {
	counters, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		result.Errors["network"] = err.Error()
		return
	}
	stats := NetworkStats{Interfaces: make([]NetworkInterfaceStats, 0, len(counters))}
	for _, counter := range counters {
		stats.Interfaces = append(stats.Interfaces, NetworkInterfaceStats{
			Name:        counter.Name,
			BytesSent:   counter.BytesSent,
			BytesRecv:   counter.BytesRecv,
			PacketsSent: counter.PacketsSent,
			PacketsRecv: counter.PacketsRecv,
		})
	}
	result.Network = &stats
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

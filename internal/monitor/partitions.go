package monitor

import (
	"context"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

type PartitionProvider interface {
	Partitions(ctx context.Context) (PartitionSnapshot, error)
}

type PartitionSnapshot struct {
	Timestamp time.Time         `json:"timestamp"`
	Items     []DiskStats       `json:"items"`
	Errors    map[string]string `json:"errors,omitempty"`
}

type PartitionService struct{}

func NewPartitionService() *PartitionService {
	return &PartitionService{}
}

func (s *PartitionService) Partitions(ctx context.Context) (PartitionSnapshot, error) {
	items, errorsByGroup := collectPartitionsNative(ctx)
	result := PartitionSnapshot{
		Timestamp: time.Now().UTC(),
		Items:     items,
		Errors:    errorsByGroup,
	}
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

type partitionsFunc func(context.Context, bool) ([]disk.PartitionStat, error)
type usageFunc func(context.Context, string) (*disk.UsageStat, error)

type partitionMountpoint struct {
	Path   string
	FSType string
}

func collectPartitions(ctx context.Context, partitions partitionsFunc, usage usageFunc) ([]DiskStats, map[string]string) {
	errorsByGroup := map[string]string{}
	partitionStats, err := partitions(ctx, false)
	if err != nil {
		errorsByGroup["partitions"] = err.Error()
		return []DiskStats{}, errorsByGroup
	}

	mountpoints := make([]partitionMountpoint, 0, len(partitionStats))
	for _, partition := range partitionStats {
		mountpoints = append(mountpoints, partitionMountpoint{
			Path:   partition.Mountpoint,
			FSType: partition.Fstype,
		})
	}
	return collectPartitionMountpoints(ctx, mountpoints, usage)
}

func collectPartitionMountpoints(ctx context.Context, mountpoints []partitionMountpoint, usage usageFunc) ([]DiskStats, map[string]string) {
	errorsByGroup := map[string]string{}
	items := make([]DiskStats, 0, len(mountpoints))
	for _, mountpoint := range mountpoints {
		usageStats, err := usage(ctx, mountpoint.Path)
		if err != nil {
			errorsByGroup["partition:"+mountpoint.Path] = err.Error()
			continue
		}
		fstype := mountpoint.FSType
		if fstype == "" {
			fstype = usageStats.Fstype
		}
		items = append(items, DiskStats{
			Path:        mountpoint.Path,
			FSType:      fstype,
			TotalBytes:  usageStats.Total,
			UsedBytes:   usageStats.Used,
			FreeBytes:   usageStats.Free,
			UsedPercent: usageStats.UsedPercent,
		})
	}
	return items, errorsByGroup
}

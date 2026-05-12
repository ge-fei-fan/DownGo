//go:build !windows

package monitor

import (
	"context"

	"github.com/shirou/gopsutil/v4/disk"
)

func collectPartitionsNative(ctx context.Context) ([]DiskStats, map[string]string) {
	return collectPartitions(ctx, disk.PartitionsWithContext, disk.UsageWithContext)
}

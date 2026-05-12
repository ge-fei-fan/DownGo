package monitor

import (
	"context"
	"errors"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestCollectPartitionsReturnsUsageAndErrors(t *testing.T) {
	t.Parallel()

	items, errorsByGroup := collectPartitions(
		context.Background(),
		func(context.Context, bool) ([]disk.PartitionStat, error) {
			return []disk.PartitionStat{
				{Mountpoint: "C:\\", Fstype: "NTFS"},
				{Mountpoint: "X:\\", Fstype: "NTFS"},
			}, nil
		},
		func(_ context.Context, path string) (*disk.UsageStat, error) {
			if path == "X:\\" {
				return nil, errors.New("access denied")
			}
			return &disk.UsageStat{
				Path:        path,
				Fstype:      "NTFS",
				Total:       2048,
				Used:        1024,
				Free:        1024,
				UsedPercent: 50,
			}, nil
		},
	)

	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Path != "C:\\" || items[0].TotalBytes != 2048 || items[0].UsedBytes != 1024 || items[0].FreeBytes != 1024 || items[0].UsedPercent != 50 {
		t.Fatalf("item = %+v", items[0])
	}
	if errorsByGroup["partition:X:\\"] != "access denied" {
		t.Fatalf("errors = %+v", errorsByGroup)
	}
}

func TestCollectPartitionsReturnsListError(t *testing.T) {
	t.Parallel()

	items, errorsByGroup := collectPartitions(
		context.Background(),
		func(context.Context, bool) ([]disk.PartitionStat, error) {
			return nil, errors.New("list failed")
		},
		func(context.Context, string) (*disk.UsageStat, error) {
			t.Fatal("usage should not be called")
			return nil, nil
		},
	)

	if len(items) != 0 {
		t.Fatalf("items = %+v", items)
	}
	if items == nil {
		t.Fatal("items is nil")
	}
	if errorsByGroup["partitions"] != "list failed" {
		t.Fatalf("errors = %+v", errorsByGroup)
	}
}

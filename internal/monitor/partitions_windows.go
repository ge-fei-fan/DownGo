//go:build windows

package monitor

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/disk"
	"golang.org/x/sys/windows"
)

func collectPartitionsNative(ctx context.Context) ([]DiskStats, map[string]string) {
	mountpoints, errorsByGroup := windowsPartitionMountpoints()
	items, usageErrors := collectPartitionMountpoints(ctx, mountpoints, disk.UsageWithContext)
	for key, value := range usageErrors {
		errorsByGroup[key] = value
	}
	return items, errorsByGroup
}

func windowsPartitionMountpoints() ([]partitionMountpoint, map[string]string) {
	errorsByGroup := map[string]string{}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		errorsByGroup["partitions"] = err.Error()
		return []partitionMountpoint{}, errorsByGroup
	}

	mountpoints := make([]partitionMountpoint, 0, 26)
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := fmt.Sprintf("%c:\\", 'A'+i)
		rootPtr, err := windows.UTF16PtrFromString(root)
		if err != nil {
			errorsByGroup["partition:"+root] = err.Error()
			continue
		}
		if !isPartitionDriveType(windows.GetDriveType(rootPtr)) {
			continue
		}

		fstype, err := windowsVolumeFSType(root)
		if err != nil {
			errorsByGroup["partitionFSType:"+root] = err.Error()
		}
		mountpoints = append(mountpoints, partitionMountpoint{
			Path:   root,
			FSType: fstype,
		})
	}
	return mountpoints, errorsByGroup
}

func isPartitionDriveType(driveType uint32) bool {
	return driveType == windows.DRIVE_FIXED ||
		driveType == windows.DRIVE_REMOVABLE ||
		driveType == windows.DRIVE_REMOTE ||
		driveType == windows.DRIVE_RAMDISK
}

func windowsVolumeFSType(root string) (string, error) {
	rootPtr, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", err
	}
	var filesystem [windows.MAX_PATH + 1]uint16
	err = windows.GetVolumeInformation(rootPtr, nil, 0, nil, nil, nil, &filesystem[0], uint32(len(filesystem)))
	if err != nil {
		return "", err
	}
	return windows.UTF16ToString(filesystem[:]), nil
}

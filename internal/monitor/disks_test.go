package monitor

import (
	"context"
	"testing"
	"time"
)

func TestParsePhysicalDiskJSONList(t *testing.T) {
	t.Parallel()

	disks, err := parsePhysicalDiskJSON([]byte(`[
		{"DeviceId":0,"FriendlyName":"NVMe Drive","SerialNumber":"SN1","MediaType":"SSD","BusType":"NVMe","HealthStatus":"Healthy","OperationalStatus":"OK","Size":1024,"Temperature":36},
		{"DeviceId":1,"FriendlyName":"Data HDD","SerialNumber":"SN2","MediaType":"HDD","BusType":"SATA","HealthStatus":"Healthy","OperationalStatus":"OK","Size":2048,"Temperature":null}
	]`))
	if err != nil {
		t.Fatalf("parsePhysicalDiskJSON() error = %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("len(disks) = %d, want 2", len(disks))
	}
	if disks[0].DeviceID != "0" || disks[0].FriendlyName != "NVMe Drive" || disks[0].SizeBytes != 1024 {
		t.Fatalf("first disk = %+v", disks[0])
	}
	if disks[0].TemperatureCelsius == nil || *disks[0].TemperatureCelsius != 36 {
		t.Fatalf("first disk temperature = %+v", disks[0].TemperatureCelsius)
	}
	if disks[1].TemperatureCelsius != nil || disks[1].TemperatureError == "" {
		t.Fatalf("second disk temperature state = %+v", disks[1])
	}
}

func TestParsePhysicalDiskJSONSingleObject(t *testing.T) {
	t.Parallel()

	disks, err := parsePhysicalDiskJSON([]byte(`{"DeviceId":"2","FriendlyName":"USB Disk","SerialNumber":"SN3","MediaType":"Unspecified","BusType":"USB","HealthStatus":"Healthy","OperationalStatus":"OK","Size":"4096","Temperature":"41"}`))
	if err != nil {
		t.Fatalf("parsePhysicalDiskJSON() error = %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("len(disks) = %d, want 1", len(disks))
	}
	if disks[0].DeviceID != "2" || disks[0].SizeBytes != 4096 {
		t.Fatalf("disk = %+v", disks[0])
	}
	if disks[0].TemperatureCelsius == nil || *disks[0].TemperatureCelsius != 41 {
		t.Fatalf("temperature = %+v", disks[0].TemperatureCelsius)
	}
}

func TestDiskServiceRefreshTemperaturesCachesSnapshot(t *testing.T) {
	t.Parallel()

	temp := 38
	service := NewDiskService(time.Minute)
	service.physical = func(context.Context) ([]PhysicalDiskStats, map[string]string) {
		return []PhysicalDiskStats{{
			DeviceID:           "0",
			FriendlyName:       "Test Disk",
			SerialNumber:       "SN",
			TemperatureCelsius: &temp,
		}}, map[string]string{"physicalDisks": "partial warning"}
	}

	service.RefreshTemperatures(context.Background())
	snapshot, err := service.DiskTemperatures(context.Background())
	if err != nil {
		t.Fatalf("DiskTemperatures() error = %v", err)
	}
	if snapshot.UpdatedAt == nil || snapshot.NextRefreshAt == nil {
		t.Fatalf("timestamps not set: %+v", snapshot)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].TemperatureCelsius == nil || *snapshot.Items[0].TemperatureCelsius != 38 {
		t.Fatalf("items = %+v", snapshot.Items)
	}
	if snapshot.Errors["physicalDisks"] != "partial warning" {
		t.Fatalf("errors = %+v", snapshot.Errors)
	}
}

func TestDiskServiceDisksReturnsPhysicalDisksWithCachedTemperatures(t *testing.T) {
	t.Parallel()

	temp := 39
	service := NewDiskService(time.Minute)
	service.physical = func(context.Context) ([]PhysicalDiskStats, map[string]string) {
		return []PhysicalDiskStats{{
			DeviceID:           "0",
			FriendlyName:       "Test Disk",
			SerialNumber:       "SN",
			TemperatureCelsius: &temp,
		}}, nil
	}

	service.RefreshTemperatures(context.Background())
	snapshot, err := service.Disks(context.Background())
	if err != nil {
		t.Fatalf("Disks() error = %v", err)
	}
	if len(snapshot.PhysicalDisks) != 1 {
		t.Fatalf("physical disks = %+v", snapshot.PhysicalDisks)
	}
	if snapshot.PhysicalDisks[0].TemperatureCelsius == nil || *snapshot.PhysicalDisks[0].TemperatureCelsius != 39 {
		t.Fatalf("cached temperature = %+v", snapshot.PhysicalDisks[0])
	}
	if snapshot.TemperatureUpdatedAt == nil || snapshot.NextRefreshAt == nil {
		t.Fatalf("temperature cache timestamps = %+v", snapshot)
	}
}

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

func TestParseSmartctlScanText(t *testing.T) {
	t.Parallel()

	devices := parseSmartctlScan([]byte(`/dev/sda -d sat # /dev/sda, ATA device
/dev/sdb -d usbjmicron # /dev/sdb, USB device`))
	if len(devices) != 2 {
		t.Fatalf("devices = %+v", devices)
	}
	if devices[0].Name != "/dev/sda" || devices[0].Type != "sat" {
		t.Fatalf("first device = %+v", devices[0])
	}
}

func TestParseSmartctlDiskJSONReturnsHDDTemperature(t *testing.T) {
	t.Parallel()

	disk, err := parseSmartctlDiskJSON([]byte(`{
		"device":{"name":"/dev/sda","protocol":"ATA"},
		"model_name":"Data HDD",
		"serial_number":"SN-HDD",
		"rotation_rate":7200,
		"user_capacity":{"bytes":1000},
		"temperature":{"current":37}
	}`))
	if err != nil {
		t.Fatalf("parseSmartctlDiskJSON() error = %v", err)
	}
	if disk.MediaType != "HDD" || disk.TemperatureCelsius == nil || *disk.TemperatureCelsius != 37 {
		t.Fatalf("disk = %+v", disk)
	}
}

func TestParseSmartctlDiskJSONReturnsTemperatureWithSmartctlExitStatus(t *testing.T) {
	t.Parallel()

	disk, err := parseSmartctlDiskJSON([]byte(`{
		"smartctl":{"version":[7,5],"exit_status":4},
		"device":{"name":"/dev/sda","protocol":"ATA"},
		"model_name":"Data HDD",
		"serial_number":"SN-HDD",
		"temperature":{"current":35}
	}`))
	if err != nil {
		t.Fatalf("parseSmartctlDiskJSON() error = %v", err)
	}
	if disk.TemperatureCelsius == nil || *disk.TemperatureCelsius != 35 {
		t.Fatalf("disk = %+v", disk)
	}
}

func TestParseSmartctlDiskJSONReturnsTemperatureFromAttributeString(t *testing.T) {
	t.Parallel()

	disk, err := parseSmartctlDiskJSON([]byte(`{
		"smartctl":{"version":[7,5],"exit_status":4},
		"device":{"name":"/dev/sdb","protocol":"ATA"},
		"model_name":"Data HDD",
		"serial_number":"SN-HDD",
		"ata_smart_attributes":{"table":[
			{"id":194,"name":"Temperature_Celsius","raw":{"value":257699086381,"string":"45 (Min/Max 16/60)"}}
		]}
	}`))
	if err != nil {
		t.Fatalf("parseSmartctlDiskJSON() error = %v", err)
	}
	if disk.TemperatureCelsius == nil || *disk.TemperatureCelsius != 45 {
		t.Fatalf("disk = %+v", disk)
	}
}

func TestMergeSmartctlTemperaturesMatchesBySerial(t *testing.T) {
	t.Parallel()

	temp := 36
	disks := []PhysicalDiskStats{{
		DeviceID:     "0",
		FriendlyName: "Windows HDD",
		SerialNumber: "SN HDD",
		MediaType:    "HDD",
	}}
	smartDisks := []PhysicalDiskStats{{
		DeviceID:           "/dev/sda",
		FriendlyName:       "Data HDD",
		SerialNumber:       "SN-HDD",
		MediaType:          "HDD",
		TemperatureCelsius: &temp,
	}}

	mergeSmartctlTemperatures(disks, smartDisks)
	if disks[0].TemperatureCelsius == nil || *disks[0].TemperatureCelsius != 36 {
		t.Fatalf("merged disk = %+v", disks[0])
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
			MediaType:          "HDD",
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

func TestDiskServiceRefreshDiskTemperaturesReturnsSnapshotAndUpdatesCache(t *testing.T) {
	t.Parallel()

	temp := 42
	store := &memoryTemperatureHistoryStore{}
	service := NewDiskService(time.Minute)
	service.SetTemperatureHistoryStore(store)
	service.physical = func(context.Context) ([]PhysicalDiskStats, map[string]string) {
		return []PhysicalDiskStats{{
			DeviceID:           "0",
			FriendlyName:       "Instant HDD",
			SerialNumber:       "SN",
			MediaType:          "HDD",
			TemperatureCelsius: &temp,
		}}, nil
	}

	snapshot, err := service.RefreshDiskTemperatures(context.Background())
	if err != nil {
		t.Fatalf("RefreshDiskTemperatures() error = %v", err)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].TemperatureCelsius == nil || *snapshot.Items[0].TemperatureCelsius != 42 {
		t.Fatalf("snapshot items = %+v", snapshot.Items)
	}
	cached, err := service.DiskTemperatures(context.Background())
	if err != nil {
		t.Fatalf("DiskTemperatures() error = %v", err)
	}
	if len(cached.Items) != 1 || cached.Items[0].TemperatureCelsius == nil || *cached.Items[0].TemperatureCelsius != 42 {
		t.Fatalf("cached items = %+v", cached.Items)
	}
	if len(store.samples) != 1 || store.samples[0].TemperatureCelsius == nil || *store.samples[0].TemperatureCelsius != 42 {
		t.Fatalf("history samples = %+v", store.samples)
	}
}

func TestDiskServiceRefreshDiskTemperaturesReportsMissingSmartctl(t *testing.T) {
	t.Parallel()

	service := NewDiskService(time.Minute)
	service.SetSmartctlPath("Z:\\missing\\smartctl.exe")
	service.physical = func(context.Context) ([]PhysicalDiskStats, map[string]string) {
		return []PhysicalDiskStats{{
			DeviceID:     "0",
			FriendlyName: "Instant HDD",
			SerialNumber: "SN",
			MediaType:    "HDD",
		}}, nil
	}

	snapshot, err := service.RefreshDiskTemperatures(context.Background())
	if err != nil {
		t.Fatalf("RefreshDiskTemperatures() error = %v", err)
	}
	if snapshot.Errors["smartctl"] == "" {
		t.Fatalf("errors = %+v", snapshot.Errors)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0].TemperatureCelsius != nil {
		t.Fatalf("items = %+v", snapshot.Items)
	}
}

func TestDiskServiceRefreshTemperaturesStoresHistory(t *testing.T) {
	t.Parallel()

	temp := 38
	store := &memoryTemperatureHistoryStore{}
	service := NewDiskService(time.Minute)
	service.SetTemperatureHistoryStore(store)
	service.physical = func(context.Context) ([]PhysicalDiskStats, map[string]string) {
		return []PhysicalDiskStats{
			{
				DeviceID:           "0",
				FriendlyName:       "Test Disk",
				SerialNumber:       "SN",
				MediaType:          "HDD",
				TemperatureCelsius: &temp,
			},
			{
				DeviceID:     "1",
				FriendlyName: "No Sensor",
				SerialNumber: "SN2",
				MediaType:    "HDD",
			},
			{
				DeviceID:           "2",
				FriendlyName:       "SSD",
				SerialNumber:       "SN3",
				MediaType:          "SSD",
				TemperatureCelsius: &temp,
			},
		}, nil
	}

	service.RefreshTemperatures(context.Background())
	if len(store.samples) != 2 {
		t.Fatalf("samples = %+v", store.samples)
	}
	if store.samples[0].TemperatureCelsius == nil || *store.samples[0].TemperatureCelsius != 38 {
		t.Fatalf("first sample = %+v", store.samples[0])
	}
	if store.samples[1].TemperatureCelsius != nil || store.samples[1].TemperatureError != "temperature unavailable from smartctl and Windows Get-PhysicalDisk" {
		t.Fatalf("second sample = %+v", store.samples[1])
	}
	if store.deletedBefore == nil {
		t.Fatal("expected old history pruning")
	}
}

func TestDiskServiceDiskTemperatureHistoryGroupsSamples(t *testing.T) {
	t.Parallel()

	sampledAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	temp := 41
	service := NewDiskService(time.Minute)
	service.SetTemperatureHistoryStore(&memoryTemperatureHistoryStore{
		samples: []DiskTemperatureSample{
			{DeviceID: "0", FriendlyName: "Disk A", SerialNumber: "SN0", MediaType: "HDD", TemperatureCelsius: &temp, SampledAt: sampledAt},
			{DeviceID: "0", FriendlyName: "Disk A", SerialNumber: "SN0", MediaType: "HDD", TemperatureError: "temperature unavailable from Windows Get-PhysicalDisk", SampledAt: sampledAt.Add(30 * time.Minute)},
		},
	})

	history, err := service.DiskTemperatureHistory(context.Background(), sampledAt.Add(-time.Hour), sampledAt.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("DiskTemperatureHistory() error = %v", err)
	}
	if len(history.Items) != 1 || len(history.Items[0].Points) != 2 {
		t.Fatalf("history = %+v", history)
	}
	if history.Items[0].Points[0].TemperatureCelsius == nil || *history.Items[0].Points[0].TemperatureCelsius != 41 {
		t.Fatalf("first point = %+v", history.Items[0].Points[0])
	}
	if history.Items[0].MediaType != "HDD" {
		t.Fatalf("history disk = %+v", history.Items[0])
	}
	if history.Items[0].Points[1].TemperatureError != "temperature unavailable from Windows Get-PhysicalDisk" {
		t.Fatalf("second point = %+v", history.Items[0].Points[1])
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
			MediaType:          "HDD",
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

type memoryTemperatureHistoryStore struct {
	samples       []DiskTemperatureSample
	deletedBefore *time.Time
}

func (s *memoryTemperatureHistoryStore) InsertDiskTemperatureSamples(ctx context.Context, samples []DiskTemperatureSample) error {
	s.samples = append(s.samples, samples...)
	return nil
}

func (s *memoryTemperatureHistoryStore) ListDiskTemperatureSamples(ctx context.Context, from time.Time, to time.Time, limit int) ([]DiskTemperatureSample, error) {
	if limit > 0 && len(s.samples) > limit {
		return s.samples[:limit], nil
	}
	return append([]DiskTemperatureSample(nil), s.samples...), nil
}

func (s *memoryTemperatureHistoryStore) DeleteDiskTemperatureSamplesBefore(ctx context.Context, cutoff time.Time) error {
	s.deletedBefore = &cutoff
	return nil
}

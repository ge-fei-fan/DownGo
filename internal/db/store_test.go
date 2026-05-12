package db

import (
	"context"
	"testing"
	"time"

	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/monitor"
)

func TestListDownloadsReturnsEmptySliceWhenNoRecords(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	result, err := store.ListDownloads("active", 1, 20)
	if err != nil {
		t.Fatalf("list downloads: %v", err)
	}
	if result.Items == nil {
		t.Fatal("expected empty items slice, got nil")
	}
	if len(result.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(result.Items))
	}
}

func TestListDownloadsActiveExcludesCanceledAndCompleted(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	items := []domain.DownloadItem{
		{SourceURL: "https://example.com/resolving", NormalizedURL: "https://example.com/resolving", Platform: domain.PlatformYouTube, Title: "Resolving", Status: domain.StatusResolving},
		{SourceURL: "https://example.com/failed", NormalizedURL: "https://example.com/failed", Platform: domain.PlatformYouTube, Title: "Failed", Status: domain.StatusFailed},
		{SourceURL: "https://example.com/canceled", NormalizedURL: "https://example.com/canceled", Platform: domain.PlatformYouTube, Title: "Canceled", Status: domain.StatusCanceled},
		{SourceURL: "https://example.com/completed", NormalizedURL: "https://example.com/completed", Platform: domain.PlatformYouTube, Title: "Completed", Status: domain.StatusCompleted},
	}
	for _, item := range items {
		if _, err := store.CreateDownload(item); err != nil {
			t.Fatalf("create download: %v", err)
		}
	}

	result, err := store.ListDownloads("active", 1, 20)
	if err != nil {
		t.Fatalf("list active downloads: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 active items, got %d", len(result.Items))
	}

	statuses := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		statuses[item.Status] = true
	}
	if !statuses[domain.StatusResolving] {
		t.Fatal("expected resolving item in active list")
	}
	if !statuses[domain.StatusFailed] {
		t.Fatal("expected failed item in active list")
	}
	if statuses[domain.StatusCanceled] {
		t.Fatal("did not expect canceled item in active list")
	}
	if statuses[domain.StatusCompleted] {
		t.Fatal("did not expect completed item in active list")
	}
}

func TestDiskTemperatureSamplesCanBeStoredQueriedAndPruned(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	oldTime := time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	temp := 39
	if err := store.InsertDiskTemperatureSamples(ctx, []monitor.DiskTemperatureSample{
		{DeviceID: "0", FriendlyName: "Old", SerialNumber: "SN0", MediaType: "HDD", TemperatureCelsius: &temp, SampledAt: oldTime},
		{DeviceID: "1", FriendlyName: "New", SerialNumber: "SN1", MediaType: "HDD", TemperatureError: "temperature unavailable from Windows Get-PhysicalDisk", SampledAt: newTime},
		{DeviceID: "2", FriendlyName: "SSD", SerialNumber: "SN2", MediaType: "SSD", TemperatureCelsius: &temp, SampledAt: newTime},
	}); err != nil {
		t.Fatalf("InsertDiskTemperatureSamples() error = %v", err)
	}

	samples, err := store.ListDiskTemperatureSamples(ctx, oldTime.Add(-time.Hour), newTime.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ListDiskTemperatureSamples() error = %v", err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %+v", samples)
	}
	if samples[0].TemperatureCelsius == nil || *samples[0].TemperatureCelsius != 39 {
		t.Fatalf("first sample = %+v", samples[0])
	}
	if samples[1].TemperatureCelsius != nil || samples[1].TemperatureError != "temperature unavailable from Windows Get-PhysicalDisk" {
		t.Fatalf("second sample = %+v", samples[1])
	}

	if err := store.DeleteDiskTemperatureSamplesBefore(ctx, newTime); err != nil {
		t.Fatalf("DeleteDiskTemperatureSamplesBefore() error = %v", err)
	}
	samples, err = store.ListDiskTemperatureSamples(ctx, oldTime.Add(-time.Hour), newTime.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("ListDiskTemperatureSamples(after delete) error = %v", err)
	}
	if len(samples) != 1 || samples[0].DeviceID != "1" {
		t.Fatalf("samples after delete = %+v", samples)
	}
}

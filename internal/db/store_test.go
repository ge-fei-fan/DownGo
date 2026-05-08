package db

import (
	"testing"

	"example.com/downgo/internal/domain"
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

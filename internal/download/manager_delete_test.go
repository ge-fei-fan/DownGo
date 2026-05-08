package download

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/downgo/internal/domain"
)

func TestDeleteQueuedTaskRemovesQueueEntryAndMarksDeleted(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{})
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "queued.mp4")
	if err := os.WriteFile(outputPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	item, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=queued",
		NormalizedURL:  "https://www.youtube.com/watch?v=queued",
		Platform:       domain.PlatformYouTube,
		VideoID:        "queued",
		Title:          "Queued Task",
		OutputFilename: "queued.mp4",
		OutputPath:     outputPath,
		Status:         domain.StatusQueued,
	})
	if err != nil {
		t.Fatalf("CreateDownload() error = %v", err)
	}

	manager.queue = []int64{item.ID}

	if err := manager.Delete(item.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	stored, err := manager.store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.DeletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}
	if len(manager.queue) != 0 {
		t.Fatalf("expected queue to be empty, got %v", manager.queue)
	}
}

func TestDeleteDownloadingTaskWaitsForJobExit(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{})
	defer cleanup()

	item, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=downloading",
		NormalizedURL:  "https://www.youtube.com/watch?v=downloading",
		Platform:       domain.PlatformYouTube,
		VideoID:        "downloading",
		Title:          "Downloading Task",
		OutputFilename: "downloading.mp4",
		OutputPath:     filepath.Join(t.TempDir(), "downloading.mp4"),
		Status:         domain.StatusDownloading,
	})
	if err != nil {
		t.Fatalf("CreateDownload() error = %v", err)
	}

	jobDone := make(chan struct{})
	manager.active[item.ID] = &activeJob{
		cancel: func() {
			go func() {
				time.Sleep(120 * time.Millisecond)
				close(jobDone)
			}()
		},
		done: jobDone,
	}

	start := time.Now()
	if err := manager.Delete(item.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("Delete() returned too early: %v", elapsed)
	}

	stored, err := manager.store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.DeletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}
}

func TestCancelQueuedTaskRemovesQueueEntry(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{})
	defer cleanup()

	item, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.youtube.com/watch?v=cancel",
		NormalizedURL: "https://www.youtube.com/watch?v=cancel",
		Platform:      domain.PlatformYouTube,
		VideoID:       "cancel",
		Title:         "Cancel Task",
		Status:        domain.StatusQueued,
	})
	if err != nil {
		t.Fatalf("CreateDownload() error = %v", err)
	}

	manager.queue = []int64{item.ID}

	updated, err := manager.Cancel(item.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if updated.Status != domain.StatusCanceled {
		t.Fatalf("status = %q, want %q", updated.Status, domain.StatusCanceled)
	}
	if len(manager.queue) != 0 {
		t.Fatalf("expected queue to be empty, got %v", manager.queue)
	}
}

func TestRetryCanceledTaskReturnsError(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{})
	defer cleanup()

	item, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.youtube.com/watch?v=canceled",
		NormalizedURL: "https://www.youtube.com/watch?v=canceled",
		Platform:      domain.PlatformYouTube,
		VideoID:       "canceled",
		Title:         "Canceled Task",
		Status:        domain.StatusCanceled,
	})
	if err != nil {
		t.Fatalf("CreateDownload() error = %v", err)
	}

	if _, err := manager.Retry(item.ID); err == nil {
		t.Fatal("expected Retry() to reject canceled task")
	}
}

func TestDeleteDownloadingTaskIgnoresLateFileCleanupFailure(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{})
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "locked.mp4")
	file, err := os.Create(outputPath)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer file.Close()

	item, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=locked",
		NormalizedURL:  "https://www.youtube.com/watch?v=locked",
		Platform:       domain.PlatformYouTube,
		VideoID:        "locked",
		Title:          "Locked Task",
		OutputFilename: "locked.mp4",
		OutputPath:     outputPath,
		Status:         domain.StatusDownloading,
	})
	if err != nil {
		t.Fatalf("CreateDownload() error = %v", err)
	}

	done := make(chan struct{})
	close(done)
	manager.active[item.ID] = &activeJob{
		cancel: func() {},
		done:   done,
	}

	if err := manager.Delete(item.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	stored, err := manager.store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.DeletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}
}

func TestDeleteDownloadManagerCleanupDoesNotBlockShutdown(t *testing.T) {
	t.Parallel()

	manager, cleanup := newTestManager(t, &testRunner{})
	defer cleanup()

	item, err := manager.store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=cleanup",
		NormalizedURL:  "https://www.youtube.com/watch?v=cleanup",
		Platform:       domain.PlatformYouTube,
		VideoID:        "cleanup",
		Title:          "Cleanup Task",
		OutputFilename: "cleanup.mp4",
		OutputPath:     filepath.Join(t.TempDir(), "cleanup.mp4"),
		Status:         domain.StatusResolving,
	})
	if err != nil {
		t.Fatalf("CreateDownload() error = %v", err)
	}

	done := make(chan struct{})
	manager.resolving[item.ID] = &resolvingJob{
		cancel: func() { close(done) },
		done:   done,
	}

	if err := manager.Delete(item.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

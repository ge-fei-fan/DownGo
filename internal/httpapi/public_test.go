package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"example.com/downgo/internal/auth"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/deps"
	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/download"
	"example.com/downgo/internal/monitor"
	"example.com/downgo/webui"
)

func TestPublicDownloadEndpointsDoNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, store, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	if _, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:       "https://www.youtube.com/watch?v=active",
		NormalizedURL:   "https://www.youtube.com/watch?v=active",
		Platform:        domain.PlatformYouTube,
		VideoID:         "active",
		Title:           "Active",
		Status:          domain.StatusDownloading,
		ProgressPercent: 42,
	}); err != nil {
		t.Fatalf("CreateDownload(active) error = %v", err)
	}
	if _, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.youtube.com/watch?v=done",
		NormalizedURL: "https://www.youtube.com/watch?v=done",
		Platform:      domain.PlatformYouTube,
		VideoID:       "done",
		Title:         "Done",
		Status:        domain.StatusCompleted,
	}); err != nil {
		t.Fatalf("CreateDownload(completed) error = %v", err)
	}

	progress := getPublicDownloads(t, server.URL+"/api/public/downloads/progress?page=1&pageSize=10")
	if progress.Total != 1 || len(progress.Items) != 1 || progress.Items[0].VideoID != "active" || progress.Items[0].ProgressPercent != 42 {
		t.Fatalf("progress response = %+v", progress)
	}

	completed := getPublicDownloads(t, server.URL+"/api/public/downloads/completed?page=1&pageSize=10")
	if completed.Total != 1 || len(completed.Items) != 1 || completed.Items[0].VideoID != "done" {
		t.Fatalf("completed response = %+v", completed)
	}
}

func TestPublicCompletedListReturnsPublicThumbnailURL(t *testing.T) {
	t.Parallel()

	server, store, manager, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	item, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1public",
		NormalizedURL: "https://www.bilibili.com/video/BV1public",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1public",
		Title:         "Public Cover",
		ThumbnailURL:  protectedThumbnailURL(1),
		Status:        domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("CreateDownload(completed) error = %v", err)
	}
	item.ThumbnailURL = protectedThumbnailURL(item.ID)
	if _, err := store.UpdateMetadata(item.ID, item, domain.StatusCompleted, ""); err != nil {
		t.Fatalf("UpdateMetadata() error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(manager.ThumbnailPath(item.ID)), 0o755); err != nil {
		t.Fatalf("MkdirAll(thumbnail) error = %v", err)
	}
	pngCover := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if err := os.WriteFile(manager.ThumbnailPath(item.ID), pngCover, 0o644); err != nil {
		t.Fatalf("WriteFile(thumbnail) error = %v", err)
	}

	completed := getPublicDownloads(t, server.URL+"/api/public/downloads/completed?page=1&pageSize=10")
	if completed.Total != 1 || len(completed.Items) != 1 {
		t.Fatalf("completed response = %+v", completed)
	}
	if completed.Items[0].ThumbnailURL != publicThumbnailURL(item.ID) {
		t.Fatalf("thumbnailUrl = %q, want %q", completed.Items[0].ThumbnailURL, publicThumbnailURL(item.ID))
	}

	res, err := http.Get(server.URL + completed.Items[0].ThumbnailURL)
	if err != nil {
		t.Fatalf("GET public thumbnail error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	if contentType := res.Header.Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", contentType)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(thumbnail) error = %v", err)
	}
	if !bytes.Equal(body, pngCover) {
		t.Fatalf("thumbnail body = %q", body)
	}
}

func TestPublicThumbnailOnlyServesCompletedCachedCover(t *testing.T) {
	t.Parallel()

	server, store, manager, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	active, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1active",
		NormalizedURL: "https://www.bilibili.com/video/BV1active",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1active",
		Title:         "Active Cover",
		ThumbnailURL:  protectedThumbnailURL(1),
		Status:        domain.StatusDownloading,
	})
	if err != nil {
		t.Fatalf("CreateDownload(active) error = %v", err)
	}
	active.ThumbnailURL = protectedThumbnailURL(active.ID)
	if _, err := store.UpdateMetadata(active.ID, active, domain.StatusDownloading, ""); err != nil {
		t.Fatalf("UpdateMetadata(active) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(manager.ThumbnailPath(active.ID)), 0o755); err != nil {
		t.Fatalf("MkdirAll(active thumbnail) error = %v", err)
	}
	if err := os.WriteFile(manager.ThumbnailPath(active.ID), []byte("active-cover"), 0o644); err != nil {
		t.Fatalf("WriteFile(active thumbnail) error = %v", err)
	}

	missing, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:     "https://www.bilibili.com/video/BV1missing",
		NormalizedURL: "https://www.bilibili.com/video/BV1missing",
		Platform:      domain.PlatformBilibili,
		VideoID:       "BV1missing",
		Title:         "Missing Cover",
		ThumbnailURL:  protectedThumbnailURL(2),
		Status:        domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("CreateDownload(missing) error = %v", err)
	}
	missing.ThumbnailURL = protectedThumbnailURL(missing.ID)
	if _, err := store.UpdateMetadata(missing.ID, missing, domain.StatusCompleted, ""); err != nil {
		t.Fatalf("UpdateMetadata(missing) error = %v", err)
	}

	for _, url := range []string{
		server.URL + publicThumbnailURL(active.ID),
		server.URL + publicThumbnailURL(missing.ID),
		server.URL + "/api/public/downloads/999999/thumbnail",
		server.URL + "/api/public/downloads/999999/thumbnail-bad",
	} {
		res, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET %s error = %v", url, err)
		}
		body, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			t.Fatalf("ReadAll(%s) error = %v", url, readErr)
		}
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want %d", url, res.StatusCode, http.StatusNotFound)
		}
		if contentType := res.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("GET %s Content-Type = %q, want application/json", url, contentType)
		}
		if bytes.Contains(bytes.ToLower(body), []byte("<html")) {
			t.Fatalf("GET %s returned HTML fallback: %q", url, body)
		}
	}
}

func TestUnknownAPIPathReturnsJSONNotFrontendFallback(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/not-a-real-route")
	if err != nil {
		t.Fatalf("GET unknown API error = %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(unknown API) error = %v", err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNotFound)
	}
	if contentType := res.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if bytes.Contains(bytes.ToLower(body), []byte("<html")) {
		t.Fatalf("unknown API returned HTML fallback: %q", body)
	}
}

func TestPublicSystemMetricsDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/metrics")
	if err != nil {
		t.Fatalf("GET public metrics error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.Metrics
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode metrics error = %v", err)
	}
	if result.CPU == nil || result.CPU.UsagePercent != 12.5 {
		t.Fatalf("CPU metrics = %+v", result.CPU)
	}
	if result.Memory == nil || result.Memory.TotalBytes != 1024 {
		t.Fatalf("memory metrics = %+v", result.Memory)
	}
	if len(result.Disks) != 1 || result.Disks[0].Path != "F:\\" {
		t.Fatalf("disks = %+v", result.Disks)
	}
	if result.Network == nil || len(result.Network.Interfaces) != 1 {
		t.Fatalf("network = %+v", result.Network)
	}
	if result.Network.Interfaces[0].HardwareAddr != "00-11-22-33-44-55" || !result.Network.Interfaces[0].IsUp {
		t.Fatalf("network interface metadata = %+v", result.Network.Interfaces[0])
	}
	if len(result.Network.Interfaces[0].IPAddresses) != 2 || result.Network.Interfaces[0].IPAddresses[0].Address != "192.168.1.10" {
		t.Fatalf("network interface addresses = %+v", result.Network.Interfaces[0].IPAddresses)
	}
	if result.Host == nil || result.Host.Hostname != "test-host" {
		t.Fatalf("host = %+v", result.Host)
	}
	if result.Process.PID != 1234 || result.Process.Goroutines != 5 {
		t.Fatalf("process = %+v", result.Process)
	}
	if result.Errors["disk:X:"] != "access denied" {
		t.Fatalf("errors = %+v", result.Errors)
	}
}

func TestPublicSystemMetricsReturnsCollectorError(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServerWithMonitor(
		t,
		&publicTestRunner{},
		staticMonitor{err: errors.New("metrics unavailable")},
	)
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/metrics")
	if err != nil {
		t.Fatalf("GET public metrics error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusInternalServerError)
	}
	var body map[string]string
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode error response = %v", err)
	}
	if body["error"] != "metrics unavailable" {
		t.Fatalf("error response = %+v", body)
	}
}

func TestPublicSystemDisksDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disks")
	if err != nil {
		t.Fatalf("GET public disks error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.DiskSnapshot
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode disks error = %v", err)
	}
	if len(result.PhysicalDisks) != 1 || result.PhysicalDisks[0].DeviceID != "0" {
		t.Fatalf("physical disks = %+v", result.PhysicalDisks)
	}
	if result.PhysicalDisks[0].TemperatureCelsius == nil || *result.PhysicalDisks[0].TemperatureCelsius != 35 {
		t.Fatalf("physical disk temperature = %+v", result.PhysicalDisks[0])
	}
	if result.Errors["physicalDisks"] != "partial warning" {
		t.Fatalf("errors = %+v", result.Errors)
	}
}

func TestPublicDiskSMARTDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disks/smart")
	if err != nil {
		t.Fatalf("GET public disk SMART error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.DiskSMARTSnapshot
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode disk SMART error = %v", err)
	}
	if result.UpdatedAt == nil || result.NextRefreshAt == nil {
		t.Fatalf("SMART timestamps = %+v", result)
	}
	if len(result.Items) != 1 || result.Items[0].DeviceID != "/dev/sda" {
		t.Fatalf("SMART items = %+v", result.Items)
	}
	if result.Items[0].HealthStatus != "PASSED" || len(result.Items[0].Attributes) != 1 {
		t.Fatalf("SMART item = %+v", result.Items[0])
	}
	if result.Items[0].Attributes[0].ID != 194 || result.Items[0].Attributes[0].RawString != "35" {
		t.Fatalf("SMART attributes = %+v", result.Items[0].Attributes)
	}
}

func TestPublicDiskSMARTBySerialDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disks/smart/sn123")
	if err != nil {
		t.Fatalf("GET public disk SMART by serial error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.DiskSMARTStats
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode disk SMART item error = %v", err)
	}
	if result.SerialNumber != "SN123" || result.DeviceID != "/dev/sda" {
		t.Fatalf("SMART item = %+v", result)
	}
	if result.HealthStatus != "PASSED" || len(result.Attributes) != 1 {
		t.Fatalf("SMART item details = %+v", result)
	}
}

func TestPublicDiskSMARTBySerialReturnsNotFound(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disks/smart/missing")
	if err != nil {
		t.Fatalf("GET missing public disk SMART by serial error = %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("ReadAll(missing SMART) error = %v", err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNotFound)
	}
	if contentType := res.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if bytes.Contains(bytes.ToLower(body), []byte("<html")) {
		t.Fatalf("missing SMART returned HTML fallback: %q", body)
	}
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode missing SMART response = %v", err)
	}
	if payload["error"] != "disk SMART information not found" {
		t.Fatalf("error response = %+v", payload)
	}
}

func TestPublicDiskTemperaturesDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disk-temperatures")
	if err != nil {
		t.Fatalf("GET public disk temperatures error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.DiskTemperatureSnapshot
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode disk temperatures error = %v", err)
	}
	if result.UpdatedAt == nil || result.NextRefreshAt == nil {
		t.Fatalf("temperature timestamps = %+v", result)
	}
	if len(result.Items) != 1 || result.Items[0].DeviceID != "0" {
		t.Fatalf("temperature items = %+v", result.Items)
	}
	if result.Items[0].TemperatureCelsius == nil || *result.Items[0].TemperatureCelsius != 35 {
		t.Fatalf("temperature = %+v", result.Items[0])
	}
}

func TestPublicCurrentDiskTemperaturesDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disk-temperatures/current")
	if err != nil {
		t.Fatalf("GET public current disk temperatures error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.DiskTemperatureSnapshot
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode current disk temperatures error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].DeviceID != "0" {
		t.Fatalf("temperature items = %+v", result.Items)
	}
	if result.Items[0].TemperatureCelsius == nil || *result.Items[0].TemperatureCelsius != 35 {
		t.Fatalf("temperature = %+v", result.Items[0])
	}
}

func TestPublicDiskTemperatureHistoryDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disk-temperatures/history?from=2026-05-12T09:00:00Z&to=2026-05-12T11:00:00Z&limit=100")
	if err != nil {
		t.Fatalf("GET public disk temperature history error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.DiskTemperatureHistorySnapshot
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode disk temperature history error = %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].DeviceID != "0" {
		t.Fatalf("history items = %+v", result.Items)
	}
	if len(result.Items[0].Points) != 1 || result.Items[0].Points[0].TemperatureCelsius == nil || *result.Items[0].Points[0].TemperatureCelsius != 35 {
		t.Fatalf("history points = %+v", result.Items[0].Points)
	}
}

func TestPublicDiskTemperatureHistoryRejectsInvalidQuery(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/disk-temperatures/history?from=bad-time")
	if err != nil {
		t.Fatalf("GET public disk temperature history error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}
}

func TestPublicSystemPartitionsDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/public/system/partitions")
	if err != nil {
		t.Fatalf("GET public partitions error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result monitor.PartitionSnapshot
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode partitions error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("partition items = %+v", result.Items)
	}
	item := result.Items[0]
	if item.Path != "F:\\" || item.TotalBytes != 2048 || item.UsedBytes != 1024 || item.FreeBytes != 1024 || item.UsedPercent != 50 {
		t.Fatalf("partition item = %+v", item)
	}
	if result.Errors["partition:X:\\"] != "access denied" {
		t.Fatalf("errors = %+v", result.Errors)
	}
}

func TestPublicCreateDownloadDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	body := bytes.NewBufferString(`{"url":"https://www.youtube.com/watch?v=public123"}`)
	res, err := http.Post(server.URL+"/api/public/downloads", "application/json", body)
	if err != nil {
		t.Fatalf("POST public downloads error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusCreated)
	}
	var item domain.DownloadItem
	if err := json.NewDecoder(res.Body).Decode(&item); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	if item.ID == 0 || item.SourceURL != "https://www.youtube.com/watch?v=public123" || item.Status != domain.StatusResolving {
		t.Fatalf("created item = %+v", item)
	}
}

func TestPublicDeleteCompletedDownloadDoesNotRequireAuth(t *testing.T) {
	t.Parallel()

	server, store, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "done.mp4")
	sidecarPath := filepath.Join(filepath.Dir(outputPath), "done.info.json")
	if err := os.WriteFile(outputPath, []byte("video"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}
	if err := os.WriteFile(sidecarPath, []byte("{}"), 0o644); err != nil {
		t.Fatalf("WriteFile(sidecar) error = %v", err)
	}

	item, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=done-delete",
		NormalizedURL:  "https://www.youtube.com/watch?v=done-delete",
		Platform:       domain.PlatformYouTube,
		VideoID:        "done-delete",
		Title:          "Done Delete",
		OutputFilename: "done.mp4",
		OutputPath:     outputPath,
		Status:         domain.StatusCompleted,
	})
	if err != nil {
		t.Fatalf("CreateDownload(completed) error = %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/public/downloads/"+strconv.FormatInt(item.ID, 10), nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE public download error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusNoContent)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("expected output file to be deleted, stat error = %v", err)
	}
	if _, err := os.Stat(sidecarPath); !os.IsNotExist(err) {
		t.Fatalf("expected sidecar file to be deleted, stat error = %v", err)
	}
	stored, err := store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.DeletedAt == nil {
		t.Fatal("expected deleted_at to be set")
	}
}

func TestPublicDeleteRejectsUnfinishedDownload(t *testing.T) {
	t.Parallel()

	server, store, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	outputPath := filepath.Join(t.TempDir(), "active.mp4")
	if err := os.WriteFile(outputPath, []byte("partial"), 0o644); err != nil {
		t.Fatalf("WriteFile(output) error = %v", err)
	}
	item, err := store.CreateDownload(domain.DownloadItem{
		SourceURL:      "https://www.youtube.com/watch?v=active-delete",
		NormalizedURL:  "https://www.youtube.com/watch?v=active-delete",
		Platform:       domain.PlatformYouTube,
		VideoID:        "active-delete",
		Title:          "Active Delete",
		OutputFilename: "active.mp4",
		OutputPath:     outputPath,
		Status:         domain.StatusDownloading,
	})
	if err != nil {
		t.Fatalf("CreateDownload(active) error = %v", err)
	}

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/public/downloads/"+strconv.FormatInt(item.ID, 10)+"/file", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE public download error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusBadRequest)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected output file to remain, stat error = %v", err)
	}
	stored, err := store.GetDownload(item.ID)
	if err != nil {
		t.Fatalf("GetDownload() error = %v", err)
	}
	if stored.DeletedAt != nil {
		t.Fatal("expected deleted_at to remain empty")
	}
}

func TestProtectedDownloadsStillRequireAuth(t *testing.T) {
	t.Parallel()

	server, _, _, cleanup := newPublicAPITestServer(t, &publicTestRunner{})
	defer cleanup()

	res, err := http.Get(server.URL + "/api/downloads?view=active")
	if err != nil {
		t.Fatalf("GET protected downloads error = %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusUnauthorized)
	}
}

func getPublicDownloads(t *testing.T, url string) domain.PagedDownloads {
	t.Helper()

	res, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s error = %v", url, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.StatusCode, http.StatusOK)
	}
	var result domain.PagedDownloads
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		t.Fatalf("decode response error = %v", err)
	}
	return result
}

func newPublicAPITestServer(t *testing.T, runner download.Runner) (*httptest.Server, *db.Store, *download.Manager, func()) {
	t.Helper()
	return newPublicAPITestServerWithMonitor(t, runner, staticMonitor{metrics: testMetrics()})
}

func newPublicAPITestServerWithMonitor(t *testing.T, runner download.Runner, metrics monitor.Collector) (*httptest.Server, *db.Store, *download.Manager, func()) {
	t.Helper()

	baseDir := t.TempDir()
	defaults := config.Defaults(baseDir)
	if err := os.MkdirAll(filepath.Dir(defaults.YtDlpPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(defaults.YtDlpPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(yt-dlp) error = %v", err)
	}
	if err := os.WriteFile(defaults.FfmpegPath, []byte("stub"), 0o644); err != nil {
		t.Fatalf("WriteFile(ffmpeg) error = %v", err)
	}

	store, err := db.Open(baseDir)
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	settings, err := config.NewService(store, defaults)
	if err != nil {
		t.Fatalf("config.NewService() error = %v", err)
	}
	manager, err := download.NewManagerWithBaseDir(baseDir, store, settings, runner)
	if err != nil {
		t.Fatalf("NewManagerWithBaseDir() error = %v", err)
	}
	api := NewAPI(baseDir, settings, manager, deps.NewService(baseDir, nil), nil, auth.NewTokenManager("test"))
	api.monitor = metrics
	api.SetDiskProvider(staticDisks())
	api.SetPartitionProvider(staticPartitions())
	server := httptest.NewServer(NewRouter(api, webui.Assets))

	return server, store, manager, func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = manager.Shutdown(ctx)
		_ = store.Close()
	}
}

type publicTestRunner struct{}

func (r *publicTestRunner) Inspect(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
	return []domain.InspectResult{{
		Platform:          domain.PlatformYouTube,
		NormalizedURL:     url,
		VideoID:           "public123",
		Title:             "Public",
		QualityLabel:      "1080p",
		Container:         "mp4",
		SuggestedFilename: "Public [public123].mp4",
	}}, nil
}

func (r *publicTestRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	<-ctx.Done()
	return ctx.Err()
}

type staticMonitor struct {
	metrics monitor.Metrics
	err     error
}

func (m staticMonitor) Snapshot(ctx context.Context) (monitor.Metrics, error) {
	return m.metrics, m.err
}

type staticDiskProvider struct {
	disks        monitor.DiskSnapshot
	smart        monitor.DiskSMARTSnapshot
	temperatures monitor.DiskTemperatureSnapshot
	history      monitor.DiskTemperatureHistorySnapshot
	err          error
}

func (p staticDiskProvider) Disks(ctx context.Context) (monitor.DiskSnapshot, error) {
	return p.disks, p.err
}

func (p staticDiskProvider) DiskSMART(ctx context.Context) (monitor.DiskSMARTSnapshot, error) {
	return p.smart, p.err
}

func (p staticDiskProvider) DiskSMARTBySerial(ctx context.Context, serialNumber string) (monitor.DiskSMARTStats, bool, error) {
	serial := strings.ToLower(strings.TrimSpace(serialNumber))
	for _, item := range p.smart.Items {
		if strings.ToLower(strings.TrimSpace(item.SerialNumber)) == serial {
			return item, true, p.err
		}
	}
	return monitor.DiskSMARTStats{}, false, p.err
}

func (p staticDiskProvider) DiskTemperatures(ctx context.Context) (monitor.DiskTemperatureSnapshot, error) {
	return p.temperatures, p.err
}

func (p staticDiskProvider) RefreshDiskTemperatures(ctx context.Context) (monitor.DiskTemperatureSnapshot, error) {
	return p.temperatures, p.err
}

func (p staticDiskProvider) DiskTemperatureHistory(ctx context.Context, from time.Time, to time.Time, limit int) (monitor.DiskTemperatureHistorySnapshot, error) {
	return p.history, p.err
}

type staticPartitionProvider struct {
	partitions monitor.PartitionSnapshot
	err        error
}

func (p staticPartitionProvider) Partitions(ctx context.Context) (monitor.PartitionSnapshot, error) {
	return p.partitions, p.err
}

func staticPartitions() staticPartitionProvider {
	return staticPartitionProvider{
		partitions: monitor.PartitionSnapshot{
			Timestamp: time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC),
			Items: []monitor.DiskStats{{
				Path:        "F:\\",
				FSType:      "NTFS",
				TotalBytes:  2048,
				UsedBytes:   1024,
				FreeBytes:   1024,
				UsedPercent: 50,
			}},
			Errors: map[string]string{"partition:X:\\": "access denied"},
		},
	}
}

func staticDisks() staticDiskProvider {
	updatedAt := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	nextRefreshAt := updatedAt.Add(30 * time.Minute)
	temp := 35
	temperatures := monitor.DiskTemperatureSnapshot{
		UpdatedAt:     &updatedAt,
		NextRefreshAt: &nextRefreshAt,
		Items: []monitor.DiskTemperatureStats{{
			DeviceID:           "0",
			FriendlyName:       "Test HDD",
			SerialNumber:       "SN123",
			MediaType:          "HDD",
			TemperatureCelsius: &temp,
			UpdatedAt:          &updatedAt,
		}},
	}
	history := monitor.DiskTemperatureHistorySnapshot{
		From: updatedAt.Add(-time.Hour),
		To:   updatedAt.Add(time.Hour),
		Items: []monitor.DiskTemperatureHistoryDiskStats{{
			DeviceID:     "0",
			FriendlyName: "Test HDD",
			SerialNumber: "SN123",
			MediaType:    "HDD",
			Points: []monitor.DiskTemperatureHistoryPoint{{
				SampledAt:          updatedAt,
				TemperatureCelsius: &temp,
			}},
		}},
	}
	return staticDiskProvider{
		disks: monitor.DiskSnapshot{
			Timestamp:            updatedAt,
			TemperatureUpdatedAt: &updatedAt,
			NextRefreshAt:        &nextRefreshAt,
			PhysicalDisks: []monitor.PhysicalDiskStats{{
				DeviceID:             "0",
				FriendlyName:         "Test HDD",
				SerialNumber:         "SN123",
				MediaType:            "HDD",
				BusType:              "SATA",
				HealthStatus:         "Healthy",
				OperationalStatus:    "OK",
				SizeBytes:            1024,
				TemperatureCelsius:   &temp,
				TemperatureUpdatedAt: &updatedAt,
			}},
			Errors: map[string]string{"physicalDisks": "partial warning"},
		},
		smart: monitor.DiskSMARTSnapshot{
			UpdatedAt:     &updatedAt,
			NextRefreshAt: &nextRefreshAt,
			Items: []monitor.DiskSMARTStats{{
				DeviceID:           "/dev/sda",
				FriendlyName:       "Test HDD",
				SerialNumber:       "SN123",
				FirmwareVersion:    "1.0",
				MediaType:          "HDD",
				BusType:            "ATA",
				HealthStatus:       "PASSED",
				SizeBytes:          1024,
				TemperatureCelsius: &temp,
				Attributes: []monitor.SMARTAttribute{{
					ID:        194,
					Name:      "Temperature_Celsius",
					RawValue:  "35",
					RawString: "35",
				}},
			}},
		},
		temperatures: temperatures,
		history:      history,
	}
}

func testMetrics() monitor.Metrics {
	return monitor.Metrics{
		Timestamp: time.Date(2026, 5, 12, 10, 30, 0, 0, time.UTC),
		CPU: &monitor.CPUStats{
			UsagePercent:  12.5,
			LogicalCores:  16,
			PhysicalCores: 8,
			ModelName:     "Test CPU",
		},
		Memory: &monitor.MemoryStats{
			TotalBytes:     1024,
			UsedBytes:      512,
			AvailableBytes: 512,
			UsedPercent:    50,
		},
		Disks: []monitor.DiskStats{{
			Path:        "F:\\",
			FSType:      "NTFS",
			TotalBytes:  2048,
			UsedBytes:   1024,
			FreeBytes:   1024,
			UsedPercent: 50,
		}},
		Network: &monitor.NetworkStats{
			Interfaces: []monitor.NetworkInterfaceStats{{
				Name:         "Ethernet",
				HardwareAddr: "00-11-22-33-44-55",
				MTU:          1500,
				Flags:        []string{"up", "broadcast", "multicast"},
				IsUp:         true,
				IPAddresses: []monitor.NetworkAddressStats{
					{Address: "192.168.1.10", Family: "ipv4", CIDR: "192.168.1.10/24"},
					{Address: "fe80::1234", Family: "ipv6", CIDR: "fe80::1234/64"},
				},
				BytesSent:   100,
				BytesRecv:   200,
				PacketsSent: 3,
				PacketsRecv: 4,
			}},
		},
		Host: &monitor.HostStats{
			Hostname:      "test-host",
			OS:            "windows",
			Platform:      "windows",
			UptimeSeconds: 60,
		},
		Process: monitor.ProcessStats{
			PID:           1234,
			UptimeSeconds: 10,
			Goroutines:    5,
			AllocBytes:    4096,
			SysBytes:      8192,
		},
		Errors: map[string]string{"disk:X:": "access denied"},
	}
}

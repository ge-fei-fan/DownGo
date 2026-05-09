package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"example.com/downgo/internal/bilibili"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/domain"
)

const bilibiliUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36 Edg/123.0.0.0"

type BilibiliRunner struct {
	client *bilibili.Client
	http   *http.Client
}

func (r *BilibiliRunner) Inspect(ctx context.Context, settings config.Settings, rawURL string) ([]domain.InspectResult, error) {
	resolvedURL, err := r.resolveURL(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	client := r.biliClient()
	info, _, err := client.GetVideoInfo(ctx, settings.BilibiliSessdata, resolvedURL)
	if err != nil {
		return nil, err
	}

	pages := info.Pages

	results := make([]domain.InspectResult, 0, len(pages))
	for _, page := range pages {
		play, err := client.GetPlayInfo(ctx, settings.BilibiliSessdata, info.Bvid, page.CID)
		if err != nil {
			return nil, fmt.Errorf("解析第 %d P 失败：%w", page.Page, err)
		}
		selected, err := bilibili.SelectStreams(play)
		if err != nil {
			return nil, err
		}
		title := bilibiliPageTitle(info.Title, page, len(info.Pages) > 1)
		videoID := bilibiliVideoID(info.Bvid, page.CID)
		results = append(results, domain.InspectResult{
			Platform:          domain.PlatformBilibili,
			NormalizedURL:     fmt.Sprintf("https://www.bilibili.com/video/%s?p=%d", info.Bvid, page.Page),
			VideoID:           videoID,
			Title:             title,
			ThumbnailURL:      info.Pic,
			QualityLabel:      selected.QualityLabel,
			Container:         "mp4",
			DurationSeconds:   page.Duration,
			SuggestedFilename: safeOutputFilename(title, videoID),
		})
	}
	return results, nil
}

func (r *BilibiliRunner) resolveURL(ctx context.Context, rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if strings.ToLower(parsed.Hostname()) != "b23.tv" {
		return rawURL, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", bilibiliUserAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	res, err := r.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.Request != nil && res.Request.URL != nil {
		return res.Request.URL.String(), nil
	}
	return rawURL, nil
}

func (r *BilibiliRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	onStart(0)
	bvid, cid, err := parseBilibiliVideoID(item.VideoID)
	if err != nil {
		return err
	}
	play, err := r.biliClient().GetPlayInfo(ctx, settings.BilibiliSessdata, bvid, cid)
	if err != nil {
		return err
	}
	selected, err := bilibili.SelectStreams(play)
	if err != nil {
		return err
	}
	audioURL := bilibili.StreamURL(selected.Audio.BaseURL, selected.Audio.BackupURL)
	videoURL := bilibili.StreamURL(selected.Video.BaseURL, selected.Video.BackupURL)
	if audioURL == "" || videoURL == "" {
		return errors.New("Bilibili 音视频流地址为空")
	}

	if err := os.MkdirAll(filepath.Dir(item.OutputPath), 0o755); err != nil {
		return err
	}
	audioPath := item.OutputPath + ".audio"
	videoPath := item.OutputPath + ".video"
	if err := r.downloadStream(ctx, audioURL, audioPath, 0, 20, onProgress, item.QualityLabel, item.Container); err != nil {
		return err
	}
	if err := r.downloadStream(ctx, videoURL, videoPath, 20, 95, onProgress, item.QualityLabel, item.Container); err != nil {
		return err
	}

	onProgress(domain.StatusPostprocessing, 100, 0, 0, item.QualityLabel, item.Container)
	if err := mergeBilibiliStreams(ctx, settings.FfmpegPath, videoPath, audioPath, item.OutputPath, onStart); err != nil {
		return err
	}
	_ = os.Remove(audioPath)
	_ = os.Remove(videoPath)
	return nil
}

func (r *BilibiliRunner) downloadStream(ctx context.Context, streamURL string, targetPath string, basePercent float64, spanPercent float64, onProgress func(string, float64, float64, int64, string, string), qualityLabel string, container string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", bilibiliUserAgent)
	req.Header.Set("Referer", "https://www.bilibili.com/")
	res, err := r.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("Bilibili 下载流返回 HTTP %d", res.StatusCode)
	}

	file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	total := res.ContentLength
	if total <= 0 {
		total = 1
	}
	buf := make([]byte, 128*1024)
	var written int64
	var lastWritten int64
	lastTick := time.Now()
	for {
		n, readErr := res.Body.Read(buf)
		if n > 0 {
			if _, err := file.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)
			now := time.Now()
			if now.Sub(lastTick) >= time.Second {
				speed := float64(written-lastWritten) / now.Sub(lastTick).Seconds()
				lastWritten = written
				lastTick = now
				percent := basePercent + (float64(written)/float64(total))*spanPercent
				onProgress(domain.StatusDownloading, percent, speed, 0, qualityLabel, container)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return readErr
		}
	}
	onProgress(domain.StatusDownloading, basePercent+spanPercent, 0, 0, qualityLabel, container)
	return nil
}

func mergeBilibiliStreams(ctx context.Context, ffmpegPath string, videoPath string, audioPath string, outputPath string, onStart func(int)) error {
	cmd := exec.CommandContext(ctx, ffmpegPath, "-y", "-i", videoPath, "-i", audioPath, "-c", "copy", outputPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	onStart(cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg 合并失败：%w", err)
	}
	return nil
}

func (r *BilibiliRunner) biliClient() *bilibili.Client {
	if r.client != nil {
		return r.client
	}
	return bilibili.NewClient(r.httpClient())
}

func (r *BilibiliRunner) httpClient() *http.Client {
	if r.http != nil {
		return r.http
	}
	return http.DefaultClient
}

func bilibiliPageTitle(title string, page bilibili.Page, multiPage bool) string {
	part := strings.TrimSpace(page.Part)
	if !multiPage || part == "" || part == strings.TrimSpace(title) {
		return title
	}
	return fmt.Sprintf("%s - P%02d %s", title, page.Page, part)
}

func bilibiliVideoID(bvid string, cid int64) string {
	return fmt.Sprintf("%s#%d", bvid, cid)
}

func parseBilibiliVideoID(videoID string) (string, int64, error) {
	parts := strings.SplitN(videoID, "#", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", 0, errors.New("Bilibili 视频 ID 格式无效")
	}
	cid, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || cid <= 0 {
		return "", 0, errors.New("Bilibili cid 格式无效")
	}
	return parts[0], cid, nil
}

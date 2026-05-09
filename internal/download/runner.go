package download

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/domain"
)

const (
	progressPrefix       = "__DOWNGO_PROGRESS__:"
	postprocessPrefix    = "__DOWNGO_POST__:"
	progressTemplate     = progressPrefix + `{"percent":"%(progress._percent_str)s","speed":"%(progress.speed)s","eta":"%(progress.eta)s"}`
	postprocessTemplate  = postprocessPrefix + `{"status":"%(progress.status)s","eta":"%(progress.eta)s"}`
	metaPrefix           = "__DOWNGO_META__:"
	finalMetaPrefix      = "__DOWNGO_FINAL__:"
	progressDeltaSeconds = "1"
)

type Runner interface {
	Inspect(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error)
	Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error
}

type YTDLPRunner struct{}

type PlatformRunner struct {
	YouTube  Runner
	Bilibili Runner
}

func NewPlatformRunner() *PlatformRunner {
	return &PlatformRunner{YouTube: &YTDLPRunner{}, Bilibili: &BilibiliRunner{}}
}

type inspectJSON struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Thumbnail      string `json:"thumbnail"`
	Resolution     string `json:"resolution"`
	Height         int64  `json:"height"`
	Ext            string `json:"ext"`
	Duration       int64  `json:"duration"`
	FilesizeApprox int64  `json:"filesize_approx"`
}

type streamLine struct {
	stream string
	text   string
}

func (r *PlatformRunner) Inspect(ctx context.Context, settings config.Settings, rawURL string) ([]domain.InspectResult, error) {
	source, err := normalizeSourceURL(rawURL)
	if err != nil {
		return nil, err
	}
	switch source.Platform {
	case domain.PlatformYouTube:
		return r.YouTube.Inspect(ctx, settings, source.URL)
	case domain.PlatformBilibili:
		return r.Bilibili.Inspect(ctx, settings, source.URL)
	default:
		return nil, errors.New(unsupportedURLMessage)
	}
}

func (r *PlatformRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	switch item.Platform {
	case domain.PlatformYouTube:
		return r.YouTube.Download(ctx, settings, item, onStart, onProgress)
	case domain.PlatformBilibili:
		return r.Bilibili.Download(ctx, settings, item, onStart, onProgress)
	default:
		return errors.New(unsupportedURLMessage)
	}
}

func (r *YTDLPRunner) Inspect(ctx context.Context, settings config.Settings, url string) ([]domain.InspectResult, error) {
	args := []string{
		"--dump-single-json",
		"--no-playlist",
		"-f", "bestvideo*+bestaudio/best",
		"--merge-output-format", "mp4",
		url,
	}
	cmd := exec.CommandContext(ctx, settings.YtDlpPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}

	var parsed inspectJSON
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, err
	}
	if parsed.ID == "" {
		return nil, errors.New("yt-dlp returned empty video id")
	}

	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = parsed.ID
	}
	qualityLabel := qualityLabelFromInspect(parsed.Height, parsed.Resolution)

	return []domain.InspectResult{{
		Platform:           domain.PlatformYouTube,
		NormalizedURL:      fmt.Sprintf("https://www.youtube.com/watch?v=%s", parsed.ID),
		VideoID:            parsed.ID,
		Title:              title,
		ThumbnailURL:       parsed.Thumbnail,
		QualityLabel:       qualityLabel,
		Container:          normalizeContainer(parsed.Ext),
		DurationSeconds:    parsed.Duration,
		EstimatedSizeBytes: parsed.FilesizeApprox,
		SuggestedFilename:  safeOutputFilename(title, parsed.ID),
	}}, nil
}

func (r *YTDLPRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64, string, string)) error {
	args := buildDownloadArgs(settings, item)

	cmd := exec.CommandContext(ctx, settings.YtDlpPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	onStart(cmd.Process.Pid)

	lineCh := make(chan streamLine, 64)
	scanErrCh := make(chan error, 2)
	var scanWG sync.WaitGroup
	scanWG.Add(2)

	go scanOutput("stdout", stdout, lineCh, scanErrCh, &scanWG)
	go scanOutput("stderr", stderr, lineCh, scanErrCh, &scanWG)
	go func() {
		scanWG.Wait()
		close(lineCh)
	}()

	var stderrBuf bytes.Buffer
	for line := range lineCh {
		handleDownloadOutputLine(line, &stderrBuf, onProgress)
	}

	waitErr := cmd.Wait()
	var scanErr error
	for i := 0; i < 2; i++ {
		if err := <-scanErrCh; err != nil && scanErr == nil {
			scanErr = err
		}
	}
	if scanErr != nil {
		return scanErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(stderrBuf.String())
		if msg == "" {
			if errors.Is(ctx.Err(), context.Canceled) {
				return ctx.Err()
			}
			msg = waitErr.Error()
		}
		return errors.New(msg)
	}

	return nil
}

func buildDownloadArgs(settings config.Settings, item domain.DownloadItem) []string {
	return []string{
		"--newline",
		"--progress",
		"--progress-delta", progressDeltaSeconds,
		"--no-colors",
		"--no-playlist",
		"--progress-template", "download:" + progressTemplate,
		"--progress-template", "postprocess:" + postprocessTemplate,
		"--print", "before_dl:" + metaPrefix + "%(resolution)s|%(ext)s",
		"--print", "after_move:" + finalMetaPrefix + "%(resolution)s|%(ext)s",
		"-f", "bestvideo*+bestaudio/best",
		"--merge-output-format", "mp4",
		"--ffmpeg-location", filepath.Dir(settings.FfmpegPath),
		"-o", item.OutputPath,
		item.NormalizedURL,
	}
}

func scanOutput(stream string, pipe io.ReadCloser, lineCh chan<- streamLine, errCh chan<- error, wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(pipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lineCh <- streamLine{stream: stream, text: scanner.Text()}
	}
	errCh <- scanner.Err()
}

func handleDownloadOutputLine(line streamLine, stderrBuf *bytes.Buffer, onProgress func(string, float64, float64, int64, string, string)) {
	text := strings.TrimSpace(line.text)
	if text == "" {
		return
	}
	if strings.HasPrefix(text, metaPrefix) || strings.HasPrefix(text, finalMetaPrefix) {
		qualityLabel, container := parseQualityMetadata(text)
		onProgress(domain.StatusDownloading, 0, 0, 0, qualityLabel, container)
		return
	}
	if strings.Contains(text, "[Merger]") || strings.Contains(text, "[ffmpeg]") {
		onProgress(domain.StatusPostprocessing, 100, 0, 0, "", "")
		return
	}

	if strings.HasPrefix(text, progressPrefix) {
		percent, speed, eta, ok := parseProgressPayload(strings.TrimPrefix(text, progressPrefix))
		if ok {
			onProgress(domain.StatusDownloading, percent, speed, eta, "", "")
			return
		}
	}

	if strings.HasPrefix(text, postprocessPrefix) {
		onProgress(domain.StatusPostprocessing, 100, 0, 0, "", "")
		return
	}

	if line.stream == "stderr" {
		stderrBuf.WriteString(text)
		stderrBuf.WriteByte('\n')
	}
}

func parseProgressPayload(value string) (float64, float64, int64, bool) {
	var payload struct {
		Percent string `json:"percent"`
		Speed   string `json:"speed"`
		ETA     string `json:"eta"`
	}
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return 0, 0, 0, false
	}
	return parsePercent(payload.Percent), parseFloat(payload.Speed), int64(parseFloat(payload.ETA)), true
}

func safeOutputFilename(title, videoID string) string {
	base := strings.TrimSpace(title)
	if base == "" {
		base = videoID
	}
	return fmt.Sprintf("%s [%s].mp4", sanitizeFilename(base), videoID)
}

func parseQualityMetadata(line string) (string, string) {
	value := strings.TrimPrefix(strings.TrimPrefix(line, metaPrefix), finalMetaPrefix)
	parts := strings.SplitN(value, "|", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return qualityLabelFromInspect(0, parts[0]), normalizeContainer(parts[1])
}

func qualityLabelFromInspect(height int64, resolution string) string {
	if height > 0 {
		return strconv.FormatInt(height, 10) + "p"
	}

	resolution = strings.TrimSpace(strings.ToLower(resolution))
	if resolution == "" || resolution == "audio only" || resolution == "unknown" || resolution == "none" {
		return ""
	}
	if strings.HasSuffix(resolution, "p") {
		return strings.ToUpper(resolution)
	}
	if strings.Contains(resolution, "x") {
		parts := strings.Split(resolution, "x")
		last := strings.TrimSpace(parts[len(parts)-1])
		if last != "" {
			return last + "p"
		}
	}
	return strings.ToUpper(resolution)
}

func normalizeContainer(ext string) string {
	ext = strings.TrimSpace(strings.ToLower(ext))
	if ext == "" || ext == "unknown" || ext == "none" {
		return ""
	}
	return ext
}

func sanitizeFilename(input string) string {
	replacer := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", "\"", "_",
		"/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
	)
	output := replacer.Replace(input)
	output = strings.Join(strings.Fields(output), " ")
	output = strings.Trim(output, ". ")
	if output == "" {
		return "video"
	}
	return output
}

func parsePercent(value string) float64 {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "%")
	return parseFloat(value)
}

func parseFloat(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "NA") {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err == nil {
		return parsed
	}

	if strings.HasSuffix(value, "/s") {
		return parseRate(value)
	}

	return 0
}

func parseRate(value string) float64 {
	value = strings.TrimSuffix(value, "/s")
	units := []struct {
		suffix string
		mul    float64
	}{
		{"KiB", 1024},
		{"MiB", 1024 * 1024},
		{"GiB", 1024 * 1024 * 1024},
		{"B", 1},
	}
	for _, unit := range units {
		if strings.HasSuffix(value, unit.suffix) {
			number := strings.TrimSuffix(value, unit.suffix)
			parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
			if err != nil {
				return 0
			}
			return parsed * unit.mul
		}
	}
	return 0
}

func KillProcessTree(pid int) error {
	if pid <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

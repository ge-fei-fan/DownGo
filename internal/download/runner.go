package download

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"example.com/downgo/internal/config"
	"example.com/downgo/internal/domain"
)

const progressTemplate = `{"percent":"%(progress._percent_str)s","speed":"%(progress.speed)s","eta":"%(progress.eta)s"}`

type Runner interface {
	Inspect(ctx context.Context, settings config.Settings, url string) (domain.InspectResult, error)
	Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64)) error
}

type YTDLPRunner struct{}

type inspectJSON struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Thumbnail      string `json:"thumbnail"`
	Duration       int64  `json:"duration"`
	FilesizeApprox int64  `json:"filesize_approx"`
}

func (r *YTDLPRunner) Inspect(ctx context.Context, settings config.Settings, url string) (domain.InspectResult, error) {
	args := []string{"--dump-single-json", "--no-playlist", url}
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
		return domain.InspectResult{}, errors.New(msg)
	}

	var parsed inspectJSON
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return domain.InspectResult{}, err
	}
	if parsed.ID == "" {
		return domain.InspectResult{}, errors.New("yt-dlp returned empty video id")
	}

	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = parsed.ID
	}

	return domain.InspectResult{
		NormalizedURL:      fmt.Sprintf("https://www.youtube.com/watch?v=%s", parsed.ID),
		VideoID:            parsed.ID,
		Title:              title,
		ThumbnailURL:       parsed.Thumbnail,
		DurationSeconds:    parsed.Duration,
		EstimatedSizeBytes: parsed.FilesizeApprox,
		SuggestedFilename:  safeOutputFilename(title, parsed.ID),
	}, nil
}

func (r *YTDLPRunner) Download(ctx context.Context, settings config.Settings, item domain.DownloadItem, onStart func(int), onProgress func(string, float64, float64, int64)) error {
	args := []string{
		"--newline",
		"--no-colors",
		"--no-playlist",
		"--progress-template", progressTemplate,
		"-f", "bestvideo*+bestaudio/best",
		"--merge-output-format", "mp4",
		"--ffmpeg-location", filepath.Dir(settings.FfmpegPath),
		"-o", item.OutputPath,
		item.NormalizedURL,
	}

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

	var stderrBuf bytes.Buffer
	if err := cmd.Start(); err != nil {
		return err
	}
	onStart(cmd.Process.Pid)

	doneErr := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrBuf.WriteString(line)
			stderrBuf.WriteByte('\n')
		}
		doneErr <- scanner.Err()
	}()

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.Contains(line, "[Merger]") || strings.Contains(line, "[ffmpeg]") {
			onProgress(domain.StatusPostprocessing, 100, 0, 0)
			continue
		}
		var payload map[string]string
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		percent := parsePercent(payload["percent"])
		speed := parseFloat(payload["speed"])
		eta := int64(parseFloat(payload["eta"]))
		onProgress(domain.StatusDownloading, percent, speed, eta)
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	waitErr := cmd.Wait()
	streamErr := <-doneErr
	if streamErr != nil {
		return streamErr
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

func safeOutputFilename(title, videoID string) string {
	base := strings.TrimSpace(title)
	if base == "" {
		base = videoID
	}
	return fmt.Sprintf("%s [%s].mp4", sanitizeFilename(base), videoID)
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

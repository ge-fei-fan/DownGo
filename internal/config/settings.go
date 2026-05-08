package config

import (
	"path/filepath"
	"strconv"
	"sync"

	"example.com/downgo/internal/db"
)

const (
	defaultHost       = "0.0.0.0"
	defaultPort       = 12225
	legacyDefaultPort = 38080
	defaultConcurrent = 2
)

type Settings struct {
	BindHost            string `json:"bindHost"`
	Port                int    `json:"port"`
	DownloadDir         string `json:"downloadDir"`
	ConcurrentDownloads int    `json:"concurrentDownloads"`
	YtDlpPath           string `json:"ytDlpPath"`
	FfmpegPath          string `json:"ffmpegPath"`
	AccessTokenHash     string `json:"-"`
}

type UpdateInput struct {
	BindHost            string `json:"bindHost"`
	Port                int    `json:"port"`
	DownloadDir         string `json:"downloadDir"`
	ConcurrentDownloads int    `json:"concurrentDownloads"`
	YtDlpPath           string `json:"ytDlpPath"`
	FfmpegPath          string `json:"ffmpegPath"`
	AccessPassword      string `json:"accessPassword"`
}

type Service struct {
	store *db.Store
	mu    sync.RWMutex
	value Settings
}

func NewService(store *db.Store, defaults Settings) (*Service, error) {
	s := &Service{store: store}
	if err := s.load(defaults); err != nil {
		return nil, err
	}
	return s, nil
}

func Defaults(baseDir string) Settings {
	return Settings{
		BindHost:            defaultHost,
		Port:                defaultPort,
		DownloadDir:         filepath.Join(baseDir, "data", "downloads"),
		ConcurrentDownloads: defaultConcurrent,
		YtDlpPath:           filepath.Join(baseDir, "data", "bin", "yt-dlp.exe"),
		FfmpegPath:          filepath.Join(baseDir, "data", "bin", "ffmpeg.exe"),
	}
}

func (s *Service) Current() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *Service) Update(input UpdateInput, passwordHash func(string) string) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.value
	if input.BindHost != "" {
		next.BindHost = input.BindHost
	}
	if input.Port > 0 {
		next.Port = input.Port
	}
	if input.DownloadDir != "" {
		next.DownloadDir = input.DownloadDir
	}
	if input.ConcurrentDownloads > 0 {
		next.ConcurrentDownloads = input.ConcurrentDownloads
	}
	if input.YtDlpPath != "" {
		next.YtDlpPath = input.YtDlpPath
	}
	if input.FfmpegPath != "" {
		next.FfmpegPath = input.FfmpegPath
	}
	if input.AccessPassword != "" {
		next.AccessTokenHash = passwordHash(input.AccessPassword)
	}

	if err := s.persist(next); err != nil {
		return Settings{}, err
	}
	s.value = next
	return next, nil
}

func (s *Service) SetPasswordHash(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.value
	next.AccessTokenHash = hash
	if err := s.persist(next); err != nil {
		return err
	}
	s.value = next
	return nil
}

func (s *Service) load(defaults Settings) error {
	settings := defaults

	rows, err := s.store.ListSettings()
	if err != nil {
		return err
	}

	if value, ok := rows["bind_host"]; ok && value != "" {
		settings.BindHost = value
	}
	if value, ok := rows["port"]; ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.Port = parsed
		}
	}
	if value, ok := rows["download_dir"]; ok && value != "" {
		settings.DownloadDir = value
	}
	if value, ok := rows["concurrent_downloads"]; ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.ConcurrentDownloads = parsed
		}
	}
	if value, ok := rows["yt_dlp_path"]; ok && value != "" {
		settings.YtDlpPath = value
	}
	if value, ok := rows["ffmpeg_path"]; ok && value != "" {
		settings.FfmpegPath = value
	}
	if value, ok := rows["access_token_hash"]; ok && value != "" {
		settings.AccessTokenHash = value
	}

	if settings.Port == legacyDefaultPort && settings.BindHost == defaultHost {
		settings.Port = defaultPort
	}

	if err := s.persist(settings); err != nil {
		return err
	}
	s.value = settings
	return nil
}

func (s *Service) persist(settings Settings) error {
	pairs := map[string]string{
		"bind_host":            settings.BindHost,
		"port":                 strconv.Itoa(settings.Port),
		"download_dir":         settings.DownloadDir,
		"concurrent_downloads": strconv.Itoa(settings.ConcurrentDownloads),
		"yt_dlp_path":          settings.YtDlpPath,
		"ffmpeg_path":          settings.FfmpegPath,
		"access_token_hash":    settings.AccessTokenHash,
	}
	return s.store.UpsertSettings(pairs)
}

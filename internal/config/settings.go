package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/downgo/internal/db"
)

const (
	defaultHost       = "0.0.0.0"
	defaultPort       = 12225
	legacyDefaultPort = 38080
	defaultConcurrent = 2
)

type Settings struct {
	BindHost             string `json:"bindHost"`
	Port                 int    `json:"port"`
	DownloadDir          string `json:"downloadDir"`
	ConcurrentDownloads  int    `json:"concurrentDownloads"`
	YtDlpPath            string `json:"ytDlpPath"`
	YtDlpCookiePath      string `json:"ytDlpCookiePath"`
	YtDlpCookieEnabled   bool   `json:"ytDlpCookieEnabled"`
	FfmpegPath           string `json:"ffmpegPath"`
	BilibiliMid          int    `json:"bilibiliMid"`
	BilibiliUname        string `json:"bilibiliUname"`
	BilibiliFace         string `json:"bilibiliFace"`
	BilibiliFaceLocal    string `json:"bilibiliFaceLocal"`
	BilibiliLoginAt      string `json:"bilibiliLoginAt"`
	BilibiliCheckStatus  string `json:"bilibiliCheckStatus"`
	BilibiliCheckedAt    string `json:"bilibiliCheckedAt"`
	BilibiliLevel        int    `json:"bilibiliLevel"`
	BilibiliSex          string `json:"bilibiliSex"`
	BilibiliSign         string `json:"bilibiliSign"`
	BilibiliVipStatus    int    `json:"bilibiliVipStatus"`
	BilibiliVipType      int    `json:"bilibiliVipType"`
	BilibiliVipLabel     string `json:"bilibiliVipLabel"`
	BilibiliVipDueDate   int64  `json:"bilibiliVipDueDate"`
	BilibiliSeniorMember bool   `json:"bilibiliSeniorMember"`
	AccessTokenHash      string `json:"-"`
	BilibiliSessdata     string `json:"-"`
	BilibiliJct          string `json:"-"`
}

type BilibiliSession struct {
	LoggedIn     bool   `json:"loggedIn"`
	Mid          int    `json:"mid"`
	Uname        string `json:"uname"`
	Face         string `json:"face"`
	FaceLocal    string `json:"faceLocal"`
	LoginAt      string `json:"loginAt"`
	Status       string `json:"status"`
	CheckedAt    string `json:"checkedAt"`
	Message      string `json:"message"`
	Level        int    `json:"level"`
	Sex          string `json:"sex"`
	Sign         string `json:"sign"`
	VipStatus    int    `json:"vipStatus"`
	VipType      int    `json:"vipType"`
	VipLabel     string `json:"vipLabel"`
	VipDueDate   int64  `json:"vipDueDate"`
	SeniorMember bool   `json:"seniorMember"`
}

type UpdateInput struct {
	BindHost            string `json:"bindHost"`
	Port                int    `json:"port"`
	DownloadDir         string `json:"downloadDir"`
	ConcurrentDownloads int    `json:"concurrentDownloads"`
	YtDlpPath           string `json:"ytDlpPath"`
	YtDlpCookiePath     string `json:"ytDlpCookiePath"`
	YtDlpCookieEnabled  bool   `json:"ytDlpCookieEnabled"`
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

func (s *Service) BilibiliSession() BilibiliSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return bilibiliSessionFromSettings(s.value)
}

func (s *Service) BilibiliCredentials() (string, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value.BilibiliSessdata, s.value.BilibiliJct, s.value.BilibiliSessdata != "" && s.value.BilibiliJct != ""
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
	next.YtDlpCookiePath = strings.TrimSpace(input.YtDlpCookiePath)
	next.YtDlpCookieEnabled = input.YtDlpCookieEnabled
	if input.AccessPassword != "" {
		next.AccessTokenHash = passwordHash(input.AccessPassword)
	}
	if err := validateYtDlpCookie(next); err != nil {
		return Settings{}, err
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

func (s *Service) SetBilibiliSession(sessdata string, biliJct string, mid int, uname string, face string, faceLocal string, loginAt time.Time) (BilibiliSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.value
	next.BilibiliSessdata = sessdata
	next.BilibiliJct = biliJct
	next.BilibiliMid = mid
	next.BilibiliUname = uname
	next.BilibiliFace = face
	next.BilibiliFaceLocal = faceLocal
	next.BilibiliLoginAt = loginAt.UTC().Format(time.RFC3339)
	next.BilibiliCheckStatus = "unchecked"
	next.BilibiliCheckedAt = ""
	if err := s.persist(next); err != nil {
		return BilibiliSession{}, err
	}
	s.value = next
	return bilibiliSessionFromSettings(next), nil
}

func (s *Service) UpdateBilibiliCheck(status string, message string, mid int, uname string, face string, faceLocal string, checkedAt time.Time) (BilibiliSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.value
	next.BilibiliCheckStatus = status
	next.BilibiliCheckedAt = checkedAt.UTC().Format(time.RFC3339)
	if mid > 0 {
		next.BilibiliMid = mid
	}
	if uname != "" {
		next.BilibiliUname = uname
	}
	if face != "" {
		next.BilibiliFace = face
	}
	if faceLocal != "" {
		next.BilibiliFaceLocal = faceLocal
	}
	if err := s.persist(next); err != nil {
		return BilibiliSession{}, err
	}
	s.value = next
	session := bilibiliSessionFromSettings(next)
	session.Message = message
	return session, nil
}

func (s *Service) UpdateBilibiliProfileCheck(status string, message string, mid int, uname string, face string, faceLocal string, level int, sex string, sign string, vipStatus int, vipType int, vipLabel string, vipDueDate int64, seniorMember bool, checkedAt time.Time) (BilibiliSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.value
	next.BilibiliCheckStatus = status
	next.BilibiliCheckedAt = checkedAt.UTC().Format(time.RFC3339)
	if mid > 0 {
		next.BilibiliMid = mid
	}
	if uname != "" {
		next.BilibiliUname = uname
	}
	if face != "" {
		next.BilibiliFace = face
	}
	if faceLocal != "" {
		next.BilibiliFaceLocal = faceLocal
	}
	next.BilibiliLevel = level
	next.BilibiliSex = sex
	next.BilibiliSign = sign
	next.BilibiliVipStatus = vipStatus
	next.BilibiliVipType = vipType
	next.BilibiliVipLabel = vipLabel
	next.BilibiliVipDueDate = vipDueDate
	next.BilibiliSeniorMember = seniorMember
	if err := s.persist(next); err != nil {
		return BilibiliSession{}, err
	}
	s.value = next
	session := bilibiliSessionFromSettings(next)
	session.Message = message
	return session, nil
}

func (s *Service) ClearBilibiliSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.value
	next.BilibiliSessdata = ""
	next.BilibiliJct = ""
	next.BilibiliMid = 0
	next.BilibiliUname = ""
	next.BilibiliFace = ""
	next.BilibiliFaceLocal = ""
	next.BilibiliLoginAt = ""
	next.BilibiliCheckStatus = ""
	next.BilibiliCheckedAt = ""
	next.BilibiliLevel = 0
	next.BilibiliSex = ""
	next.BilibiliSign = ""
	next.BilibiliVipStatus = 0
	next.BilibiliVipType = 0
	next.BilibiliVipLabel = ""
	next.BilibiliVipDueDate = 0
	next.BilibiliSeniorMember = false
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
	if value, ok := rows["yt_dlp_cookie_path"]; ok && value != "" {
		settings.YtDlpCookiePath = value
	}
	if value, ok := rows["yt_dlp_cookie_enabled"]; ok && value != "" {
		settings.YtDlpCookieEnabled = value == "true"
	}
	if value, ok := rows["access_token_hash"]; ok && value != "" {
		settings.AccessTokenHash = value
	}
	if value, ok := rows["bilibili_sessdata"]; ok && value != "" {
		settings.BilibiliSessdata = value
	}
	if value, ok := rows["bilibili_jct"]; ok && value != "" {
		settings.BilibiliJct = value
	}
	if value, ok := rows["bilibili_mid"]; ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.BilibiliMid = parsed
		}
	}
	if value, ok := rows["bilibili_uname"]; ok && value != "" {
		settings.BilibiliUname = value
	}
	if value, ok := rows["bilibili_face"]; ok && value != "" {
		settings.BilibiliFace = value
	}
	if value, ok := rows["bilibili_face_local"]; ok && value != "" {
		settings.BilibiliFaceLocal = value
	}
	if value, ok := rows["bilibili_login_at"]; ok && value != "" {
		settings.BilibiliLoginAt = value
	}
	if value, ok := rows["bilibili_check_status"]; ok && value != "" {
		settings.BilibiliCheckStatus = value
	}
	if value, ok := rows["bilibili_checked_at"]; ok && value != "" {
		settings.BilibiliCheckedAt = value
	}
	if value, ok := rows["bilibili_level"]; ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.BilibiliLevel = parsed
		}
	}
	if value, ok := rows["bilibili_sex"]; ok && value != "" {
		settings.BilibiliSex = value
	}
	if value, ok := rows["bilibili_sign"]; ok && value != "" {
		settings.BilibiliSign = value
	}
	if value, ok := rows["bilibili_vip_status"]; ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.BilibiliVipStatus = parsed
		}
	}
	if value, ok := rows["bilibili_vip_type"]; ok && value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			settings.BilibiliVipType = parsed
		}
	}
	if value, ok := rows["bilibili_vip_label"]; ok && value != "" {
		settings.BilibiliVipLabel = value
	}
	if value, ok := rows["bilibili_vip_due_date"]; ok && value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			settings.BilibiliVipDueDate = parsed
		}
	}
	if value, ok := rows["bilibili_senior_member"]; ok && value != "" {
		settings.BilibiliSeniorMember = value == "true"
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
		"bind_host":              settings.BindHost,
		"port":                   strconv.Itoa(settings.Port),
		"download_dir":           settings.DownloadDir,
		"concurrent_downloads":   strconv.Itoa(settings.ConcurrentDownloads),
		"yt_dlp_cookie_path":     settings.YtDlpCookiePath,
		"yt_dlp_cookie_enabled":  strconv.FormatBool(settings.YtDlpCookieEnabled),
		"access_token_hash":      settings.AccessTokenHash,
		"bilibili_sessdata":      settings.BilibiliSessdata,
		"bilibili_jct":           settings.BilibiliJct,
		"bilibili_mid":           strconv.Itoa(settings.BilibiliMid),
		"bilibili_uname":         settings.BilibiliUname,
		"bilibili_face":          settings.BilibiliFace,
		"bilibili_face_local":    settings.BilibiliFaceLocal,
		"bilibili_login_at":      settings.BilibiliLoginAt,
		"bilibili_check_status":  settings.BilibiliCheckStatus,
		"bilibili_checked_at":    settings.BilibiliCheckedAt,
		"bilibili_level":         strconv.Itoa(settings.BilibiliLevel),
		"bilibili_sex":           settings.BilibiliSex,
		"bilibili_sign":          settings.BilibiliSign,
		"bilibili_vip_status":    strconv.Itoa(settings.BilibiliVipStatus),
		"bilibili_vip_type":      strconv.Itoa(settings.BilibiliVipType),
		"bilibili_vip_label":     settings.BilibiliVipLabel,
		"bilibili_vip_due_date":  strconv.FormatInt(settings.BilibiliVipDueDate, 10),
		"bilibili_senior_member": strconv.FormatBool(settings.BilibiliSeniorMember),
	}
	return s.store.UpsertSettings(pairs)
}

func validateYtDlpCookie(settings Settings) error {
	if !settings.YtDlpCookieEnabled {
		return nil
	}
	if strings.TrimSpace(settings.YtDlpCookiePath) == "" {
		return errors.New("启用 yt-dlp Cookie 时请选择 cookie txt 文件")
	}
	if strings.ToLower(filepath.Ext(settings.YtDlpCookiePath)) != ".txt" {
		return errors.New("yt-dlp Cookie 文件必须是 .txt 文件")
	}
	info, err := os.Stat(settings.YtDlpCookiePath)
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("yt-dlp Cookie 文件不存在")
		}
		return err
	}
	if info.IsDir() {
		return errors.New("yt-dlp Cookie 路径不能是目录")
	}
	return nil
}

func bilibiliSessionFromSettings(settings Settings) BilibiliSession {
	status := settings.BilibiliCheckStatus
	if status == "" && settings.BilibiliSessdata != "" && settings.BilibiliJct != "" {
		status = "unchecked"
	}
	if status == "" {
		status = "missing"
	}
	return BilibiliSession{
		LoggedIn:     settings.BilibiliSessdata != "" && settings.BilibiliJct != "",
		Mid:          settings.BilibiliMid,
		Uname:        settings.BilibiliUname,
		Face:         settings.BilibiliFace,
		FaceLocal:    settings.BilibiliFaceLocal,
		LoginAt:      settings.BilibiliLoginAt,
		Status:       status,
		CheckedAt:    settings.BilibiliCheckedAt,
		Level:        settings.BilibiliLevel,
		Sex:          settings.BilibiliSex,
		Sign:         settings.BilibiliSign,
		VipStatus:    settings.BilibiliVipStatus,
		VipType:      settings.BilibiliVipType,
		VipLabel:     settings.BilibiliVipLabel,
		VipDueDate:   settings.BilibiliVipDueDate,
		SeniorMember: settings.BilibiliSeniorMember,
	}
}

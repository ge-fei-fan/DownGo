package domain

import "time"

const (
	PlatformYouTube  = "youtube"
	PlatformBilibili = "bilibili"

	StatusResolving      = "resolving"
	StatusQueued         = "queued"
	StatusDownloading    = "downloading"
	StatusPostprocessing = "postprocessing"
	StatusCompleted      = "completed"
	StatusFailed         = "failed"
	StatusCanceled       = "canceled"
)

type DownloadItem struct {
	ID              int64      `json:"id"`
	SourceURL       string     `json:"sourceUrl"`
	NormalizedURL   string     `json:"normalizedUrl"`
	Platform        string     `json:"platform"`
	VideoID         string     `json:"videoId"`
	Title           string     `json:"title"`
	ThumbnailURL    string     `json:"thumbnailUrl"`
	QualityLabel    string     `json:"qualityLabel"`
	Container       string     `json:"container"`
	OutputFilename  string     `json:"outputFilename"`
	OutputPath      string     `json:"outputPath"`
	Status          string     `json:"status"`
	ProgressPercent float64    `json:"progressPercent"`
	SpeedBPS        float64    `json:"speedBps"`
	ETASeconds      int64      `json:"etaSeconds"`
	ErrorMessage    string     `json:"errorMessage"`
	ProcessPID      int        `json:"processPid"`
	CreatedAt       time.Time  `json:"createdAt"`
	StartedAt       *time.Time `json:"startedAt"`
	CompletedAt     *time.Time `json:"completedAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

type InspectResult struct {
	Platform           string        `json:"platform"`
	NormalizedURL      string        `json:"normalizedUrl"`
	VideoID            string        `json:"videoId"`
	Title              string        `json:"title"`
	ThumbnailURL       string        `json:"thumbnailUrl"`
	QualityLabel       string        `json:"qualityLabel"`
	Container          string        `json:"container"`
	DurationSeconds    int64         `json:"durationSeconds"`
	EstimatedSizeBytes int64         `json:"estimatedSizeBytes"`
	SuggestedFilename  string        `json:"suggestedFilename"`
	DuplicateOf        *DownloadItem `json:"duplicateOf,omitempty"`
}

type PagedDownloads struct {
	Items    []DownloadItem `json:"items"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"pageSize"`
}

type DownloadEvent struct {
	Type string       `json:"type"`
	Item DownloadItem `json:"item"`
}

type FavoriteFolder struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

type FavoriteSubscription struct {
	MediaID       int64      `json:"mediaId"`
	Title         string     `json:"title"`
	Enabled       bool       `json:"enabled"`
	LastCheckedAt *time.Time `json:"lastCheckedAt,omitempty"`
	LastError     string     `json:"lastError"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type FavoriteMedia struct {
	ID    int64  `json:"id"`
	Type  int    `json:"type"`
	Title string `json:"title"`
	Bvid  string `json:"bvid"`
}

type FavoriteOrigin struct {
	MediaID      int64
	ResourceID   int64
	ResourceType int
	Bvid         string
	Title        string
}

type DiskTemperatureSample struct {
	DeviceID           string
	FriendlyName       string
	SerialNumber       string
	MediaType          string
	TemperatureCelsius *int
	TemperatureError   string
	SampledAt          time.Time
}

const (
	NotificationTypeDiskTemperature = "disk_temperature"

	NotificationChannelBark = "bark"

	NotificationStatusSent     = "sent"
	NotificationStatusFailed   = "failed"
	NotificationStatusDisabled = "disabled"
)

type NotificationRule struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	Enabled          bool      `json:"enabled"`
	ThresholdCelsius int       `json:"thresholdCelsius"`
	BarkEnabled      bool      `json:"barkEnabled"`
	BarkServerURL    string    `json:"barkServerUrl"`
	BarkDeviceKey    string    `json:"barkDeviceKey"`
	BarkDeviceKeySet bool      `json:"barkDeviceKeySet"`
	CooldownMinutes  int       `json:"cooldownMinutes"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type NotificationRuleUpdate struct {
	Enabled            bool
	ThresholdCelsius   int
	BarkEnabled        bool
	BarkServerURL      string
	BarkDeviceKey      string
	ClearBarkDeviceKey bool
}

type NotificationRecord struct {
	ID                 int64      `json:"id"`
	Type               string     `json:"type"`
	Channel            string     `json:"channel"`
	Title              string     `json:"title"`
	Body               string     `json:"body"`
	Status             string     `json:"status"`
	ErrorMessage       string     `json:"errorMessage"`
	DiskKey            string     `json:"diskKey"`
	DeviceID           string     `json:"deviceId"`
	FriendlyName       string     `json:"friendlyName"`
	SerialNumber       string     `json:"serialNumber"`
	TemperatureCelsius int        `json:"temperatureCelsius"`
	ThresholdCelsius   int        `json:"thresholdCelsius"`
	SuppressedCount    int        `json:"suppressedCount"`
	LastSuppressedAt   *time.Time `json:"lastSuppressedAt,omitempty"`
	SentAt             *time.Time `json:"sentAt,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
}

type PagedNotifications struct {
	Items    []NotificationRecord `json:"items"`
	Total    int                  `json:"total"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
}

type ScheduledTask struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	Description            string     `json:"description"`
	Enabled                bool       `json:"enabled"`
	IntervalMinutes        int        `json:"intervalMinutes"`
	DefaultIntervalMinutes int        `json:"defaultIntervalMinutes"`
	LastRunAt              *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt              *time.Time `json:"nextRunAt,omitempty"`
	LastError              string     `json:"lastError"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

type ScheduledTaskUpdate struct {
	Enabled         bool
	IntervalMinutes int
}

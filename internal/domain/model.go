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

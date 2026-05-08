package domain

import "time"

const (
	PlatformYouTube = "youtube"

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
	NormalizedURL      string        `json:"normalizedUrl"`
	VideoID            string        `json:"videoId"`
	Title              string        `json:"title"`
	ThumbnailURL       string        `json:"thumbnailUrl"`
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

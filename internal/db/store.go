package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/util"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(baseDir string) (*Store, error) {
	dataDir := filepath.Join(baseDir, "data")
	if err := util.EnsureDir(dataDir); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "app.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)

	if _, err := conn.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec(schema); err != nil {
		return nil, err
	}
	if err := ensureDownloadColumns(conn); err != nil {
		return nil, err
	}
	if err := ensureDownloadIndexes(conn); err != nil {
		return nil, err
	}

	return &Store{db: conn}, nil
}

func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) ListSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]string{}
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		result[key] = value
	}
	return result, rows.Err()
}

func (s *Store) UpsertSettings(values map[string]string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT INTO settings(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for key, value := range values {
		if _, err := stmt.Exec(key, value); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) CreateDownload(item domain.DownloadItem) (domain.DownloadItem, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(`
		INSERT INTO downloads(
			source_url, normalized_url, platform, video_id, title, thumbnail_url, quality_label, container,
			output_filename, output_path, status, progress_percent, speed_bps,
			eta_seconds, error_message, process_pid, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.SourceURL, item.NormalizedURL, item.Platform, item.VideoID, item.Title, item.ThumbnailURL, item.QualityLabel, item.Container,
		item.OutputFilename, item.OutputPath, item.Status, item.ProgressPercent, item.SpeedBPS,
		item.ETASeconds, item.ErrorMessage, item.ProcessPID, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.DownloadItem{}, ErrDuplicate
		}
		return domain.DownloadItem{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.DownloadItem{}, err
	}
	item.ID = id
	item.CreatedAt = now
	item.UpdatedAt = now
	return item, nil
}

func (s *Store) GetDownload(id int64) (domain.DownloadItem, error) {
	row := s.db.QueryRow(downloadSelectByIDSQL, id)
	item, err := scanDownload(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.DownloadItem{}, ErrNotFound
		}
		return domain.DownloadItem{}, err
	}
	return item, nil
}

func (s *Store) FindByVideoID(videoID string) ([]domain.DownloadItem, error) {
	rows, err := s.db.Query(downloadSelectByVideoIDSQL, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDownloads(rows)
}

func (s *Store) FindByPlatformVideoID(platform string, videoID string) ([]domain.DownloadItem, error) {
	rows, err := s.db.Query(downloadSelectByPlatformVideoIDSQL, platform, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDownloads(rows)
}

func (s *Store) UpdateProgress(id int64, status string, percent float64, speed float64, eta int64, pid int, errMsg string, qualityLabel string, container string, startedAt *time.Time, completedAt *time.Time) (domain.DownloadItem, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE downloads
		SET status = ?, progress_percent = ?, speed_bps = ?, eta_seconds = ?,
			process_pid = ?, error_message = ?, started_at = COALESCE(started_at, ?),
			completed_at = ?, quality_label = COALESCE(NULLIF(?, ''), quality_label),
			container = COALESCE(NULLIF(?, ''), container), updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, status, percent, speed, eta, pid, errMsg, startedAt, completedAt, qualityLabel, container, now, id)
	if err != nil {
		return domain.DownloadItem{}, err
	}
	return s.GetDownload(id)
}

func (s *Store) UpdateMetadata(id int64, item domain.DownloadItem, status string, errMsg string) (domain.DownloadItem, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE downloads
		SET normalized_url = ?, platform = ?, video_id = ?, title = ?, thumbnail_url = ?,
			quality_label = ?, container = ?, output_filename = ?, output_path = ?,
			status = ?, error_message = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, item.NormalizedURL, item.Platform, item.VideoID, item.Title, item.ThumbnailURL,
		item.QualityLabel, item.Container, item.OutputFilename, item.OutputPath,
		status, errMsg, now, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.DownloadItem{}, ErrDuplicate
		}
		return domain.DownloadItem{}, err
	}
	return s.GetDownload(id)
}

func (s *Store) MarkDeleted(id int64) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(`
		UPDATE downloads
		SET deleted_at = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL
	`, now, now, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListDownloads(view string, page int, pageSize int) (domain.PagedDownloads, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	where := `deleted_at IS NULL AND status = 'completed'`
	if view == "active" {
		where = `deleted_at IS NULL AND status IN ('resolving', 'queued', 'downloading', 'postprocessing', 'failed')`
	}

	countRow := s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM downloads WHERE %s`, where))
	var total int
	if err := countRow.Scan(&total); err != nil {
		return domain.PagedDownloads{}, err
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT %s FROM downloads
		WHERE %s
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, downloadSelectColumns, where), pageSize, offset)
	if err != nil {
		return domain.PagedDownloads{}, err
	}
	defer rows.Close()

	items, err := scanDownloads(rows)
	if err != nil {
		return domain.PagedDownloads{}, err
	}

	return domain.PagedDownloads{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *Store) MarkStaleActiveAsFailed() error {
	_, err := s.db.Exec(`
		UPDATE downloads
		SET status = ?, error_message = ?, process_pid = 0, updated_at = CURRENT_TIMESTAMP
		WHERE deleted_at IS NULL AND status IN (?, ?, ?, ?)
	`, domain.StatusFailed, "服务重启导致任务中断", domain.StatusResolving, domain.StatusQueued, domain.StatusDownloading, domain.StatusPostprocessing)
	return err
}

func (s *Store) MarkMissingCompletedAsFailed() ([]domain.DownloadItem, error) {
	rows, err := s.db.Query(`
		SELECT `+downloadSelectColumns+` FROM downloads
		WHERE deleted_at IS NULL AND status = ?
	`, domain.StatusCompleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items, err := scanDownloads(rows)
	if err != nil {
		return nil, err
	}

	updated := make([]domain.DownloadItem, 0)
	for _, item := range items {
		if util.FileExists(item.OutputPath) {
			continue
		}
		next, err := s.UpdateProgress(item.ID, domain.StatusFailed, 0, 0, 0, 0, "文件丢失", item.QualityLabel, item.Container, item.StartedAt, nil)
		if err != nil {
			return nil, err
		}
		updated = append(updated, next)
	}
	return updated, nil
}

var (
	ErrNotFound  = errors.New("记录不存在")
	ErrDuplicate = errors.New("下载任务重复")
)

const downloadSelectColumns = `
id, source_url, normalized_url, platform, video_id, title, thumbnail_url, quality_label, container,
output_filename, output_path, status, progress_percent, speed_bps, eta_seconds,
error_message, process_pid, created_at, started_at, completed_at, updated_at, deleted_at
`

const downloadSelectByIDSQL = `SELECT ` + downloadSelectColumns + ` FROM downloads WHERE id = ?`
const downloadSelectByVideoIDSQL = `SELECT ` + downloadSelectColumns + ` FROM downloads WHERE deleted_at IS NULL AND video_id = ? ORDER BY created_at DESC`
const downloadSelectByPlatformVideoIDSQL = `SELECT ` + downloadSelectColumns + ` FROM downloads WHERE deleted_at IS NULL AND platform = ? AND video_id = ? ORDER BY created_at DESC`

type scanner interface {
	Scan(dest ...any) error
}

func scanDownloads(rows *sql.Rows) ([]domain.DownloadItem, error) {
	items := make([]domain.DownloadItem, 0)
	for rows.Next() {
		item, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanDownload(row scanner) (domain.DownloadItem, error) {
	var item domain.DownloadItem
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var deletedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.SourceURL, &item.NormalizedURL, &item.Platform, &item.VideoID, &item.Title,
		&item.ThumbnailURL, &item.QualityLabel, &item.Container, &item.OutputFilename, &item.OutputPath, &item.Status, &item.ProgressPercent,
		&item.SpeedBPS, &item.ETASeconds, &item.ErrorMessage, &item.ProcessPID, &item.CreatedAt,
		&startedAt, &completedAt, &item.UpdatedAt, &deletedAt,
	); err != nil {
		return domain.DownloadItem{}, err
	}
	if startedAt.Valid {
		item.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	if deletedAt.Valid {
		item.DeletedAt = &deletedAt.Time
	}
	return item, nil
}

func ensureDownloadColumns(conn *sql.DB) error {
	statements := []string{
		`ALTER TABLE downloads ADD COLUMN quality_label TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE downloads ADD COLUMN container TEXT NOT NULL DEFAULT ''`,
	}

	for _, statement := range statements {
		if _, err := conn.Exec(statement); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

func ensureDownloadIndexes(conn *sql.DB) error {
	if _, err := conn.Exec(`DROP INDEX IF EXISTS ux_downloads_video_active`); err != nil {
		return err
	}
	_, err := conn.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS ux_downloads_video_active
		ON downloads(platform, video_id)
		WHERE deleted_at IS NULL
		  AND video_id <> ''
		  AND status IN ('resolving', 'queued', 'downloading', 'postprocessing', 'completed')
	`)
	return err
}

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

func (s *Store) GetFavoriteSubscription() (domain.FavoriteSubscription, error) {
	row := s.db.QueryRow(`
		SELECT media_id, title, enabled, last_checked_at, last_error, updated_at
		FROM bilibili_favorite_subscription
		WHERE id = 1
	`)
	return scanFavoriteSubscription(row)
}

func (s *Store) UpsertFavoriteSubscription(sub domain.FavoriteSubscription) (domain.FavoriteSubscription, error) {
	now := time.Now().UTC()
	enabled := 0
	if sub.Enabled {
		enabled = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO bilibili_favorite_subscription(id, media_id, title, enabled, last_checked_at, last_error, updated_at)
		VALUES(1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			media_id=excluded.media_id,
			title=excluded.title,
			enabled=excluded.enabled,
			last_checked_at=excluded.last_checked_at,
			last_error=excluded.last_error,
			updated_at=excluded.updated_at
	`, sub.MediaID, sub.Title, enabled, sub.LastCheckedAt, sub.LastError, now)
	if err != nil {
		return domain.FavoriteSubscription{}, err
	}
	return s.GetFavoriteSubscription()
}

func (s *Store) UpdateFavoriteSubscriptionCheck(lastCheckedAt time.Time, lastError string) error {
	_, err := s.db.Exec(`
		INSERT INTO bilibili_favorite_subscription(id, last_checked_at, last_error, updated_at)
		VALUES(1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			last_checked_at=excluded.last_checked_at,
			last_error=excluded.last_error,
			updated_at=excluded.updated_at
	`, lastCheckedAt.UTC(), lastError, time.Now().UTC())
	return err
}

func (s *Store) UpsertFavoriteResource(origin domain.FavoriteOrigin, status string, lastError string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO bilibili_favorite_resources(
			media_id, resource_id, resource_type, bvid, title, status, last_error, created_at, updated_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(media_id, resource_id, resource_type) DO UPDATE SET
			bvid=excluded.bvid,
			title=excluded.title,
			status=excluded.status,
			last_error=excluded.last_error,
			updated_at=excluded.updated_at
	`, origin.MediaID, origin.ResourceID, origin.ResourceType, origin.Bvid, origin.Title, status, lastError, now, now)
	return err
}

func (s *Store) MarkFavoriteResourceRemoved(origin domain.FavoriteOrigin) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		UPDATE bilibili_favorite_resources
		SET status = 'removed', last_error = '', removed_at = ?, updated_at = ?
		WHERE media_id = ? AND resource_id = ? AND resource_type = ?
	`, now, now, origin.MediaID, origin.ResourceID, origin.ResourceType)
	return err
}

func (s *Store) LinkFavoriteDownload(downloadID int64, origin domain.FavoriteOrigin) error {
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO bilibili_favorite_downloads(
			download_id, media_id, resource_id, resource_type, created_at
		) VALUES(?, ?, ?, ?, ?)
	`, downloadID, origin.MediaID, origin.ResourceID, origin.ResourceType, time.Now().UTC())
	return err
}

func (s *Store) FavoriteOriginsForDownload(downloadID int64) ([]domain.FavoriteOrigin, error) {
	rows, err := s.db.Query(`
		SELECT r.media_id, r.resource_id, r.resource_type, r.bvid, r.title
		FROM bilibili_favorite_downloads d
		JOIN bilibili_favorite_resources r
		  ON r.media_id = d.media_id
		 AND r.resource_id = d.resource_id
		 AND r.resource_type = d.resource_type
		WHERE d.download_id = ?
	`, downloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFavoriteOrigins(rows)
}

func (s *Store) DownloadsForFavoriteOrigin(origin domain.FavoriteOrigin) ([]domain.DownloadItem, error) {
	rows, err := s.db.Query(`
		SELECT `+downloadSelectColumnsD+`
		FROM bilibili_favorite_downloads fd
		JOIN downloads d ON d.id = fd.download_id
		WHERE fd.media_id = ? AND fd.resource_id = ? AND fd.resource_type = ? AND d.deleted_at IS NULL
		ORDER BY d.id ASC
	`, origin.MediaID, origin.ResourceID, origin.ResourceType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDownloads(rows)
}

func (s *Store) PendingFavoriteResources() ([]domain.FavoriteOrigin, error) {
	rows, err := s.db.Query(`
		SELECT media_id, resource_id, resource_type, bvid, title
		FROM bilibili_favorite_resources
		WHERE status <> 'removed'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFavoriteOrigins(rows)
}

func scanFavoriteSubscription(row scanner) (domain.FavoriteSubscription, error) {
	var sub domain.FavoriteSubscription
	var enabled int
	var checkedAt sql.NullTime
	err := row.Scan(&sub.MediaID, &sub.Title, &enabled, &checkedAt, &sub.LastError, &sub.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.FavoriteSubscription{}, nil
		}
		return domain.FavoriteSubscription{}, err
	}
	sub.Enabled = enabled == 1
	if checkedAt.Valid {
		sub.LastCheckedAt = &checkedAt.Time
	}
	return sub, nil
}

func scanFavoriteOrigins(rows *sql.Rows) ([]domain.FavoriteOrigin, error) {
	origins := make([]domain.FavoriteOrigin, 0)
	for rows.Next() {
		var origin domain.FavoriteOrigin
		if err := rows.Scan(&origin.MediaID, &origin.ResourceID, &origin.ResourceType, &origin.Bvid, &origin.Title); err != nil {
			return nil, err
		}
		origins = append(origins, origin)
	}
	return origins, rows.Err()
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

const downloadSelectColumnsD = `
d.id, d.source_url, d.normalized_url, d.platform, d.video_id, d.title, d.thumbnail_url, d.quality_label, d.container,
d.output_filename, d.output_path, d.status, d.progress_percent, d.speed_bps, d.eta_seconds,
d.error_message, d.process_pid, d.created_at, d.started_at, d.completed_at, d.updated_at, d.deleted_at
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

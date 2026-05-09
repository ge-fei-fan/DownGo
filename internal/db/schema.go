package db

const schema = `
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS downloads (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_url TEXT NOT NULL,
  normalized_url TEXT NOT NULL,
  platform TEXT NOT NULL,
  video_id TEXT NOT NULL,
  title TEXT NOT NULL,
  thumbnail_url TEXT NOT NULL DEFAULT '',
  quality_label TEXT NOT NULL DEFAULT '',
  container TEXT NOT NULL DEFAULT '',
  output_filename TEXT NOT NULL,
  output_path TEXT NOT NULL,
  status TEXT NOT NULL,
  progress_percent REAL NOT NULL DEFAULT 0,
  speed_bps REAL NOT NULL DEFAULT 0,
  eta_seconds INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  process_pid INTEGER NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at DATETIME,
  completed_at DATETIME,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_downloads_video_active
ON downloads(platform, video_id)
WHERE deleted_at IS NULL
  AND video_id <> ''
  AND status IN ('resolving', 'queued', 'downloading', 'postprocessing', 'completed');

CREATE INDEX IF NOT EXISTS idx_downloads_view
ON downloads(deleted_at, status, created_at DESC);

CREATE TABLE IF NOT EXISTS bilibili_favorite_subscription (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  media_id INTEGER NOT NULL DEFAULT 0,
  title TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 0,
  last_checked_at DATETIME,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bilibili_favorite_resources (
  media_id INTEGER NOT NULL,
  resource_id INTEGER NOT NULL,
  resource_type INTEGER NOT NULL,
  bvid TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  removed_at DATETIME,
  PRIMARY KEY(media_id, resource_id, resource_type)
);

CREATE TABLE IF NOT EXISTS bilibili_favorite_downloads (
  download_id INTEGER NOT NULL,
  media_id INTEGER NOT NULL,
  resource_id INTEGER NOT NULL,
  resource_type INTEGER NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY(download_id, media_id, resource_id, resource_type)
);

CREATE INDEX IF NOT EXISTS idx_bilibili_favorite_downloads_resource
ON bilibili_favorite_downloads(media_id, resource_id, resource_type);
`

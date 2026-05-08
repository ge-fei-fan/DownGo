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
  AND status IN ('queued', 'downloading', 'postprocessing', 'completed');

CREATE INDEX IF NOT EXISTS idx_downloads_view
ON downloads(deleted_at, status, created_at DESC);
`

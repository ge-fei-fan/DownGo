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

CREATE TABLE IF NOT EXISTS disk_temperature_samples (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  device_id TEXT NOT NULL,
  friendly_name TEXT NOT NULL DEFAULT '',
  serial_number TEXT NOT NULL DEFAULT '',
  media_type TEXT NOT NULL DEFAULT '',
  temperature_celsius INTEGER,
  temperature_error TEXT NOT NULL DEFAULT '',
  sampled_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_disk_temperature_samples_time
ON disk_temperature_samples(sampled_at);

CREATE INDEX IF NOT EXISTS idx_disk_temperature_samples_disk_time
ON disk_temperature_samples(device_id, serial_number, sampled_at);

CREATE TABLE IF NOT EXISTS notification_rules (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 0,
  threshold_celsius INTEGER NOT NULL DEFAULT 50,
  bark_enabled INTEGER NOT NULL DEFAULT 0,
  bark_server_url TEXT NOT NULL DEFAULT 'https://api.day.app',
  bark_device_key TEXT NOT NULL DEFAULT '',
  cooldown_minutes INTEGER NOT NULL DEFAULT 60,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS notification_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  type TEXT NOT NULL,
  channel TEXT NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  status TEXT NOT NULL,
  error_message TEXT NOT NULL DEFAULT '',
  disk_key TEXT NOT NULL DEFAULT '',
  device_id TEXT NOT NULL DEFAULT '',
  friendly_name TEXT NOT NULL DEFAULT '',
  serial_number TEXT NOT NULL DEFAULT '',
  temperature_celsius INTEGER NOT NULL DEFAULT 0,
  threshold_celsius INTEGER NOT NULL DEFAULT 0,
  suppressed_count INTEGER NOT NULL DEFAULT 0,
  last_suppressed_at DATETIME,
  sent_at DATETIME,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_notification_records_created
ON notification_records(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notification_records_type_disk
ON notification_records(type, disk_key, created_at DESC);

CREATE TABLE IF NOT EXISTS scheduled_tasks (
  id TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 1,
  interval_minutes INTEGER NOT NULL,
  last_run_at DATETIME,
  next_run_at DATETIME,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

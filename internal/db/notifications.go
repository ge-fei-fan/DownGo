package db

import (
	"context"
	"database/sql"
	"time"

	"example.com/downgo/internal/domain"
)

const diskTemperatureRuleID = "disk-temperature"

type notificationRuleRow struct {
	Rule          domain.NotificationRule
	BarkDeviceKey string
}

func (s *Store) EnsureDefaultNotificationRules(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_rules(
			id, name, type, enabled, threshold_celsius, bark_enabled, bark_server_url, bark_device_key, cooldown_minutes, updated_at
		) VALUES (?, ?, ?, 0, 50, 0, 'https://api.day.app', '', 60, ?)
		ON CONFLICT(id) DO NOTHING
	`, diskTemperatureRuleID, "磁盘温度告警", domain.NotificationTypeDiskTemperature, time.Now().UTC())
	return err
}

func (s *Store) ListNotificationRules(ctx context.Context) ([]domain.NotificationRule, error) {
	if err := s.EnsureDefaultNotificationRules(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, type, enabled, threshold_celsius, bark_enabled, bark_server_url,
		       bark_device_key, bark_device_key <> '', cooldown_minutes, updated_at
		FROM notification_rules
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rules := make([]domain.NotificationRule, 0)
	for rows.Next() {
		rule, err := scanNotificationRule(rows)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (s *Store) GetDiskTemperatureNotificationRule(ctx context.Context) (domain.NotificationRule, string, error) {
	if err := s.EnsureDefaultNotificationRules(ctx); err != nil {
		return domain.NotificationRule{}, "", err
	}
	var row notificationRuleRow
	var enabled int
	var barkEnabled int
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, enabled, threshold_celsius, bark_enabled, bark_server_url,
		       bark_device_key, cooldown_minutes, updated_at
		FROM notification_rules
		WHERE id = ?
	`, diskTemperatureRuleID).Scan(
		&row.Rule.ID,
		&row.Rule.Name,
		&row.Rule.Type,
		&enabled,
		&row.Rule.ThresholdCelsius,
		&barkEnabled,
		&row.Rule.BarkServerURL,
		&row.BarkDeviceKey,
		&row.Rule.CooldownMinutes,
		&row.Rule.UpdatedAt,
	)
	if err != nil {
		return domain.NotificationRule{}, "", err
	}
	row.Rule.Enabled = enabled == 1
	row.Rule.BarkEnabled = barkEnabled == 1
	row.Rule.BarkDeviceKey = row.BarkDeviceKey
	row.Rule.BarkDeviceKeySet = row.BarkDeviceKey != ""
	return row.Rule, row.BarkDeviceKey, nil
}

func (s *Store) UpdateDiskTemperatureNotificationRule(ctx context.Context, input domain.NotificationRuleUpdate) (domain.NotificationRule, error) {
	if err := s.EnsureDefaultNotificationRules(ctx); err != nil {
		return domain.NotificationRule{}, err
	}
	current, currentKey, err := s.GetDiskTemperatureNotificationRule(ctx)
	if err != nil {
		return domain.NotificationRule{}, err
	}
	deviceKey := currentKey
	if input.ClearBarkDeviceKey {
		deviceKey = ""
	}
	if input.BarkDeviceKey != "" {
		deviceKey = input.BarkDeviceKey
	}
	now := time.Now().UTC()
	enabled := 0
	if input.Enabled {
		enabled = 1
	}
	barkEnabled := 0
	if input.BarkEnabled {
		barkEnabled = 1
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE notification_rules
		SET enabled = ?, threshold_celsius = ?, bark_enabled = ?, bark_server_url = ?,
		    bark_device_key = ?, cooldown_minutes = ?, updated_at = ?
		WHERE id = ?
	`, enabled, input.ThresholdCelsius, barkEnabled, input.BarkServerURL, deviceKey, current.CooldownMinutes, now, diskTemperatureRuleID)
	if err != nil {
		return domain.NotificationRule{}, err
	}
	rule, _, err := s.GetDiskTemperatureNotificationRule(ctx)
	return rule, err
}

func (s *Store) CreateNotificationRecord(ctx context.Context, record domain.NotificationRecord) (domain.NotificationRecord, error) {
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_records(
			type, channel, title, body, status, error_message, disk_key, device_id, friendly_name,
			serial_number, temperature_celsius, threshold_celsius, suppressed_count,
			last_suppressed_at, sent_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.Type, record.Channel, record.Title, record.Body, record.Status, record.ErrorMessage,
		record.DiskKey, record.DeviceID, record.FriendlyName, record.SerialNumber,
		record.TemperatureCelsius, record.ThresholdCelsius, record.SuppressedCount,
		record.LastSuppressedAt, record.SentAt, record.CreatedAt)
	if err != nil {
		return domain.NotificationRecord{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.NotificationRecord{}, err
	}
	return s.GetNotificationRecord(ctx, id)
}

func (s *Store) GetNotificationRecord(ctx context.Context, id int64) (domain.NotificationRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, channel, title, body, status, error_message, disk_key, device_id,
		       friendly_name, serial_number, temperature_celsius, threshold_celsius,
		       suppressed_count, last_suppressed_at, sent_at, created_at
		FROM notification_records
		WHERE id = ?
	`, id)
	return scanNotificationRecord(row)
}

func (s *Store) LatestNotificationRecordForDisk(ctx context.Context, typ string, diskKey string) (domain.NotificationRecord, bool, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, channel, title, body, status, error_message, disk_key, device_id,
		       friendly_name, serial_number, temperature_celsius, threshold_celsius,
		       suppressed_count, last_suppressed_at, sent_at, created_at
		FROM notification_records
		WHERE type = ? AND disk_key = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, typ, diskKey)
	record, err := scanNotificationRecord(row)
	if err == sql.ErrNoRows {
		return domain.NotificationRecord{}, false, nil
	}
	if err != nil {
		return domain.NotificationRecord{}, false, err
	}
	return record, true, nil
}

func (s *Store) IncrementNotificationSuppressed(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE notification_records
		SET suppressed_count = suppressed_count + 1, last_suppressed_at = ?
		WHERE id = ?
	`, at.UTC(), id)
	return err
}

func (s *Store) ListNotificationRecords(ctx context.Context, page int, pageSize int) (domain.PagedNotifications, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_records`).Scan(&total); err != nil {
		return domain.PagedNotifications{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, channel, title, body, status, error_message, disk_key, device_id,
		       friendly_name, serial_number, temperature_celsius, threshold_celsius,
		       suppressed_count, last_suppressed_at, sent_at, created_at
		FROM notification_records
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, pageSize, offset)
	if err != nil {
		return domain.PagedNotifications{}, err
	}
	defer rows.Close()

	items := make([]domain.NotificationRecord, 0)
	for rows.Next() {
		record, err := scanNotificationRecord(rows)
		if err != nil {
			return domain.PagedNotifications{}, err
		}
		items = append(items, record)
	}
	if err := rows.Err(); err != nil {
		return domain.PagedNotifications{}, err
	}
	return domain.PagedNotifications{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

type notificationRuleScanner interface {
	Scan(dest ...any) error
}

func scanNotificationRule(scanner notificationRuleScanner) (domain.NotificationRule, error) {
	var rule domain.NotificationRule
	var enabled int
	var barkEnabled int
	var barkDeviceKeySet int
	if err := scanner.Scan(
		&rule.ID,
		&rule.Name,
		&rule.Type,
		&enabled,
		&rule.ThresholdCelsius,
		&barkEnabled,
		&rule.BarkServerURL,
		&rule.BarkDeviceKey,
		&barkDeviceKeySet,
		&rule.CooldownMinutes,
		&rule.UpdatedAt,
	); err != nil {
		return domain.NotificationRule{}, err
	}
	rule.Enabled = enabled == 1
	rule.BarkEnabled = barkEnabled == 1
	rule.BarkDeviceKeySet = barkDeviceKeySet == 1
	return rule, nil
}

type notificationRecordScanner interface {
	Scan(dest ...any) error
}

func scanNotificationRecord(scanner notificationRecordScanner) (domain.NotificationRecord, error) {
	var record domain.NotificationRecord
	var lastSuppressedAt sql.NullTime
	var sentAt sql.NullTime
	if err := scanner.Scan(
		&record.ID,
		&record.Type,
		&record.Channel,
		&record.Title,
		&record.Body,
		&record.Status,
		&record.ErrorMessage,
		&record.DiskKey,
		&record.DeviceID,
		&record.FriendlyName,
		&record.SerialNumber,
		&record.TemperatureCelsius,
		&record.ThresholdCelsius,
		&record.SuppressedCount,
		&lastSuppressedAt,
		&sentAt,
		&record.CreatedAt,
	); err != nil {
		return domain.NotificationRecord{}, err
	}
	if lastSuppressedAt.Valid {
		record.LastSuppressedAt = &lastSuppressedAt.Time
	}
	if sentAt.Valid {
		record.SentAt = &sentAt.Time
	}
	return record, nil
}

package db

import (
	"context"
	"database/sql"
	"time"

	"example.com/downgo/internal/domain"
)

func (s *Store) EnsureScheduledTask(ctx context.Context, task domain.ScheduledTask) error {
	enabled := 0
	if task.Enabled {
		enabled = 1
	}
	interval := task.IntervalMinutes
	if interval <= 0 {
		interval = task.DefaultIntervalMinutes
	}
	if interval <= 0 {
		interval = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO scheduled_tasks(id, enabled, interval_minutes, updated_at)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`, task.ID, enabled, interval, time.Now().UTC())
	return err
}

func (s *Store) ListScheduledTasks(ctx context.Context) ([]domain.ScheduledTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, enabled, interval_minutes, last_run_at, next_run_at, last_error, updated_at
		FROM scheduled_tasks
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := make([]domain.ScheduledTask, 0)
	for rows.Next() {
		task, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) GetScheduledTask(ctx context.Context, id string) (domain.ScheduledTask, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, enabled, interval_minutes, last_run_at, next_run_at, last_error, updated_at
		FROM scheduled_tasks
		WHERE id = ?
	`, id)
	return scanScheduledTask(row)
}

func (s *Store) UpdateScheduledTaskConfig(ctx context.Context, id string, input domain.ScheduledTaskUpdate) (domain.ScheduledTask, error) {
	current, err := s.GetScheduledTask(ctx, id)
	if err != nil {
		return domain.ScheduledTask{}, err
	}
	now := time.Now().UTC()
	var nextRunAt *time.Time
	if input.Enabled {
		if current.LastRunAt != nil {
			next := current.LastRunAt.Add(time.Duration(input.IntervalMinutes) * time.Minute)
			if next.Before(now) {
				next = now
			}
			nextRunAt = &next
		}
	}
	enabled := 0
	if input.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE scheduled_tasks
		SET enabled = ?, interval_minutes = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, enabled, input.IntervalMinutes, nextRunAt, now, id)
	if err != nil {
		return domain.ScheduledTask{}, err
	}
	return s.GetScheduledTask(ctx, id)
}

func (s *Store) UpdateScheduledTaskRun(ctx context.Context, id string, lastRunAt time.Time, nextRunAt *time.Time, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE scheduled_tasks
		SET last_run_at = ?, next_run_at = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`, lastRunAt.UTC(), nextRunAt, lastError, time.Now().UTC(), id)
	return err
}

func scanScheduledTask(scanner scanner) (domain.ScheduledTask, error) {
	var task domain.ScheduledTask
	var enabled int
	var lastRunAt sql.NullTime
	var nextRunAt sql.NullTime
	if err := scanner.Scan(
		&task.ID,
		&enabled,
		&task.IntervalMinutes,
		&lastRunAt,
		&nextRunAt,
		&task.LastError,
		&task.UpdatedAt,
	); err != nil {
		return domain.ScheduledTask{}, err
	}
	task.Enabled = enabled == 1
	if lastRunAt.Valid {
		value := lastRunAt.Time
		task.LastRunAt = &value
	}
	if nextRunAt.Valid {
		value := nextRunAt.Time
		task.NextRunAt = &value
	}
	return task, nil
}

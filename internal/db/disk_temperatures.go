package db

import (
	"context"
	"database/sql"
	"time"

	"example.com/downgo/internal/domain"
)

type DiskTemperatureSample = domain.DiskTemperatureSample

func (s *Store) InsertDiskTemperatureSamples(ctx context.Context, samples []DiskTemperatureSample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO disk_temperature_samples(
			device_id, friendly_name, serial_number, media_type, temperature_celsius, temperature_error, sampled_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sample := range samples {
		var temperature any
		if sample.TemperatureCelsius != nil {
			temperature = *sample.TemperatureCelsius
		}
		if _, err := stmt.ExecContext(ctx,
			sample.DeviceID,
			sample.FriendlyName,
			sample.SerialNumber,
			sample.MediaType,
			temperature,
			sample.TemperatureError,
			sample.SampledAt.UTC(),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListDiskTemperatureSamples(ctx context.Context, from time.Time, to time.Time, limit int) ([]DiskTemperatureSample, error) {
	if limit <= 0 {
		limit = 2000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, friendly_name, serial_number, media_type, temperature_celsius, temperature_error, sampled_at
		FROM disk_temperature_samples
		WHERE sampled_at >= ? AND sampled_at <= ?
		  AND LOWER(media_type) = 'hdd'
		ORDER BY sampled_at ASC, device_id ASC, serial_number ASC
		LIMIT ?
	`, from.UTC(), to.UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	samples := make([]DiskTemperatureSample, 0)
	for rows.Next() {
		var sample DiskTemperatureSample
		var temperature sql.NullInt64
		if err := rows.Scan(
			&sample.DeviceID,
			&sample.FriendlyName,
			&sample.SerialNumber,
			&sample.MediaType,
			&temperature,
			&sample.TemperatureError,
			&sample.SampledAt,
		); err != nil {
			return nil, err
		}
		if temperature.Valid {
			value := int(temperature.Int64)
			sample.TemperatureCelsius = &value
		}
		samples = append(samples, sample)
	}
	return samples, rows.Err()
}

func (s *Store) DeleteDiskTemperatureSamplesBefore(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM disk_temperature_samples WHERE sampled_at < ?`, cutoff.UTC())
	return err
}

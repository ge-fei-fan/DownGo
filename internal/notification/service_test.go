package notification

import (
	"context"
	"testing"
	"time"

	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/monitor"
)

type serviceTestStore struct {
	rule    domain.NotificationRule
	key     string
	records []domain.NotificationRecord
}

func (s *serviceTestStore) EnsureDefaultNotificationRules(context.Context) error {
	return nil
}

func (s *serviceTestStore) ListNotificationRules(context.Context) ([]domain.NotificationRule, error) {
	return []domain.NotificationRule{s.rule}, nil
}

func (s *serviceTestStore) GetDiskTemperatureNotificationRule(context.Context) (domain.NotificationRule, string, error) {
	return s.rule, s.key, nil
}

func (s *serviceTestStore) UpdateDiskTemperatureNotificationRule(_ context.Context, input domain.NotificationRuleUpdate) (domain.NotificationRule, error) {
	s.rule.Enabled = input.Enabled
	s.rule.ThresholdCelsius = input.ThresholdCelsius
	s.rule.BarkEnabled = input.BarkEnabled
	s.rule.BarkServerURL = input.BarkServerURL
	if input.BarkDeviceKey != "" {
		s.key = input.BarkDeviceKey
	}
	s.rule.BarkDeviceKey = s.key
	s.rule.BarkDeviceKeySet = s.key != ""
	return s.rule, nil
}

func (s *serviceTestStore) CreateNotificationRecord(_ context.Context, record domain.NotificationRecord) (domain.NotificationRecord, error) {
	record.ID = int64(len(s.records) + 1)
	s.records = append(s.records, record)
	return record, nil
}

func (s *serviceTestStore) LatestNotificationRecordForDisk(context.Context, string, string) (domain.NotificationRecord, bool, error) {
	return domain.NotificationRecord{}, false, nil
}

func (s *serviceTestStore) IncrementNotificationSuppressed(context.Context, int64, time.Time) error {
	return nil
}

func (s *serviceTestStore) ListNotificationRecords(context.Context, int, int) (domain.PagedNotifications, error) {
	return domain.PagedNotifications{Items: s.records, Total: len(s.records)}, nil
}

type serviceTestBark struct {
	calls []BarkMessage
}

func (b *serviceTestBark) Send(_ context.Context, _ string, message BarkMessage) error {
	b.calls = append(b.calls, message)
	return nil
}

func TestSendTestUsesSavedBarkConfigWithoutRuleChannel(t *testing.T) {
	store := &serviceTestStore{
		rule: domain.NotificationRule{
			Enabled:          true,
			ThresholdCelsius: 50,
			BarkEnabled:      false,
			BarkServerURL:    "https://api.day.app",
		},
		key: "device-key",
	}
	bark := &serviceTestBark{}
	service, err := NewService(store, bark)
	if err != nil {
		t.Fatal(err)
	}

	if err := service.SendTest(context.Background()); err != nil {
		t.Fatalf("SendTest error = %v", err)
	}
	if len(bark.calls) != 1 {
		t.Fatalf("bark calls = %d, want 1", len(bark.calls))
	}
}

func TestUpdateDiskTemperatureRuleAllowsBarkChannelWithoutKey(t *testing.T) {
	store := &serviceTestStore{
		rule: domain.NotificationRule{
			ThresholdCelsius: 50,
			BarkServerURL:    "https://api.day.app",
		},
	}
	service, err := NewService(store, &serviceTestBark{})
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.UpdateDiskTemperatureRule(context.Background(), domain.NotificationRuleUpdate{
		Enabled:          true,
		ThresholdCelsius: 55,
		BarkEnabled:      true,
		BarkServerURL:    "https://api.day.app",
	})
	if err != nil {
		t.Fatalf("UpdateDiskTemperatureRule error = %v", err)
	}
}

func TestCheckDiskTemperaturesRecordsDisabledWithoutChannel(t *testing.T) {
	store := &serviceTestStore{
		rule: domain.NotificationRule{
			Enabled:          true,
			ThresholdCelsius: 50,
			BarkEnabled:      false,
			BarkServerURL:    "https://api.day.app",
		},
	}
	bark := &serviceTestBark{}
	service, err := NewService(store, bark)
	if err != nil {
		t.Fatal(err)
	}

	service.CheckDiskTemperatures(context.Background(), temperatureSnapshot(60))

	if len(bark.calls) != 0 {
		t.Fatalf("bark calls = %d, want 0", len(bark.calls))
	}
	if len(store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(store.records))
	}
	record := store.records[0]
	if record.Status != domain.NotificationStatusDisabled {
		t.Fatalf("record status = %q, want %q", record.Status, domain.NotificationStatusDisabled)
	}
	if record.Channel != "" {
		t.Fatalf("record channel = %q, want empty", record.Channel)
	}
}

func TestCheckDiskTemperaturesSendsBarkWhenChannelConfigured(t *testing.T) {
	store := &serviceTestStore{
		rule: domain.NotificationRule{
			Enabled:          true,
			ThresholdCelsius: 50,
			BarkEnabled:      true,
			BarkServerURL:    "https://api.day.app",
		},
		key: "device-key",
	}
	bark := &serviceTestBark{}
	service, err := NewService(store, bark)
	if err != nil {
		t.Fatal(err)
	}

	service.CheckDiskTemperatures(context.Background(), temperatureSnapshot(60))

	if len(bark.calls) != 1 {
		t.Fatalf("bark calls = %d, want 1", len(bark.calls))
	}
	if len(store.records) != 1 {
		t.Fatalf("records = %d, want 1", len(store.records))
	}
	record := store.records[0]
	if record.Status != domain.NotificationStatusSent {
		t.Fatalf("record status = %q, want %q", record.Status, domain.NotificationStatusSent)
	}
	if record.Channel != domain.NotificationChannelBark {
		t.Fatalf("record channel = %q, want %q", record.Channel, domain.NotificationChannelBark)
	}
	if record.SentAt == nil {
		t.Fatal("record SentAt is nil")
	}
}

func temperatureSnapshot(value int) monitor.DiskTemperatureSnapshot {
	return monitor.DiskTemperatureSnapshot{
		Items: []monitor.DiskTemperatureStats{
			{
				DeviceID:           "disk0",
				FriendlyName:       "Disk 0",
				SerialNumber:       "serial0",
				TemperatureCelsius: &value,
			},
		},
	}
}

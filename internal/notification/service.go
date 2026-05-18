package notification

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/monitor"
)

type Store interface {
	EnsureDefaultNotificationRules(context.Context) error
	ListNotificationRules(context.Context) ([]domain.NotificationRule, error)
	GetDiskTemperatureNotificationRule(context.Context) (domain.NotificationRule, string, error)
	UpdateDiskTemperatureNotificationRule(context.Context, domain.NotificationRuleUpdate) (domain.NotificationRule, error)
	CreateNotificationRecord(context.Context, domain.NotificationRecord) (domain.NotificationRecord, error)
	LatestNotificationRecordForDisk(context.Context, string, string) (domain.NotificationRecord, bool, error)
	IncrementNotificationSuppressed(context.Context, int64, time.Time) error
	ListNotificationRecords(context.Context, int, int) (domain.PagedNotifications, error)
}

type BarkSender interface {
	Send(context.Context, string, BarkMessage) error
}

type Service struct {
	store Store
	bark  BarkSender
	now   func() time.Time
}

func NewService(store Store, bark BarkSender) (*Service, error) {
	if bark == nil {
		bark = NewBarkClient(nil)
	}
	s := &Service{
		store: store,
		bark:  bark,
		now:   func() time.Time { return time.Now().UTC() },
	}
	if err := store.EnsureDefaultNotificationRules(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Service) ListRules(ctx context.Context) ([]domain.NotificationRule, error) {
	return s.store.ListNotificationRules(ctx)
}

func (s *Service) UpdateDiskTemperatureRule(ctx context.Context, input domain.NotificationRuleUpdate) (domain.NotificationRule, error) {
	if input.ThresholdCelsius <= 0 || input.ThresholdCelsius > 120 {
		return domain.NotificationRule{}, errors.New("告警温度必须在 1 到 120 摄氏度之间")
	}
	input.BarkServerURL = normalizeBarkServerURL(input.BarkServerURL)
	if input.BarkEnabled && input.BarkServerURL == "" {
		return domain.NotificationRule{}, errors.New("请填写 Bark 服务端地址")
	}
	return s.store.UpdateDiskTemperatureNotificationRule(ctx, input)
}

func (s *Service) ListRecords(ctx context.Context, page int, pageSize int) (domain.PagedNotifications, error) {
	return s.store.ListNotificationRecords(ctx, page, pageSize)
}

func (s *Service) SendTest(ctx context.Context) error {
	rule, deviceKey, err := s.store.GetDiskTemperatureNotificationRule(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(deviceKey) == "" {
		return errors.New("请先填写 Bark device key")
	}
	return s.bark.Send(ctx, rule.BarkServerURL, BarkMessage{
		DeviceKey: deviceKey,
		Title:     "DownGo 通知测试",
		Body:      "Bark 通知配置可用。",
		Group:     "DownGo",
	})
}

func (s *Service) CheckDiskTemperatures(ctx context.Context, snapshot monitor.DiskTemperatureSnapshot) {
	rule, deviceKey, err := s.store.GetDiskTemperatureNotificationRule(ctx)
	if err != nil || !rule.Enabled {
		return
	}
	cooldown := time.Duration(rule.CooldownMinutes) * time.Minute
	if cooldown <= 0 {
		cooldown = time.Hour
	}
	now := s.now()
	for _, item := range snapshot.Items {
		if item.TemperatureCelsius == nil || *item.TemperatureCelsius <= rule.ThresholdCelsius {
			continue
		}
		diskKey := diskAlertKey(item)
		if diskKey == "" {
			continue
		}
		latest, ok, err := s.store.LatestNotificationRecordForDisk(ctx, domain.NotificationTypeDiskTemperature, diskKey)
		if err == nil && ok && now.Sub(latest.CreatedAt) < cooldown {
			_ = s.store.IncrementNotificationSuppressed(ctx, latest.ID, now)
			continue
		}

		title := "DownGo 磁盘温度告警"
		body := fmt.Sprintf("%s 当前温度 %d°C，已高于告警阈值 %d°C。", diskDisplayName(item), *item.TemperatureCelsius, rule.ThresholdCelsius)
		channel := ""
		status := domain.NotificationStatusDisabled
		errorMessage := ""
		var sentAt *time.Time
		if rule.BarkEnabled && strings.TrimSpace(deviceKey) != "" {
			channel = domain.NotificationChannelBark
			status = domain.NotificationStatusSent
			sentAt = &now
			if err := s.bark.Send(ctx, rule.BarkServerURL, BarkMessage{
				DeviceKey: deviceKey,
				Title:     title,
				Body:      body,
				Group:     "DownGo",
			}); err != nil {
				status = domain.NotificationStatusFailed
				errorMessage = err.Error()
				sentAt = nil
			}
		}
		_, _ = s.store.CreateNotificationRecord(ctx, domain.NotificationRecord{
			Type:               domain.NotificationTypeDiskTemperature,
			Channel:            channel,
			Title:              title,
			Body:               body,
			Status:             status,
			ErrorMessage:       errorMessage,
			DiskKey:            diskKey,
			DeviceID:           item.DeviceID,
			FriendlyName:       item.FriendlyName,
			SerialNumber:       item.SerialNumber,
			TemperatureCelsius: *item.TemperatureCelsius,
			ThresholdCelsius:   rule.ThresholdCelsius,
			SentAt:             sentAt,
			CreatedAt:          now,
		})
	}
}

func diskAlertKey(item monitor.DiskTemperatureStats) string {
	if strings.TrimSpace(item.SerialNumber) != "" {
		return strings.ToLower(strings.TrimSpace(item.SerialNumber))
	}
	if strings.TrimSpace(item.DeviceID) != "" {
		return strings.ToLower(strings.TrimSpace(item.DeviceID))
	}
	return strings.ToLower(strings.TrimSpace(item.FriendlyName))
}

func diskDisplayName(item monitor.DiskTemperatureStats) string {
	if strings.TrimSpace(item.FriendlyName) != "" {
		return item.FriendlyName
	}
	if strings.TrimSpace(item.SerialNumber) != "" {
		return item.SerialNumber
	}
	if strings.TrimSpace(item.DeviceID) != "" {
		return "磁盘 " + item.DeviceID
	}
	return "磁盘"
}

func normalizeBarkServerURL(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "https://api.day.app"
	}
	return strings.TrimRight(trimmed, "/")
}

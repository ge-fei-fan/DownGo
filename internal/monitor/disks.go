package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type DiskProvider interface {
	Disks(ctx context.Context) (DiskSnapshot, error)
	DiskTemperatures(ctx context.Context) (DiskTemperatureSnapshot, error)
}

type DiskSnapshot struct {
	Timestamp            time.Time           `json:"timestamp"`
	PhysicalDisks        []PhysicalDiskStats `json:"physicalDisks"`
	TemperatureUpdatedAt *time.Time          `json:"temperatureUpdatedAt,omitempty"`
	NextRefreshAt        *time.Time          `json:"nextRefreshAt,omitempty"`
	Errors               map[string]string   `json:"errors,omitempty"`
}

type DiskTemperatureSnapshot struct {
	UpdatedAt     *time.Time             `json:"updatedAt,omitempty"`
	NextRefreshAt *time.Time             `json:"nextRefreshAt,omitempty"`
	Items         []DiskTemperatureStats `json:"items"`
	Errors        map[string]string      `json:"errors,omitempty"`
}

type PhysicalDiskStats struct {
	DeviceID             string     `json:"deviceId"`
	FriendlyName         string     `json:"friendlyName"`
	SerialNumber         string     `json:"serialNumber"`
	MediaType            string     `json:"mediaType"`
	BusType              string     `json:"busType"`
	HealthStatus         string     `json:"healthStatus"`
	OperationalStatus    string     `json:"operationalStatus"`
	SizeBytes            uint64     `json:"sizeBytes"`
	TemperatureCelsius   *int       `json:"temperatureCelsius"`
	TemperatureUpdatedAt *time.Time `json:"temperatureUpdatedAt,omitempty"`
	TemperatureError     string     `json:"temperatureError,omitempty"`
}

type DiskTemperatureStats struct {
	DeviceID           string     `json:"deviceId"`
	FriendlyName       string     `json:"friendlyName"`
	SerialNumber       string     `json:"serialNumber"`
	TemperatureCelsius *int       `json:"temperatureCelsius"`
	TemperatureError   string     `json:"temperatureError,omitempty"`
	UpdatedAt          *time.Time `json:"updatedAt,omitempty"`
}

type DiskService struct {
	refreshInterval time.Duration
	physical        physicalDiskFunc

	mu          sync.RWMutex
	temps       DiskTemperatureSnapshot
	lastRefresh time.Time
	nextRefresh time.Time
}

type physicalDiskFunc func(context.Context) ([]PhysicalDiskStats, map[string]string)

func NewDiskService(refreshInterval time.Duration) *DiskService {
	if refreshInterval <= 0 {
		refreshInterval = 30 * time.Minute
	}
	return &DiskService{
		refreshInterval: refreshInterval,
		physical:        collectPhysicalDisks,
		temps:           DiskTemperatureSnapshot{Items: []DiskTemperatureStats{}},
	}
}

func (s *DiskService) Start(ctx context.Context) {
	go func() {
		s.RefreshTemperatures(ctx)
		ticker := time.NewTicker(s.refreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.RefreshTemperatures(ctx)
			}
		}
	}()
}

func (s *DiskService) Disks(ctx context.Context) (DiskSnapshot, error) {
	result := DiskSnapshot{
		Timestamp: time.Now().UTC(),
		Errors:    map[string]string{},
	}

	physical, physicalErrors := s.physical(ctx)
	for i := range physical {
		physical[i].TemperatureCelsius = nil
		physical[i].TemperatureError = ""
		physical[i].TemperatureUpdatedAt = nil
	}
	result.PhysicalDisks = s.withCachedTemperatures(physical)
	mergeErrors(result.Errors, physicalErrors)

	temps, _ := s.DiskTemperatures(ctx)
	result.TemperatureUpdatedAt = temps.UpdatedAt
	result.NextRefreshAt = temps.NextRefreshAt
	mergeErrors(result.Errors, temps.Errors)

	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

func (s *DiskService) DiskTemperatures(ctx context.Context) (DiskTemperatureSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneTemperatureSnapshot(s.temps), nil
}

func (s *DiskService) RefreshTemperatures(ctx context.Context) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	physical, errorsByGroup := s.physical(ctx)
	now := time.Now().UTC()
	items := make([]DiskTemperatureStats, 0, len(physical))
	for _, item := range physical {
		tempError := item.TemperatureError
		if item.TemperatureCelsius == nil && tempError == "" {
			tempError = "temperature unavailable"
		}
		items = append(items, DiskTemperatureStats{
			DeviceID:           item.DeviceID,
			FriendlyName:       item.FriendlyName,
			SerialNumber:       item.SerialNumber,
			TemperatureCelsius: item.TemperatureCelsius,
			TemperatureError:   tempError,
			UpdatedAt:          &now,
		})
	}

	next := now.Add(s.refreshInterval)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastRefresh = now
	s.nextRefresh = next
	s.temps = DiskTemperatureSnapshot{
		UpdatedAt:     &now,
		NextRefreshAt: &next,
		Items:         items,
		Errors:        errorsByGroup,
	}
	if len(s.temps.Errors) == 0 {
		s.temps.Errors = nil
	}
}

func (s *DiskService) withCachedTemperatures(disks []PhysicalDiskStats) []PhysicalDiskStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(disks) == 0 || len(s.temps.Items) == 0 {
		return disks
	}
	byKey := map[string]DiskTemperatureStats{}
	for _, item := range s.temps.Items {
		for _, key := range diskKeys(item.DeviceID, item.SerialNumber, item.FriendlyName) {
			byKey[key] = item
		}
	}
	for i := range disks {
		for _, key := range diskKeys(disks[i].DeviceID, disks[i].SerialNumber, disks[i].FriendlyName) {
			if cached, ok := byKey[key]; ok {
				disks[i].TemperatureCelsius = cached.TemperatureCelsius
				disks[i].TemperatureError = cached.TemperatureError
				disks[i].TemperatureUpdatedAt = cached.UpdatedAt
				break
			}
		}
	}
	return disks
}

func collectPhysicalDisks(ctx context.Context) ([]PhysicalDiskStats, map[string]string) {
	if runtime.GOOS != "windows" {
		return nil, map[string]string{"physicalDisks": "physical disk temperature collection is only supported on Windows"}
	}
	cmd := hiddenCommandContext(ctx, "powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
		"Get-PhysicalDisk | Select-Object DeviceId,FriendlyName,SerialNumber,MediaType,BusType,HealthStatus,OperationalStatus,Size,Temperature | ConvertTo-Json -Depth 3 -Compress")
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return nil, map[string]string{"physicalDisks": strings.TrimSpace(string(exitErr.Stderr))}
		}
		return nil, map[string]string{"physicalDisks": err.Error()}
	}
	disks, err := parsePhysicalDiskJSON(output)
	if err != nil {
		return nil, map[string]string{"physicalDisks": err.Error()}
	}
	return disks, nil
}

func parsePhysicalDiskJSON(input []byte) ([]PhysicalDiskStats, error) {
	input = bytes.TrimSpace(input)
	if len(input) == 0 {
		return nil, nil
	}
	var rows []physicalDiskRow
	if input[0] == '[' {
		if err := json.Unmarshal(input, &rows); err != nil {
			return nil, err
		}
	} else {
		var row physicalDiskRow
		if err := json.Unmarshal(input, &row); err != nil {
			return nil, err
		}
		rows = []physicalDiskRow{row}
	}
	disks := make([]PhysicalDiskStats, 0, len(rows))
	for _, row := range rows {
		disks = append(disks, row.toStats())
	}
	return disks, nil
}

type physicalDiskRow struct {
	DeviceID          any `json:"DeviceId"`
	FriendlyName      any `json:"FriendlyName"`
	SerialNumber      any `json:"SerialNumber"`
	MediaType         any `json:"MediaType"`
	BusType           any `json:"BusType"`
	HealthStatus      any `json:"HealthStatus"`
	OperationalStatus any `json:"OperationalStatus"`
	Size              any `json:"Size"`
	Temperature       any `json:"Temperature"`
}

func (r physicalDiskRow) toStats() PhysicalDiskStats {
	temp := intPointerFromAny(r.Temperature)
	tempError := ""
	if temp == nil {
		tempError = "temperature unavailable"
	}
	return PhysicalDiskStats{
		DeviceID:           stringFromAny(r.DeviceID),
		FriendlyName:       stringFromAny(r.FriendlyName),
		SerialNumber:       stringFromAny(r.SerialNumber),
		MediaType:          stringFromAny(r.MediaType),
		BusType:            stringFromAny(r.BusType),
		HealthStatus:       stringFromAny(r.HealthStatus),
		OperationalStatus:  stringFromAny(r.OperationalStatus),
		SizeBytes:          uint64FromAny(r.Size),
		TemperatureCelsius: temp,
		TemperatureError:   tempError,
	}
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func uint64FromAny(value any) uint64 {
	switch typed := value.(type) {
	case float64:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case string:
		parsed, _ := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		return parsed
	case json.Number:
		parsed, _ := strconv.ParseUint(typed.String(), 10, 64)
		return parsed
	default:
		return 0
	}
}

func intPointerFromAny(value any) *int {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		parsed := int(typed)
		return &parsed
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return nil
		}
		return &parsed
	case json.Number:
		parsed, err := strconv.Atoi(typed.String())
		if err != nil {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}

func mergeErrors(target map[string]string, source map[string]string) {
	for key, value := range source {
		target[key] = value
	}
}

func cloneTemperatureSnapshot(snapshot DiskTemperatureSnapshot) DiskTemperatureSnapshot {
	result := snapshot
	result.Items = append([]DiskTemperatureStats(nil), snapshot.Items...)
	if snapshot.Errors != nil {
		result.Errors = map[string]string{}
		mergeErrors(result.Errors, snapshot.Errors)
	}
	return result
}

func diskKeys(deviceID string, serialNumber string, friendlyName string) []string {
	keys := make([]string, 0, 3)
	for _, value := range []string{deviceID, serialNumber, friendlyName} {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			keys = append(keys, value)
		}
	}
	return keys
}

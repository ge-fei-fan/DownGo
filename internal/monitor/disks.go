package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/downgo/internal/domain"
)

type DiskProvider interface {
	Disks(ctx context.Context) (DiskSnapshot, error)
	DiskSMART(ctx context.Context) (DiskSMARTSnapshot, error)
	DiskSMARTBySerial(ctx context.Context, serialNumber string) (DiskSMARTStats, bool, error)
	DiskTemperatures(ctx context.Context) (DiskTemperatureSnapshot, error)
	RefreshDiskTemperatures(ctx context.Context) (DiskTemperatureSnapshot, error)
	DiskTemperatureHistory(ctx context.Context, from time.Time, to time.Time, limit int) (DiskTemperatureHistorySnapshot, error)
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
	MediaType          string     `json:"mediaType"`
	TemperatureCelsius *int       `json:"temperatureCelsius"`
	TemperatureError   string     `json:"temperatureError,omitempty"`
	UpdatedAt          *time.Time `json:"updatedAt,omitempty"`
}

type DiskTemperatureHistorySnapshot struct {
	From   time.Time                         `json:"from"`
	To     time.Time                         `json:"to"`
	Items  []DiskTemperatureHistoryDiskStats `json:"items"`
	Errors map[string]string                 `json:"errors,omitempty"`
}

type DiskTemperatureHistoryDiskStats struct {
	DeviceID     string                        `json:"deviceId"`
	FriendlyName string                        `json:"friendlyName"`
	SerialNumber string                        `json:"serialNumber"`
	MediaType    string                        `json:"mediaType"`
	Points       []DiskTemperatureHistoryPoint `json:"points"`
}

type DiskTemperatureHistoryPoint struct {
	SampledAt          time.Time `json:"sampledAt"`
	TemperatureCelsius *int      `json:"temperatureCelsius"`
	TemperatureError   string    `json:"temperatureError,omitempty"`
}

type DiskSMARTSnapshot struct {
	UpdatedAt     *time.Time        `json:"updatedAt,omitempty"`
	NextRefreshAt *time.Time        `json:"nextRefreshAt,omitempty"`
	Items         []DiskSMARTStats  `json:"items"`
	Errors        map[string]string `json:"errors,omitempty"`
}

type DiskSMARTStats struct {
	DeviceID           string           `json:"deviceId"`
	FriendlyName       string           `json:"friendlyName"`
	SerialNumber       string           `json:"serialNumber"`
	FirmwareVersion    string           `json:"firmwareVersion,omitempty"`
	MediaType          string           `json:"mediaType"`
	BusType            string           `json:"busType"`
	HealthStatus       string           `json:"healthStatus,omitempty"`
	SizeBytes          uint64           `json:"sizeBytes"`
	TemperatureCelsius *int             `json:"temperatureCelsius"`
	PowerOnHours       *uint64          `json:"powerOnHours,omitempty"`
	PowerCycleCount    *uint64          `json:"powerCycleCount,omitempty"`
	Attributes         []SMARTAttribute `json:"attributes"`
}

type SMARTAttribute struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Value     *int   `json:"value,omitempty"`
	Worst     *int   `json:"worst,omitempty"`
	Threshold *int   `json:"threshold,omitempty"`
	RawValue  string `json:"rawValue,omitempty"`
	RawString string `json:"rawString,omitempty"`
}

type DiskTemperatureSample = domain.DiskTemperatureSample

type DiskService struct {
	refreshInterval time.Duration
	retention       time.Duration
	physical        physicalDiskFunc
	smartctlPath    string
	history         DiskTemperatureHistoryStore

	mu          sync.RWMutex
	temps       DiskTemperatureSnapshot
	smart       DiskSMARTSnapshot
	lastRefresh time.Time
	nextRefresh time.Time
}

type physicalDiskFunc func(context.Context) ([]PhysicalDiskStats, map[string]string)

type DiskTemperatureHistoryStore interface {
	InsertDiskTemperatureSamples(context.Context, []DiskTemperatureSample) error
	ListDiskTemperatureSamples(context.Context, time.Time, time.Time, int) ([]DiskTemperatureSample, error)
	DeleteDiskTemperatureSamplesBefore(context.Context, time.Time) error
}

func NewDiskService(refreshInterval time.Duration) *DiskService {
	if refreshInterval <= 0 {
		refreshInterval = 30 * time.Minute
	}
	return &DiskService{
		refreshInterval: refreshInterval,
		retention:       30 * 24 * time.Hour,
		physical:        collectPhysicalDisks,
		smartctlPath:    "smartctl.exe",
		temps:           DiskTemperatureSnapshot{Items: []DiskTemperatureStats{}},
		smart:           DiskSMARTSnapshot{Items: []DiskSMARTStats{}},
	}
}

func (s *DiskService) SetSmartctlPath(path string) {
	if strings.TrimSpace(path) != "" {
		s.smartctlPath = path
	}
}

func (s *DiskService) SetTemperatureHistoryStore(store DiskTemperatureHistoryStore) {
	s.history = store
}

func (s *DiskService) Start(ctx context.Context) {
	go func() {
		_, _ = s.RefreshDiskTemperatures(ctx)
		ticker := time.NewTicker(s.refreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.RefreshDiskTemperatures(ctx)
			}
		}
	}()
}

func (s *DiskService) Disks(ctx context.Context) (DiskSnapshot, error) {
	result := DiskSnapshot{
		Timestamp: time.Now().UTC(),
		Errors:    map[string]string{},
	}

	physical, physicalErrors := s.collectHDDDisks(ctx)
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

func (s *DiskService) DiskSMART(ctx context.Context) (DiskSMARTSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSMARTSnapshot(s.smart), nil
}

func (s *DiskService) DiskSMARTBySerial(ctx context.Context, serialNumber string) (DiskSMARTStats, bool, error) {
	serial := normalizeDiskID(serialNumber)
	if serial == "" {
		return DiskSMARTStats{}, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.smart.Items {
		if normalizeDiskID(item.SerialNumber) == serial {
			return cloneSMARTStats(item), true, nil
		}
	}
	return DiskSMARTStats{}, false, nil
}

func (s *DiskService) DiskTemperatureHistory(ctx context.Context, from time.Time, to time.Time, limit int) (DiskTemperatureHistorySnapshot, error) {
	result := DiskTemperatureHistorySnapshot{
		From:   from.UTC(),
		To:     to.UTC(),
		Items:  []DiskTemperatureHistoryDiskStats{},
		Errors: map[string]string{},
	}
	if s.history == nil {
		result.Errors["history"] = "disk temperature history store is unavailable"
		return result, nil
	}
	samples, err := s.history.ListDiskTemperatureSamples(ctx, from, to, limit)
	if err != nil {
		return result, err
	}
	result.Items = groupDiskTemperatureSamples(samples)
	if len(result.Errors) == 0 {
		result.Errors = nil
	}
	return result, nil
}

func (s *DiskService) RefreshTemperatures(ctx context.Context) {
	_, _ = s.RefreshDiskTemperatures(ctx)
}

func (s *DiskService) RefreshDiskTemperatures(ctx context.Context) (DiskTemperatureSnapshot, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	physical, smartItems, errorsByGroup := s.collectHDDDiskData(ctx)
	smartErrors := cloneErrors(errorsByGroup)
	now := time.Now().UTC()
	items := make([]DiskTemperatureStats, 0, len(physical))
	for _, item := range physical {
		tempError := item.TemperatureError
		if item.TemperatureCelsius == nil && tempError == "" {
			tempError = "temperature unavailable from Windows Get-PhysicalDisk"
		}
		items = append(items, DiskTemperatureStats{
			DeviceID:           item.DeviceID,
			FriendlyName:       item.FriendlyName,
			SerialNumber:       item.SerialNumber,
			MediaType:          item.MediaType,
			TemperatureCelsius: item.TemperatureCelsius,
			TemperatureError:   tempError,
			UpdatedAt:          &now,
		})
	}

	next := now.Add(s.refreshInterval)
	s.mu.Lock()
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
	s.smart = DiskSMARTSnapshot{
		UpdatedAt:     &now,
		NextRefreshAt: &next,
		Items:         smartItems,
		Errors:        smartErrors,
	}
	if len(s.smart.Errors) == 0 {
		s.smart.Errors = nil
	}
	snapshot := cloneTemperatureSnapshot(s.temps)
	s.mu.Unlock()

	s.saveTemperatureHistory(ctx, items, now)
	return snapshot, nil
}

func (s *DiskService) collectHDDDisks(ctx context.Context) ([]PhysicalDiskStats, map[string]string) {
	physical, _, errorsByGroup := s.collectHDDDiskData(ctx)
	return physical, errorsByGroup
}

func (s *DiskService) collectHDDDiskData(ctx context.Context) ([]PhysicalDiskStats, []DiskSMARTStats, map[string]string) {
	physical, errorsByGroup := s.physical(ctx)
	if errorsByGroup == nil {
		errorsByGroup = map[string]string{}
	}
	physical = filterHDDDisks(physical)

	smartDisks, smartItems, smartErrors := collectSmartctlDiskData(ctx, s.smartctlPath)
	mergeErrors(errorsByGroup, smartErrors)
	if len(physical) == 0 {
		return filterLikelyHDDDisks(smartDisks), filterLikelyHDDSMART(smartItems), errorsByGroup
	}
	mergeSmartctlTemperatures(physical, smartDisks)
	return physical, matchSMARTToPhysicalHDDs(physical, smartItems), errorsByGroup
}

func (s *DiskService) collectHDDDiskSMART(ctx context.Context) ([]DiskSMARTStats, map[string]string) {
	physical, errorsByGroup := s.physical(ctx)
	if errorsByGroup == nil {
		errorsByGroup = map[string]string{}
	}
	physical = filterHDDDisks(physical)

	smartItems, smartErrors := collectSmartctlSMART(ctx, s.smartctlPath)
	mergeErrors(errorsByGroup, smartErrors)
	if len(physical) == 0 {
		return filterLikelyHDDSMART(smartItems), errorsByGroup
	}
	return matchSMARTToPhysicalHDDs(physical, smartItems), errorsByGroup
}

func (s *DiskService) saveTemperatureHistory(ctx context.Context, items []DiskTemperatureStats, sampledAt time.Time) {
	if s.history == nil {
		return
	}
	samples := make([]DiskTemperatureSample, 0, len(items))
	for _, item := range items {
		samples = append(samples, DiskTemperatureSample{
			DeviceID:           item.DeviceID,
			FriendlyName:       item.FriendlyName,
			SerialNumber:       item.SerialNumber,
			MediaType:          item.MediaType,
			TemperatureCelsius: item.TemperatureCelsius,
			TemperatureError:   item.TemperatureError,
			SampledAt:          sampledAt,
		})
	}
	if err := s.history.InsertDiskTemperatureSamples(ctx, samples); err != nil {
		return
	}
	if s.retention > 0 {
		_ = s.history.DeleteDiskTemperatureSamplesBefore(ctx, sampledAt.Add(-s.retention))
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

type smartctlScanDevice struct {
	Name string
	Type string
}

type smartctlScanJSON struct {
	Devices []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"devices"`
}

type smartctlJSON struct {
	Smartctl struct {
		Version    []int `json:"version"`
		ExitStatus int   `json:"exit_status"`
	} `json:"smartctl"`
	Device struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Protocol string `json:"protocol"`
	} `json:"device"`
	ModelName        string `json:"model_name"`
	SerialNumber     string `json:"serial_number"`
	FirmwareVersion  string `json:"firmware_version"`
	RotationRate     any    `json:"rotation_rate"`
	SolidStateDevice any    `json:"solid_state_device"`
	UserCapacity     struct {
		Bytes any `json:"bytes"`
	} `json:"user_capacity"`
	SmartStatus struct {
		Passed any `json:"passed"`
	} `json:"smart_status"`
	PowerOnTime struct {
		Hours any `json:"hours"`
	} `json:"power_on_time"`
	PowerCycleCount any `json:"power_cycle_count"`
	Temperature     struct {
		Current any `json:"current"`
	} `json:"temperature"`
	NVMeSmartHealthInformationLog struct {
		Temperature any `json:"temperature"`
	} `json:"nvme_smart_health_information_log"`
	ATASmartAttributes struct {
		Table []struct {
			ID     int    `json:"id"`
			Name   string `json:"name"`
			Value  any    `json:"value"`
			Worst  any    `json:"worst"`
			Thresh any    `json:"thresh"`
			Raw    struct {
				Value  any    `json:"value"`
				String string `json:"string"`
			} `json:"raw"`
		} `json:"table"`
	} `json:"ata_smart_attributes"`
}

func collectSmartctlDisks(ctx context.Context, smartctlPath string) ([]PhysicalDiskStats, map[string]string) {
	disks, _, errorsByGroup := collectSmartctlDiskData(ctx, smartctlPath)
	return disks, errorsByGroup
}

func collectSmartctlSMART(ctx context.Context, smartctlPath string) ([]DiskSMARTStats, map[string]string) {
	_, items, errorsByGroup := collectSmartctlDiskData(ctx, smartctlPath)
	return items, errorsByGroup
}

func collectSmartctlDiskData(ctx context.Context, smartctlPath string) ([]PhysicalDiskStats, []DiskSMARTStats, map[string]string) {
	errorsByGroup := map[string]string{}
	if strings.TrimSpace(smartctlPath) == "" {
		return nil, nil, errorsByGroup
	}
	if strings.ContainsAny(smartctlPath, `\/`) {
		if _, err := os.Stat(smartctlPath); err != nil {
			return nil, nil, map[string]string{"smartctl": err.Error()}
		}
	}

	scanOutput, err := hiddenCommandContext(ctx, smartctlPath, "--scan-open").CombinedOutput()
	if err != nil {
		errorsByGroup["smartctl"] = commandError("smartctl --scan-open", scanOutput, err)
		return nil, nil, errorsByGroup
	}
	devices := parseSmartctlScan(scanOutput)
	if len(devices) == 0 {
		errorsByGroup["smartctl"] = "smartctl found no devices"
		return nil, nil, errorsByGroup
	}

	disks := make([]PhysicalDiskStats, 0, len(devices))
	smartItems := make([]DiskSMARTStats, 0, len(devices))
	for _, device := range devices {
		args := []string{"-A", "-i", "-j"}
		if device.Type != "" {
			args = append(args, "-d", device.Type)
		}
		args = append(args, device.Name)
		output, err := hiddenCommandContext(ctx, smartctlPath, args...).CombinedOutput()
		disk, parseErr := parseSmartctlDiskJSON(output)
		if parseErr != nil {
			if err != nil {
				errorsByGroup["smartctl:"+device.Name] = commandError("smartctl "+device.Name, output, err)
			} else {
				errorsByGroup["smartctl:"+device.Name] = parseErr.Error()
			}
			continue
		}
		if err != nil && disk.TemperatureCelsius == nil {
			errorsByGroup["smartctl:"+device.Name] = smartctlExitError(output, err)
		}
		disks = append(disks, disk)
		item, parseErr := parseSmartctlSMARTJSON(output)
		if parseErr != nil {
			if err != nil {
				errorsByGroup["smartctl:"+device.Name] = commandError("smartctl "+device.Name, output, err)
			} else {
				errorsByGroup["smartctl:"+device.Name] = parseErr.Error()
			}
			continue
		}
		if err != nil && len(item.Attributes) == 0 && item.HealthStatus == "" {
			errorsByGroup["smartctl:"+device.Name] = commandError("smartctl "+device.Name, output, err)
		}
		smartItems = append(smartItems, item)
	}
	return disks, smartItems, errorsByGroup
}

func parseSmartctlScan(input []byte) []smartctlScanDevice {
	input = bytes.TrimSpace(input)
	if len(input) == 0 {
		return nil
	}
	var parsed smartctlScanJSON
	if input[0] == '{' && json.Unmarshal(input, &parsed) == nil {
		devices := make([]smartctlScanDevice, 0, len(parsed.Devices))
		for _, device := range parsed.Devices {
			if device.Name != "" {
				devices = append(devices, smartctlScanDevice{Name: device.Name, Type: device.Type})
			}
		}
		return devices
	}

	lines := strings.Split(string(input), "\n")
	devices := make([]smartctlScanDevice, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if before, _, ok := strings.Cut(line, "#"); ok {
			line = strings.TrimSpace(before)
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		device := smartctlScanDevice{Name: fields[0]}
		for i := 1; i+1 < len(fields); i++ {
			if fields[i] == "-d" {
				device.Type = fields[i+1]
				break
			}
		}
		devices = append(devices, device)
	}
	return devices
}

func parseSmartctlDiskJSON(input []byte) (PhysicalDiskStats, error) {
	var parsed smartctlJSON
	if err := json.Unmarshal(input, &parsed); err != nil {
		return PhysicalDiskStats{}, err
	}
	temp := smartctlTemperature(parsed)
	tempError := ""
	if temp == nil {
		tempError = "temperature unavailable from smartctl"
	}
	mediaType := smartctlMediaType(parsed)
	deviceID := parsed.Device.Name
	if deviceID == "" {
		deviceID = parsed.SerialNumber
	}
	return PhysicalDiskStats{
		DeviceID:           deviceID,
		FriendlyName:       parsed.ModelName,
		SerialNumber:       parsed.SerialNumber,
		MediaType:          mediaType,
		BusType:            parsed.Device.Protocol,
		SizeBytes:          uint64FromAny(parsed.UserCapacity.Bytes),
		TemperatureCelsius: temp,
		TemperatureError:   tempError,
	}, nil
}

func parseSmartctlSMARTJSON(input []byte) (DiskSMARTStats, error) {
	var parsed smartctlJSON
	if err := json.Unmarshal(input, &parsed); err != nil {
		return DiskSMARTStats{}, err
	}
	healthStatus := ""
	if passed := boolPointerFromAny(parsed.SmartStatus.Passed); passed != nil {
		if *passed {
			healthStatus = "PASSED"
		} else {
			healthStatus = "FAILED"
		}
	}
	deviceID := parsed.Device.Name
	if deviceID == "" {
		deviceID = parsed.SerialNumber
	}
	return DiskSMARTStats{
		DeviceID:           deviceID,
		FriendlyName:       parsed.ModelName,
		SerialNumber:       parsed.SerialNumber,
		FirmwareVersion:    parsed.FirmwareVersion,
		MediaType:          smartctlMediaType(parsed),
		BusType:            parsed.Device.Protocol,
		HealthStatus:       healthStatus,
		SizeBytes:          uint64FromAny(parsed.UserCapacity.Bytes),
		TemperatureCelsius: smartctlTemperature(parsed),
		PowerOnHours:       uint64PointerFromAny(parsed.PowerOnTime.Hours),
		PowerCycleCount:    uint64PointerFromAny(parsed.PowerCycleCount),
		Attributes:         smartctlAttributes(parsed),
	}, nil
}

func smartctlAttributes(parsed smartctlJSON) []SMARTAttribute {
	items := make([]SMARTAttribute, 0, len(parsed.ATASmartAttributes.Table))
	for _, attr := range parsed.ATASmartAttributes.Table {
		items = append(items, SMARTAttribute{
			ID:        attr.ID,
			Name:      attr.Name,
			Value:     intPointerFromAny(attr.Value),
			Worst:     intPointerFromAny(attr.Worst),
			Threshold: intPointerFromAny(attr.Thresh),
			RawValue:  stringFromAny(attr.Raw.Value),
			RawString: attr.Raw.String,
		})
	}
	return items
}

func smartctlMediaType(parsed smartctlJSON) string {
	if boolFromAny(parsed.SolidStateDevice) {
		return "SSD"
	}
	if rate := intPointerFromAny(parsed.RotationRate); rate != nil {
		if *rate > 0 {
			return "HDD"
		}
		return "SSD"
	}
	if strings.EqualFold(parsed.Device.Protocol, "NVMe") {
		return "SSD"
	}
	return ""
}

func isLikelyHDDSMART(item DiskSMARTStats) bool {
	if isHDDMediaType(item.MediaType) {
		return true
	}
	if strings.EqualFold(item.MediaType, "SSD") || strings.EqualFold(item.BusType, "NVMe") {
		return false
	}
	return strings.EqualFold(item.BusType, "ATA") || strings.EqualFold(item.BusType, "SATA")
}

func smartctlTemperature(parsed smartctlJSON) *int {
	if temp := intPointerFromAny(parsed.Temperature.Current); temp != nil {
		return temp
	}
	if temp := intPointerFromAny(parsed.NVMeSmartHealthInformationLog.Temperature); temp != nil {
		return temp
	}
	for _, attr := range parsed.ATASmartAttributes.Table {
		if attr.ID != 190 && attr.ID != 194 {
			continue
		}
		if temp := firstIntPointer(attr.Raw.String); temp != nil {
			return temp
		}
		if temp := intPointerFromAny(attr.Raw.Value); temp != nil {
			return temp
		}
	}
	return nil
}

func firstIntPointer(value string) *int {
	for _, field := range strings.FieldsFunc(value, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		if field == "" {
			continue
		}
		parsed, err := strconv.Atoi(field)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func mergeSmartctlTemperatures(disks []PhysicalDiskStats, smartDisks []PhysicalDiskStats) {
	used := map[int]bool{}
	for i := range disks {
		match := findSmartctlMatch(disks[i], smartDisks, used)
		if match == nil {
			if disks[i].TemperatureCelsius == nil && disks[i].TemperatureError == "" {
				disks[i].TemperatureError = "temperature unavailable from smartctl and Windows Get-PhysicalDisk"
			}
			continue
		}
		if match.TemperatureCelsius != nil {
			disks[i].TemperatureCelsius = match.TemperatureCelsius
			disks[i].TemperatureError = ""
			continue
		}
		if disks[i].TemperatureCelsius == nil {
			disks[i].TemperatureError = match.TemperatureError
		}
	}
}

func findSmartctlMatch(disk PhysicalDiskStats, smartDisks []PhysicalDiskStats, used map[int]bool) *PhysicalDiskStats {
	serial := normalizeDiskID(disk.SerialNumber)
	if serial != "" {
		for i := range smartDisks {
			if used[i] {
				continue
			}
			if normalizeDiskID(smartDisks[i].SerialNumber) == serial {
				used[i] = true
				return &smartDisks[i]
			}
		}
	}
	name := normalizeDiskID(disk.FriendlyName)
	if name != "" {
		for i := range smartDisks {
			if used[i] {
				continue
			}
			smartName := normalizeDiskID(smartDisks[i].FriendlyName)
			if smartName != "" && (strings.Contains(smartName, name) || strings.Contains(name, smartName)) {
				used[i] = true
				return &smartDisks[i]
			}
		}
	}
	for i := range smartDisks {
		if !used[i] {
			used[i] = true
			return &smartDisks[i]
		}
	}
	return nil
}

func matchSMARTToPhysicalHDDs(disks []PhysicalDiskStats, smartItems []DiskSMARTStats) []DiskSMARTStats {
	items := make([]DiskSMARTStats, 0, len(disks))
	used := map[int]bool{}
	for _, disk := range disks {
		match := findSMARTMatch(disk, smartItems, used)
		if match == nil {
			continue
		}
		if match.DeviceID == "" {
			match.DeviceID = disk.DeviceID
		}
		if match.FriendlyName == "" {
			match.FriendlyName = disk.FriendlyName
		}
		if match.SerialNumber == "" {
			match.SerialNumber = disk.SerialNumber
		}
		if match.MediaType == "" {
			match.MediaType = disk.MediaType
		}
		if match.BusType == "" {
			match.BusType = disk.BusType
		}
		if match.SizeBytes == 0 {
			match.SizeBytes = disk.SizeBytes
		}
		items = append(items, *match)
	}
	return items
}

func findSMARTMatch(disk PhysicalDiskStats, smartItems []DiskSMARTStats, used map[int]bool) *DiskSMARTStats {
	serial := normalizeDiskID(disk.SerialNumber)
	if serial != "" {
		for i := range smartItems {
			if used[i] {
				continue
			}
			if normalizeDiskID(smartItems[i].SerialNumber) == serial {
				used[i] = true
				return &smartItems[i]
			}
		}
	}
	name := normalizeDiskID(disk.FriendlyName)
	if name != "" {
		for i := range smartItems {
			if used[i] {
				continue
			}
			smartName := normalizeDiskID(smartItems[i].FriendlyName)
			if smartName != "" && (strings.Contains(smartName, name) || strings.Contains(name, smartName)) {
				used[i] = true
				return &smartItems[i]
			}
		}
	}
	for i := range smartItems {
		if !used[i] && isLikelyHDDSMART(smartItems[i]) {
			used[i] = true
			return &smartItems[i]
		}
	}
	return nil
}

func filterLikelyHDDSMART(smartItems []DiskSMARTStats) []DiskSMARTStats {
	items := make([]DiskSMARTStats, 0, len(smartItems))
	for _, item := range smartItems {
		if isLikelyHDDSMART(item) {
			if item.MediaType == "" {
				item.MediaType = "HDD"
			}
			items = append(items, item)
		}
	}
	return items
}

func normalizeDiskID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "", "-", "", "_", "", ".", "")
	return replacer.Replace(value)
}

func commandError(command string, output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if text != "" {
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		return command + ": " + text
	}
	return command + ": " + err.Error()
}

func smartctlExitError(output []byte, err error) string {
	var parsed smartctlJSON
	if json.Unmarshal(output, &parsed) == nil && len(parsed.Smartctl.Version) > 0 {
		return "smartctl exited with status but did not report temperature"
	}
	return err.Error()
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
		tempError = "temperature unavailable from Windows Get-PhysicalDisk"
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

func uint64PointerFromAny(value any) *uint64 {
	parsed := uint64FromAny(value)
	if parsed == 0 {
		switch typed := value.(type) {
		case float64:
			if typed == 0 {
				return &parsed
			}
		case string:
			if strings.TrimSpace(typed) == "0" {
				return &parsed
			}
		case json.Number:
			if typed.String() == "0" {
				return &parsed
			}
		}
		return nil
	}
	return &parsed
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

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return false
	}
}

func boolPointerFromAny(value any) *bool {
	switch typed := value.(type) {
	case nil:
		return nil
	case bool:
		return &typed
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
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

func cloneErrors(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := map[string]string{}
	mergeErrors(result, source)
	return result
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

func cloneSMARTSnapshot(snapshot DiskSMARTSnapshot) DiskSMARTSnapshot {
	result := snapshot
	result.Items = append([]DiskSMARTStats(nil), snapshot.Items...)
	for i := range result.Items {
		result.Items[i] = cloneSMARTStats(snapshot.Items[i])
	}
	if snapshot.Errors != nil {
		result.Errors = map[string]string{}
		mergeErrors(result.Errors, snapshot.Errors)
	}
	return result
}

func cloneSMARTStats(item DiskSMARTStats) DiskSMARTStats {
	result := item
	result.Attributes = append([]SMARTAttribute(nil), item.Attributes...)
	return result
}

func groupDiskTemperatureSamples(samples []DiskTemperatureSample) []DiskTemperatureHistoryDiskStats {
	items := make([]DiskTemperatureHistoryDiskStats, 0)
	indexes := map[string]int{}
	for _, sample := range samples {
		key := strings.Join(diskKeys(sample.DeviceID, sample.SerialNumber, sample.FriendlyName), "\x00")
		if key == "" {
			key = sample.DeviceID + "\x00" + sample.SerialNumber + "\x00" + sample.FriendlyName
		}
		index, ok := indexes[key]
		if !ok {
			index = len(items)
			indexes[key] = index
			items = append(items, DiskTemperatureHistoryDiskStats{
				DeviceID:     sample.DeviceID,
				FriendlyName: sample.FriendlyName,
				SerialNumber: sample.SerialNumber,
				MediaType:    sample.MediaType,
				Points:       []DiskTemperatureHistoryPoint{},
			})
		}
		items[index].Points = append(items[index].Points, DiskTemperatureHistoryPoint{
			SampledAt:          sample.SampledAt,
			TemperatureCelsius: sample.TemperatureCelsius,
			TemperatureError:   sample.TemperatureError,
		})
	}
	return items
}

func filterHDDDisks(disks []PhysicalDiskStats) []PhysicalDiskStats {
	items := make([]PhysicalDiskStats, 0, len(disks))
	for _, disk := range disks {
		if isHDDMediaType(disk.MediaType) {
			items = append(items, disk)
		}
	}
	return items
}

func filterLikelyHDDDisks(disks []PhysicalDiskStats) []PhysicalDiskStats {
	items := make([]PhysicalDiskStats, 0, len(disks))
	for _, disk := range disks {
		if isHDDMediaType(disk.MediaType) || disk.MediaType == "" && !strings.EqualFold(disk.BusType, "NVMe") {
			if disk.MediaType == "" {
				disk.MediaType = "HDD"
			}
			items = append(items, disk)
		}
	}
	return items
}

func isHDDMediaType(mediaType string) bool {
	return strings.EqualFold(strings.TrimSpace(mediaType), "HDD")
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

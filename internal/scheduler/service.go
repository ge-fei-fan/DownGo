package scheduler

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"example.com/downgo/internal/domain"
)

const (
	TaskDiskTemperatureRefresh        = "disk-temperature-refresh"
	TaskBilibiliFavoritesSubscription = "bilibili-favorites-subscription"
)

type Store interface {
	EnsureScheduledTask(context.Context, domain.ScheduledTask) error
	ListScheduledTasks(context.Context) ([]domain.ScheduledTask, error)
	GetScheduledTask(context.Context, string) (domain.ScheduledTask, error)
	UpdateScheduledTaskConfig(context.Context, string, domain.ScheduledTaskUpdate) (domain.ScheduledTask, error)
	UpdateScheduledTaskRun(context.Context, string, time.Time, *time.Time, string) error
}

type TaskDefinition struct {
	ID                     string
	Name                   string
	Description            string
	DefaultEnabled         bool
	DefaultIntervalMinutes int
	Run                    func(context.Context, domain.ScheduledTask) error
}

type Service struct {
	store Store
	defs  map[string]TaskDefinition

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once

	mu     sync.Mutex
	update chan struct{}
}

func NewService(ctx context.Context, store Store, definitions []TaskDefinition) (*Service, error) {
	if store == nil {
		return nil, errors.New("scheduled task store is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	serviceCtx, cancel := context.WithCancel(ctx)
	s := &Service{
		store:  store,
		defs:   map[string]TaskDefinition{},
		ctx:    serviceCtx,
		cancel: cancel,
		update: make(chan struct{}),
	}
	for _, def := range definitions {
		if def.ID == "" || def.Run == nil {
			cancel()
			return nil, errors.New("scheduled task definition is invalid")
		}
		if def.DefaultIntervalMinutes <= 0 {
			def.DefaultIntervalMinutes = 1
		}
		s.defs[def.ID] = def
		if err := store.EnsureScheduledTask(ctx, domain.ScheduledTask{
			ID:                     def.ID,
			Enabled:                def.DefaultEnabled,
			IntervalMinutes:        def.DefaultIntervalMinutes,
			DefaultIntervalMinutes: def.DefaultIntervalMinutes,
		}); err != nil {
			cancel()
			return nil, err
		}
	}
	return s, nil
}

func (s *Service) Start() {
	s.once.Do(func() {
		for _, def := range s.defs {
			go s.runLoop(def)
		}
	})
}

func (s *Service) Shutdown() {
	s.cancel()
}

func (s *Service) List(ctx context.Context) ([]domain.ScheduledTask, error) {
	items, err := s.store.ListScheduledTasks(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]domain.ScheduledTask, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	result := make([]domain.ScheduledTask, 0, len(s.defs))
	for _, def := range s.defs {
		item := byID[def.ID]
		item.ID = def.ID
		item.Name = def.Name
		item.Description = def.Description
		item.DefaultIntervalMinutes = def.DefaultIntervalMinutes
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result, nil
}

func (s *Service) Update(ctx context.Context, id string, input domain.ScheduledTaskUpdate) (domain.ScheduledTask, error) {
	if _, ok := s.defs[id]; !ok {
		return domain.ScheduledTask{}, errors.New("定时任务不存在")
	}
	if input.IntervalMinutes < 1 || input.IntervalMinutes > 1440 {
		return domain.ScheduledTask{}, errors.New("定时间隔必须在 1 到 1440 分钟之间")
	}
	task, err := s.store.UpdateScheduledTaskConfig(ctx, id, input)
	if err != nil {
		return domain.ScheduledTask{}, err
	}
	s.notifyUpdated()
	list, err := s.List(ctx)
	if err != nil {
		return task, nil
	}
	for _, item := range list {
		if item.ID == id {
			return item, nil
		}
	}
	return task, nil
}

func (s *Service) runLoop(def TaskDefinition) {
	for {
		task, err := s.store.GetScheduledTask(s.ctx, def.ID)
		if err != nil {
			if !s.wait(30 * time.Second) {
				return
			}
			continue
		}
		if !task.Enabled {
			if !s.waitForUpdate() {
				return
			}
			continue
		}

		now := time.Now().UTC()
		if task.NextRunAt != nil && task.NextRunAt.After(now) {
			if !s.wait(task.NextRunAt.Sub(now)) {
				return
			}
			continue
		}

		runCtx, cancel := context.WithTimeout(s.ctx, 10*time.Minute)
		err = def.Run(runCtx, task)
		cancel()

		finishedAt := time.Now().UTC()
		interval := task.IntervalMinutes
		if interval <= 0 {
			interval = def.DefaultIntervalMinutes
		}
		nextRunAt := finishedAt.Add(time.Duration(interval) * time.Minute)
		lastError := ""
		if err != nil {
			lastError = err.Error()
		}
		_ = s.store.UpdateScheduledTaskRun(s.ctx, def.ID, finishedAt, &nextRunAt, lastError)
	}
}

func (s *Service) notifyUpdated() {
	s.mu.Lock()
	close(s.update)
	s.update = make(chan struct{})
	s.mu.Unlock()
}

func (s *Service) waitForUpdate() bool {
	return s.wait(0)
}

func (s *Service) wait(duration time.Duration) bool {
	s.mu.Lock()
	update := s.update
	s.mu.Unlock()
	if duration <= 0 {
		select {
		case <-s.ctx.Done():
			return false
		case <-update:
			return true
		}
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-s.ctx.Done():
		return false
	case <-update:
		return true
	case <-timer.C:
		return true
	}
}

package favorites

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"example.com/downgo/internal/bilibili"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/download"
)

const checkInterval = 10 * time.Minute

type Service struct {
	store    *db.Store
	settings *config.Service
	manager  *download.Manager
	client   *bilibili.Client

	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	mu     sync.Mutex
}

func NewService(store *db.Store, settings *config.Service, manager *download.Manager, client *bilibili.Client) *Service {
	if client == nil {
		client = bilibili.NewClient(nil)
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{store: store, settings: settings, manager: manager, client: client, ctx: ctx, cancel: cancel}
}

func (s *Service) Start() {
	s.once.Do(func() {
		go s.watchDownloads()
		go s.schedule()
	})
}

func (s *Service) Shutdown() {
	s.cancel()
}

func (s *Service) Folders(ctx context.Context) ([]domain.FavoriteFolder, error) {
	sessdata, _, ok := s.settings.BilibiliCredentials()
	if !ok {
		return nil, errors.New("未保存 Bilibili 登录凭据")
	}
	folders, err := s.client.GetFavoriteFolders(ctx, sessdata)
	if err != nil {
		return nil, err
	}
	result := make([]domain.FavoriteFolder, 0, len(folders))
	for _, folder := range folders {
		result = append(result, domain.FavoriteFolder{ID: folder.ID, Title: folder.Title})
	}
	return result, nil
}

func (s *Service) Subscription() (domain.FavoriteSubscription, error) {
	return s.store.GetFavoriteSubscription()
}

func (s *Service) SaveSubscription(sub domain.FavoriteSubscription) (domain.FavoriteSubscription, error) {
	if sub.Enabled && sub.MediaID <= 0 {
		return domain.FavoriteSubscription{}, errors.New("请选择 Bilibili 收藏夹")
	}
	return s.store.UpsertFavoriteSubscription(sub)
}

func (s *Service) RunOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runOnceLocked(ctx)
}

func (s *Service) schedule() {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			if err := s.RunOnce(s.ctx); err != nil {
				zap.L().Warn("bilibili favorite subscription run failed", zap.Error(err))
			}
			timer.Reset(checkInterval)
		}
	}
}

func (s *Service) watchDownloads() {
	events, unsubscribe := s.manager.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Type == "updated" && event.Item.Status == domain.StatusCompleted {
				s.handleCompletedDownload(s.ctx, event.Item.ID)
			}
		}
	}
}

func (s *Service) runOnceLocked(ctx context.Context) error {
	sub, err := s.store.GetFavoriteSubscription()
	if err != nil {
		return err
	}
	if !sub.Enabled || sub.MediaID <= 0 {
		return nil
	}
	sessdata, _, ok := s.settings.BilibiliCredentials()
	if !ok {
		err := errors.New("未保存 Bilibili 登录凭据")
		_ = s.store.UpdateFavoriteSubscriptionCheck(time.Now().UTC(), err.Error())
		return err
	}

	medias, err := s.client.GetFavoriteResources(ctx, sessdata, sub.MediaID)
	if err != nil {
		_ = s.store.UpdateFavoriteSubscriptionCheck(time.Now().UTC(), err.Error())
		return err
	}

	for _, media := range medias {
		if media.Bvid == "" || media.ID <= 0 || media.Type <= 0 {
			continue
		}
		origin := domain.FavoriteOrigin{
			MediaID:      sub.MediaID,
			ResourceID:   media.ID,
			ResourceType: media.Type,
			Bvid:         media.Bvid,
			Title:        media.Title,
		}
		if err := s.store.UpsertFavoriteResource(origin, "queued", ""); err != nil {
			zap.L().Warn("upsert favorite resource failed", zap.Error(err))
			continue
		}
		existing, err := s.store.DownloadsForFavoriteOrigin(origin)
		if err == nil && hasWorkingOrCompleted(existing) {
			continue
		}
		if err != nil {
			zap.L().Warn("load favorite resource downloads failed", zap.Error(err))
		}
		url := fmt.Sprintf("https://www.bilibili.com/video/%s", media.Bvid)
		if _, err := s.manager.CreateWithOrigin(ctx, url, &origin); err != nil {
			_ = s.store.UpsertFavoriteResource(origin, "error", err.Error())
			zap.L().Warn("create favorite download failed", zap.String("bvid", media.Bvid), zap.Error(err))
			continue
		}
	}
	s.processPendingRemovals(ctx)

	return s.store.UpdateFavoriteSubscriptionCheck(time.Now().UTC(), "")
}

func hasWorkingOrCompleted(items []domain.DownloadItem) bool {
	for _, item := range items {
		switch item.Status {
		case domain.StatusResolving, domain.StatusQueued, domain.StatusDownloading, domain.StatusPostprocessing, domain.StatusCompleted:
			return true
		}
	}
	return false
}

func (s *Service) handleCompletedDownload(ctx context.Context, downloadID int64) {
	origins, err := s.store.FavoriteOriginsForDownload(downloadID)
	if err != nil {
		zap.L().Warn("load favorite origins for completed download failed", zap.Int64("downloadId", downloadID), zap.Error(err))
		return
	}
	for _, origin := range origins {
		if err := s.removeIfAllCompleted(ctx, origin); err != nil {
			zap.L().Warn("remove completed favorite resource failed", zap.String("bvid", origin.Bvid), zap.Error(err))
		}
	}
}

func (s *Service) processPendingRemovals(ctx context.Context) {
	origins, err := s.store.PendingFavoriteResources()
	if err != nil {
		zap.L().Warn("load pending favorite resources failed", zap.Error(err))
		return
	}
	for _, origin := range origins {
		if err := s.removeIfAllCompleted(ctx, origin); err != nil {
			zap.L().Warn("remove pending favorite resource failed", zap.String("bvid", origin.Bvid), zap.Error(err))
		}
	}
}

func (s *Service) removeIfAllCompleted(ctx context.Context, origin domain.FavoriteOrigin) error {
	items, err := s.store.DownloadsForFavoriteOrigin(origin)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	for _, item := range items {
		if item.Status != domain.StatusCompleted {
			return nil
		}
	}
	sessdata, biliJct, ok := s.settings.BilibiliCredentials()
	if !ok {
		return errors.New("未保存 Bilibili 登录凭据")
	}
	media := bilibili.FavoriteMedia{ID: origin.ResourceID, Type: origin.ResourceType, Title: origin.Title, Bvid: origin.Bvid}
	if err := s.client.DeleteFavoriteResource(ctx, sessdata, biliJct, origin.MediaID, media); err != nil {
		_ = s.store.UpsertFavoriteResource(origin, "error", err.Error())
		return err
	}
	return s.store.MarkFavoriteResourceRemoved(origin)
}

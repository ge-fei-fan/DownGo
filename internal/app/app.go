package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"

	"example.com/downgo/internal/auth"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/deps"
	"example.com/downgo/internal/download"
	"example.com/downgo/internal/favorites"
	"example.com/downgo/internal/httpapi"
	"example.com/downgo/internal/util"
	"example.com/downgo/webui"
)

type App struct {
	baseDir   string
	logger    *zap.Logger
	store     *db.Store
	settings  *config.Service
	http      *http.Server
	password  string
	manager   *download.Manager
	favorites *favorites.Service
	appCtx    context.Context
	appCancel context.CancelFunc

	mu       sync.RWMutex
	listener net.Listener
	status   string

	errCh     chan error
	closeOnce sync.Once
}

func New(baseDir string, logger *zap.Logger) (*App, error) {
	store, err := db.Open(baseDir)
	if err != nil {
		return nil, err
	}

	settingsService, err := config.NewService(store, config.Defaults(baseDir))
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	initialPassword := ""
	if settingsService.Current().AccessTokenHash == "" {
		plain, err := util.RandomHex(8)
		if err != nil {
			_ = store.Close()
			return nil, err
		}
		initialPassword = plain
		if err := settingsService.SetPasswordHash(auth.HashPassword(plain)); err != nil {
			_ = store.Close()
			return nil, err
		}
	}

	manager, err := download.NewManagerWithBaseDir(baseDir, store, settingsService, download.NewPlatformRunner())
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	depsService := deps.NewService(baseDir, nil)
	favoritesService := favorites.NewService(store, settingsService, manager, nil)
	tokens := auth.NewTokenManager(baseDir + "|downgo")
	api := httpapi.NewAPI(baseDir, settingsService, manager, depsService, favoritesService, tokens)
	router := httpapi.NewRouter(api, webui.Assets)
	appCtx, appCancel := context.WithCancel(context.Background())

	current := settingsService.Current()
	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", current.BindHost, current.Port),
		Handler:           router,
		BaseContext:       func(net.Listener) context.Context { return appCtx },
		ReadHeaderTimeout: 15 * time.Second,
	}

	return &App{
		baseDir:   baseDir,
		logger:    logger,
		store:     store,
		settings:  settingsService,
		http:      httpServer,
		password:  initialPassword,
		manager:   manager,
		favorites: favoritesService,
		appCtx:    appCtx,
		appCancel: appCancel,
		status:    "未启动",
		errCh:     make(chan error, 1),
	}, nil
}

func (a *App) Start() error {
	current := a.settings.Current()
	addr := fmt.Sprintf("%s:%d", current.BindHost, current.Port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		a.setStatus("启动失败")
		return err
	}

	a.mu.Lock()
	a.listener = listener
	a.http.Addr = addr
	a.mu.Unlock()
	a.setStatus("运行中")

	if a.password != "" {
		a.logger.Info("generated initial password", zap.String("password", a.password))
	}
	a.logger.Info("http server listening", zap.String("addr", addr))
	a.logger.Info("available urls", zap.Strings("urls", a.AccessURLs()))
	if a.favorites != nil {
		a.favorites.Start()
	}

	go func() {
		if err := a.http.Serve(listener); err != nil && err != http.ErrServerClosed {
			a.setStatus("异常退出")
			select {
			case a.errCh <- err:
			default:
			}
		}
	}()

	return nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.Start(); err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return a.Shutdown(shutdownCtx)
	case err := <-a.errCh:
		_ = a.Shutdown(context.Background())
		return err
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	a.setStatus("正在停止")

	var shutdownErr error
	a.closeOnce.Do(func() {
		if a.appCancel != nil {
			a.appCancel()
		}

		if a.favorites != nil {
			a.favorites.Shutdown()
		}
		if a.manager != nil {
			if err := a.manager.Shutdown(ctx); err != nil {
				shutdownErr = err
			}
		}

		if a.http != nil {
			if err := a.http.Shutdown(ctx); err != nil {
				if closeErr := a.http.Close(); closeErr != nil {
					if a.logger != nil {
						a.logger.Error("forced http close failed", zap.Error(closeErr), zap.Error(err))
					}
					if shutdownErr == nil {
						shutdownErr = closeErr
					}
				} else if a.logger != nil {
					a.logger.Warn("graceful http shutdown timed out; forced close completed", zap.Error(err))
				}
			}
		}

		if a.store != nil {
			_ = a.store.Close()
		}
		a.setStatus("已停止")
	})

	return shutdownErr
}

func (a *App) Errors() <-chan error {
	return a.errCh
}

func (a *App) Current() config.Settings {
	return a.settings.Current()
}

func (a *App) Status() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

func (a *App) LogDir() string {
	return filepath.Join(a.baseDir, "data", "logs")
}

func (a *App) AccessURLs() []string {
	entries := buildAccessEntries(a.settings.Current().BindHost, a.settings.Current().Port, listIPv4Addresses())
	urls := make([]string, 0, len(entries))
	for _, entry := range entries {
		urls = append(urls, entry.URL)
	}
	return urls
}

func (a *App) AccessEntries() []AccessEntry {
	return buildAccessEntries(a.settings.Current().BindHost, a.settings.Current().Port, listIPv4Addresses())
}

func (a *App) setStatus(status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = status
}

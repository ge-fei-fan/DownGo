package tray

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"go.uber.org/zap"

	"example.com/downgo/internal/app"
)

type Runner struct {
	baseDir string
	logDir  string
	logger  *zap.Logger

	app    *app.App
	runErr error

	statusItem *systray.MenuItem
	portItem   *systray.MenuItem
	logItem    *systray.MenuItem
	quitItem   *systray.MenuItem
	urlItems   []*systray.MenuItem

	quitOnce sync.Once
}

func Run(baseDir, logDir string, logger *zap.Logger) error {
	runner := &Runner{
		baseDir: baseDir,
		logDir:  logDir,
		logger:  logger,
	}
	systray.Run(runner.onReady, runner.onExit)
	return runner.runErr
}

func (r *Runner) onReady() {
	systray.SetIcon(iconData())
	systray.SetTitle("DownGo")
	systray.SetTooltip("DownGo - 启动中")

	r.statusItem = systray.AddMenuItem("状态：启动中", "")
	r.statusItem.Disable()
	r.portItem = systray.AddMenuItem("端口：-", "")
	r.portItem.Disable()
	systray.AddSeparator()

	instance, err := app.New(r.baseDir, r.logger)
	if err != nil {
		r.failStartup(err)
		return
	}
	r.app = instance

	if err := r.app.Start(); err != nil {
		r.failStartup(err)
		return
	}

	r.statusItem.SetTitle("状态：" + r.app.Status())
	r.portItem.SetTitle(fmt.Sprintf("端口：%d", r.app.Current().Port))
	systray.SetTooltip(fmt.Sprintf("DownGo - %s - %d", r.app.Status(), r.app.Current().Port))

	for _, entry := range r.app.AccessEntries() {
		item := systray.AddMenuItem(entry.Label+"："+entry.URL, "打开浏览器")
		r.urlItems = append(r.urlItems, item)
		go func(url string, menu *systray.MenuItem) {
			for range menu.ClickedCh {
				if err := openURL(url); err != nil {
					r.logger.Error("open url failed", zap.String("url", url), zap.Error(err))
				}
			}
		}(entry.URL, item)
	}

	systray.AddSeparator()
	r.logItem = systray.AddMenuItem("打开日志目录", "")
	r.quitItem = systray.AddMenuItem("退出", "")

	go r.eventLoop()
}

func (r *Runner) eventLoop() {
	for {
		select {
		case <-r.logItem.ClickedCh:
			if err := openFolder(r.logDir); err != nil {
				r.logger.Error("open log directory failed", zap.String("dir", r.logDir), zap.Error(err))
			}
		case <-r.quitItem.ClickedCh:
			r.requestQuit("用户退出")
			return
		case err, ok := <-r.app.Errors():
			if !ok {
				return
			}
			if err == nil {
				continue
			}
			r.logger.Error("http server exited unexpectedly", zap.Error(err))
			r.statusItem.SetTitle("状态：异常退出")
			systray.SetTooltip("DownGo - 异常退出")
			showError("DownGo 运行异常", err.Error())
			r.requestQuit("服务异常")
			return
		}
	}
}

func (r *Runner) requestQuit(reason string) {
	r.quitOnce.Do(func() {
		if r.statusItem != nil {
			r.statusItem.SetTitle("状态：正在停止")
		}
		r.logger.Info("tray exit requested", zap.String("reason", reason))
		systray.Quit()
	})
}

func (r *Runner) failStartup(err error) {
	r.runErr = err
	r.logger.Error("startup failed", zap.Error(err))
	showError("DownGo 启动失败", err.Error())
	r.requestQuit("启动失败")
}

func (r *Runner) onExit() {
	if r.app == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := r.app.Shutdown(ctx); err != nil {
		r.logger.Error("shutdown failed", zap.Error(err))
	}
}

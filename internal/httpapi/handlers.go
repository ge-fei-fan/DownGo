package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"example.com/downgo/internal/auth"
	"example.com/downgo/internal/bilibili"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/deps"
	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/download"
	"example.com/downgo/internal/favorites"
	"example.com/downgo/internal/monitor"
	"example.com/downgo/internal/util"
)

type API struct {
	baseDir    string
	settings   *config.Service
	manager    *download.Manager
	deps       *deps.Service
	favorites  *favorites.Service
	tokens     *auth.TokenManager
	monitor    monitor.Collector
	disks      monitor.DiskProvider
	partitions monitor.PartitionProvider
}

type loginRequest struct {
	Password string `json:"password"`
}

type inspectRequest struct {
	URL string `json:"url"`
}

type createRequest struct {
	URL string `json:"url"`
}

type favoriteSubscriptionRequest struct {
	MediaID int64  `json:"mediaId"`
	Title   string `json:"title"`
	Enabled bool   `json:"enabled"`
}

type selectDownloadDirRequest struct {
	CurrentDir string `json:"currentDir"`
}

type jsonResponse map[string]any

func NewRouter(api *API, assets embed.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(jsonMiddleware)

	r.Post("/api/auth/login", api.handleLogin)
	r.Get("/api/public/downloads/progress", api.handlePublicProgress)
	r.Get("/api/public/downloads/completed", api.handlePublicCompleted)
	r.Get("/api/public/downloads/{id}/thumbnail", api.handlePublicThumbnail)
	r.Get("/api/public/system/metrics", api.handlePublicSystemMetrics)
	r.Get("/api/public/system/disks", api.handlePublicSystemDisks)
	r.Get("/api/public/system/disk-temperatures", api.handlePublicDiskTemperatures)
	r.Get("/api/public/system/disk-temperatures/current", api.handlePublicCurrentDiskTemperatures)
	r.Get("/api/public/system/disk-temperatures/history", api.handlePublicDiskTemperatureHistory)
	r.Get("/api/public/system/partitions", api.handlePublicSystemPartitions)
	r.Post("/api/public/downloads", api.handleCreate)
	r.Delete("/api/public/downloads/{id}", api.handlePublicDeleteCompleted)
	r.Delete("/api/public/downloads/{id}/file", api.handlePublicDeleteCompleted)

	r.Group(func(protected chi.Router) {
		protected.Use(api.authMiddleware)
		protected.Get("/api/settings", api.handleGetSettings)
		protected.Put("/api/settings", api.handlePutSettings)
		protected.Post("/api/settings/download-dir/select", api.handleSelectDownloadDir)
		protected.Post("/api/bilibili/qrcode", api.handleBilibiliQRCode)
		protected.Get("/api/bilibili/qrcode/poll", api.handleBilibiliQRCodePoll)
		protected.Get("/api/bilibili/session", api.handleBilibiliSession)
		protected.Post("/api/bilibili/session/check", api.handleCheckBilibiliSession)
		protected.Get("/api/bilibili/avatar", api.handleBilibiliAvatar)
		protected.Delete("/api/bilibili/session", api.handleClearBilibiliSession)
		protected.Get("/api/bilibili/favorites/folders", api.handleBilibiliFavoriteFolders)
		protected.Get("/api/bilibili/favorites/subscription", api.handleGetFavoriteSubscription)
		protected.Put("/api/bilibili/favorites/subscription", api.handlePutFavoriteSubscription)
		protected.Post("/api/bilibili/favorites/subscription/run", api.handleRunFavoriteSubscription)
		protected.Get("/api/tools/dependencies", api.handleGetDependencies)
		protected.Post("/api/tools/dependencies/install", api.handleInstallDependencies)
		protected.Get("/api/tools/dependencies/install/status", api.handleInstallDependenciesStatus)
		protected.Get("/api/tools/dependencies/install/events", api.handleInstallDependenciesEvents)
		protected.Post("/api/downloads/inspect", api.handleInspect)
		protected.Post("/api/downloads", api.handleCreate)
		protected.Get("/api/downloads", api.handleList)
		protected.Get("/api/downloads/events", api.handleEvents)
		protected.Post("/api/downloads/{id}/retry", api.handleRetry)
		protected.Post("/api/downloads/{id}/open-path", api.handleOpenPath)
		protected.Delete("/api/downloads/{id}", api.handleDelete)
		protected.Get("/api/downloads/{id}/thumbnail", api.handleThumbnail)
		protected.Get("/api/downloads/{id}/file", api.handleFile)
	})

	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	indexHTML, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(err)
	}

	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			writeJSON(w, http.StatusNotFound, jsonResponse{"error": "not found"})
			return
		}

		clean := path.Clean(strings.TrimPrefix(req.URL.Path, "/"))
		if clean != "." && clean != "" {
			if _, err := fs.Stat(sub, clean); err == nil {
				fileServer.ServeHTTP(w, req)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(indexHTML)
	})

	return r
}

func NewAPI(baseDir string, settings *config.Service, manager *download.Manager, depsService *deps.Service, favoritesService *favorites.Service, tokens *auth.TokenManager) *API {
	return &API{
		baseDir:    baseDir,
		settings:   settings,
		manager:    manager,
		deps:       depsService,
		favorites:  favoritesService,
		tokens:     tokens,
		monitor:    monitor.NewCollector(time.Now()),
		disks:      monitor.NewDiskService(30 * time.Minute),
		partitions: monitor.NewPartitionService(),
	}
}

func (api *API) SetDiskProvider(disks monitor.DiskProvider) {
	api.disks = disks
}

func (api *API) SetPartitionProvider(partitions monitor.PartitionProvider) {
	api.partitions = partitions
}

func (api *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "请求体格式不正确"})
		return
	}

	settings := api.settings.Current()
	if !auth.VerifyPassword(settings.AccessTokenHash, req.Password) {
		writeJSON(w, http.StatusUnauthorized, jsonResponse{"error": "密码错误"})
		return
	}

	token, err := api.tokens.Issue("downgo", 24*time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, jsonResponse{"token": token})
}

func (api *API) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.settings.Current())
}

func (api *API) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var input config.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "请求体格式不正确"})
		return
	}

	settings, err := api.settings.Update(input, auth.HashPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, settings)
}

func (api *API) handleSelectDownloadDir(w http.ResponseWriter, r *http.Request) {
	var req selectDownloadDirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		zap.L().Warn("select download directory request decode failed", zap.Error(err))
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "请求体格式不正确"})
		return
	}

	zap.L().Info("select download directory requested", zap.String("currentDir", req.CurrentDir), zap.String("remoteAddr", r.RemoteAddr))
	selected, err := util.SelectFolder(req.CurrentDir)
	if err != nil {
		zap.L().Error("select download directory failed", zap.String("currentDir", req.CurrentDir), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	if selected == "" {
		zap.L().Info("select download directory canceled", zap.String("currentDir", req.CurrentDir))
		w.WriteHeader(http.StatusNoContent)
		return
	}

	zap.L().Info("select download directory completed", zap.String("selectedDir", selected))
	writeJSON(w, http.StatusOK, jsonResponse{"path": selected})
}

func (api *API) handleBilibiliQRCode(w http.ResponseWriter, r *http.Request) {
	qr, err := bilibili.NewClient(nil).GenerateQRCode(r.Context())
	if err != nil {
		zap.L().Error("generate bilibili qrcode failed", zap.Error(err))
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, qr)
}

func (api *API) handleBilibiliQRCodePoll(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	result, err := bilibili.NewClient(nil).PollQRCode(r.Context(), key)
	if err != nil {
		zap.L().Error("poll bilibili qrcode failed", zap.Error(err))
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
		return
	}

	response := jsonResponse{
		"code":    result.Code,
		"message": result.Message,
	}
	if result.Code == 0 && result.Profile != nil {
		faceLocal, avatarErr := api.downloadBilibiliAvatar(r, bilibili.NewClient(nil), result.Profile.Face)
		if avatarErr != nil {
			zap.L().Warn("bilibili avatar download failed", zap.String("face", result.Profile.Face), zap.Error(avatarErr))
		}
		session, err := api.settings.SetBilibiliSession(
			result.Tokens.Sessdata,
			result.Tokens.BiliJct,
			result.Profile.Mid,
			result.Profile.Uname,
			result.Profile.Face,
			faceLocal,
			result.LoginAt,
		)
		if err != nil {
			zap.L().Error("save bilibili session failed", zap.Error(err))
			writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
			return
		}
		response["session"] = session
		zap.L().Info("bilibili login saved", zap.Int("mid", session.Mid), zap.String("uname", session.Uname))
	}
	writeJSON(w, http.StatusOK, response)
}

func (api *API) handleBilibiliSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.settings.BilibiliSession())
}

func (api *API) handleCheckBilibiliSession(w http.ResponseWriter, r *http.Request) {
	sessdata, _, ok := api.settings.BilibiliCredentials()
	if !ok {
		session := api.settings.BilibiliSession()
		session.Status = "missing"
		session.Message = "未保存 Bilibili 登录凭据"
		writeJSON(w, http.StatusOK, session)
		return
	}

	checkedAt := time.Now().UTC()
	biliClient := bilibili.NewClient(nil)
	profile, err := biliClient.CheckLogin(r.Context(), sessdata)
	if err != nil {
		status := "error"
		message := "检查失败：网络异常或 Bilibili 接口不可用"
		if errors.Is(err, bilibili.ErrNotLoggedIn) {
			status = "invalid"
			message = "Bilibili 登录已失效，请重新扫码登录"
		}
		session, saveErr := api.settings.UpdateBilibiliCheck(status, message, 0, "", "", "", checkedAt)
		if saveErr != nil {
			writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": saveErr.Error()})
			return
		}
		zap.L().Warn("bilibili session check failed", zap.String("status", status), zap.Error(err))
		writeJSON(w, http.StatusOK, session)
		return
	}

	spaceInfo, err := biliClient.GetSpaceInfo(r.Context(), sessdata, profile.Mid)
	if err != nil {
		message := "Bilibili 登录有效，但获取空间信息失败：" + err.Error()
		faceLocal, avatarErr := api.downloadBilibiliAvatar(r, biliClient, profile.Face)
		if avatarErr != nil {
			zap.L().Warn("bilibili avatar download failed", zap.String("face", profile.Face), zap.Error(avatarErr))
		}
		session, saveErr := api.settings.UpdateBilibiliCheck("valid", message, profile.Mid, profile.Uname, profile.Face, faceLocal, checkedAt)
		if saveErr != nil {
			writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": saveErr.Error()})
			return
		}
		zap.L().Warn("bilibili space info fetch failed", zap.Int("mid", profile.Mid), zap.Error(err))
		writeJSON(w, http.StatusOK, session)
		return
	}

	session, err := api.settings.UpdateBilibiliProfileCheck(
		"valid",
		"Bilibili 登录有效，已更新空间资料",
		spaceInfo.Mid,
		spaceInfo.Name,
		spaceInfo.Face,
		api.mustDownloadBilibiliAvatar(r, biliClient, spaceInfo.Face),
		spaceInfo.Level,
		spaceInfo.Sex,
		spaceInfo.Sign,
		spaceInfo.Vip.Status,
		spaceInfo.Vip.Type,
		spaceInfo.Vip.Label.Text,
		spaceInfo.Vip.DueDate,
		spaceInfo.SeniorMember == 1,
		checkedAt,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	zap.L().Info("bilibili session check succeeded", zap.Int("mid", spaceInfo.Mid), zap.String("uname", spaceInfo.Name))
	writeJSON(w, http.StatusOK, session)
}

func (api *API) handleClearBilibiliSession(w http.ResponseWriter, r *http.Request) {
	_ = os.Remove(api.bilibiliAvatarPath())
	if err := api.settings.ClearBilibiliSession(); err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) handleBilibiliAvatar(w http.ResponseWriter, r *http.Request) {
	avatarPath := api.bilibiliAvatarPath()
	if _, err := os.Stat(avatarPath); err != nil {
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": "Bilibili 本地头像不存在，请先检查登录状态"})
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, avatarPath)
}

func (api *API) downloadBilibiliAvatar(r *http.Request, client *bilibili.Client, face string) (string, error) {
	if face == "" {
		return "", nil
	}
	if err := client.DownloadAvatar(r.Context(), face, api.bilibiliAvatarPath()); err != nil {
		return "", err
	}
	return "/api/bilibili/avatar", nil
}

func (api *API) mustDownloadBilibiliAvatar(r *http.Request, client *bilibili.Client, face string) string {
	faceLocal, err := api.downloadBilibiliAvatar(r, client, face)
	if err != nil {
		zap.L().Warn("bilibili avatar download failed", zap.String("face", face), zap.Error(err))
		return ""
	}
	return faceLocal
}

func (api *API) bilibiliAvatarPath() string {
	return filepath.Join(api.baseDir, "data", "bilibili", "avatar")
}

func (api *API) handleBilibiliFavoriteFolders(w http.ResponseWriter, r *http.Request) {
	if api.favorites == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "收藏夹订阅服务未启用"})
		return
	}
	folders, err := api.favorites.Folders(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

func (api *API) handleGetFavoriteSubscription(w http.ResponseWriter, r *http.Request) {
	if api.favorites == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "收藏夹订阅服务未启用"})
		return
	}
	sub, err := api.favorites.Subscription()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (api *API) handlePutFavoriteSubscription(w http.ResponseWriter, r *http.Request) {
	if api.favorites == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "收藏夹订阅服务未启用"})
		return
	}
	var req favoriteSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "请求体格式不正确"})
		return
	}
	sub, err := api.favorites.SaveSubscription(domain.FavoriteSubscription{
		MediaID: req.MediaID,
		Title:   req.Title,
		Enabled: req.Enabled,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (api *API) handleRunFavoriteSubscription(w http.ResponseWriter, r *http.Request) {
	if api.favorites == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "收藏夹订阅服务未启用"})
		return
	}
	if err := api.favorites.RunOnce(r.Context()); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
		return
	}
	sub, err := api.favorites.Subscription()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

func (api *API) handleGetDependencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.deps.Status())
}

func (api *API) handleInstallDependencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.deps.StartInstall())
}

func (api *API) handleInstallDependenciesStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.deps.InstallSnapshot())
}

func (api *API) handleInstallDependenciesEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "当前服务器不支持实时进度"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	zap.L().Info("dependency install event stream started", zap.String("remoteAddr", r.RemoteAddr))
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	snapshot, ch, unsubscribe := api.deps.SubscribeInstall()
	defer unsubscribe()
	for _, event := range snapshot.Events {
		if !writeDependencyInstallEvent(w, flusher, event) {
			return
		}
	}
	if !snapshot.Installing {
		zap.L().Info("dependency install event stream completed without active install", zap.String("remoteAddr", r.RemoteAddr))
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if !writeDependencyInstallEvent(w, flusher, event) {
				return
			}
			if event.Type == "done" {
				zap.L().Info("dependency install event stream completed", zap.String("remoteAddr", r.RemoteAddr))
				return
			}
		}
	}
}

func writeDependencyInstallEvent(w http.ResponseWriter, flusher http.Flusher, event deps.ProgressEvent) bool {
	payload, err := json.Marshal(event)
	if err != nil {
		zap.L().Error("marshal dependency install event failed", zap.Error(err))
		return true
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		zap.L().Error("write dependency install event failed", zap.Error(err))
		return false
	}
	flusher.Flush()
	return true
}

func (api *API) handleInspect(w http.ResponseWriter, r *http.Request) {
	var req inspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "请求体格式不正确"})
		return
	}

	result, err := api.manager.Inspect(r.Context(), req.URL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (api *API) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "请求体格式不正确"})
		return
	}

	item, err := api.manager.Create(r.Context(), req.URL)
	if err != nil {
		var duplicate download.ErrAlreadyDownloaded
		if errors.As(err, &duplicate) {
			writeJSON(w, http.StatusConflict, jsonResponse{
				"code":  "DOWNLOAD_ALREADY_EXISTS",
				"error": err.Error(),
				"item":  duplicate.Existing,
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, item)
}

func (api *API) handleList(w http.ResponseWriter, r *http.Request) {
	view := r.URL.Query().Get("view")
	if view == "" {
		view = "active"
	}
	api.writeDownloadsList(w, r, view)
}

func (api *API) handlePublicProgress(w http.ResponseWriter, r *http.Request) {
	api.writeDownloadsList(w, r, "active")
}

func (api *API) handlePublicCompleted(w http.ResponseWriter, r *http.Request) {
	page := parseInt(r.URL.Query().Get("page"), 1)
	pageSize := parseInt(r.URL.Query().Get("pageSize"), 20)

	result, err := api.manager.List("completed", page, pageSize)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	publicizeCompletedThumbnails(result.Items)
	writeJSON(w, http.StatusOK, result)
}

func (api *API) handlePublicSystemMetrics(w http.ResponseWriter, r *http.Request) {
	if api.monitor == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "system metrics are unavailable"})
		return
	}
	metrics, err := api.monitor.Snapshot(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (api *API) handlePublicSystemDisks(w http.ResponseWriter, r *http.Request) {
	if api.disks == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "disk metrics are unavailable"})
		return
	}
	disks, err := api.disks.Disks(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, disks)
}

func (api *API) handlePublicDiskTemperatures(w http.ResponseWriter, r *http.Request) {
	if api.disks == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "disk metrics are unavailable"})
		return
	}
	temperatures, err := api.disks.DiskTemperatures(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, temperatures)
}

func (api *API) handlePublicCurrentDiskTemperatures(w http.ResponseWriter, r *http.Request) {
	if api.disks == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "disk metrics are unavailable"})
		return
	}
	temperatures, err := api.disks.RefreshDiskTemperatures(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, temperatures)
}

func (api *API) handlePublicDiskTemperatureHistory(w http.ResponseWriter, r *http.Request) {
	if api.disks == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "disk metrics are unavailable"})
		return
	}
	now := time.Now().UTC()
	from := now.Add(-24 * time.Hour)
	to := now
	var err error
	if value := strings.TrimSpace(r.URL.Query().Get("from")); value != "" {
		from, err = parseAPITime(value)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "invalid from time"})
			return
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("to")); value != "" {
		to, err = parseAPITime(value)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "invalid to time"})
			return
		}
	}
	if from.After(to) {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "from must be before to"})
		return
	}
	limit := 2000
	if value := strings.TrimSpace(r.URL.Query().Get("limit")); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "invalid limit"})
			return
		}
	}
	if limit <= 0 {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "limit must be positive"})
		return
	}
	if limit > 10000 {
		limit = 10000
	}

	history, err := api.disks.DiskTemperatureHistory(r.Context(), from, to, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, history)
}

func (api *API) handlePublicSystemPartitions(w http.ResponseWriter, r *http.Request) {
	if api.partitions == nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "partition metrics are unavailable"})
		return
	}
	partitions, err := api.partitions.Partitions(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, partitions)
}

func (api *API) writeDownloadsList(w http.ResponseWriter, r *http.Request, view string) {
	page := parseInt(r.URL.Query().Get("page"), 1)
	pageSize := parseInt(r.URL.Query().Get("pageSize"), 20)

	result, err := api.manager.List(view, page, pageSize)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseAPITime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func (api *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": "当前环境不支持流式推送"})
		return
	}

	ch, unsubscribe := api.manager.Subscribe()
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
		}
	}
}

func (api *API) handleRetry(w http.ResponseWriter, r *http.Request) {
	item, err := api.manager.Retry(routeID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (api *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := api.manager.Delete(routeID(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) handlePublicDeleteCompleted(w http.ResponseWriter, r *http.Request) {
	if err := api.manager.DeleteCompleted(routeID(r)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) handlePublicThumbnail(w http.ResponseWriter, r *http.Request) {
	id := routeID(r)
	item, err := api.manager.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": err.Error()})
		return
	}
	if item.Status != domain.StatusCompleted {
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": "thumbnail not found"})
		return
	}
	if item.ThumbnailURL != protectedThumbnailURL(id) {
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": "thumbnail not found"})
		return
	}
	thumbnailPath := api.manager.ThumbnailPath(id)
	if _, err := os.Stat(thumbnailPath); err != nil {
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": "thumbnail not found"})
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Del("Content-Type")
	http.ServeFile(w, r, thumbnailPath)
}

func (api *API) handleOpenPath(w http.ResponseWriter, r *http.Request) {
	id := routeID(r)
	zap.L().Info("open download path requested", zap.Int64("id", id), zap.String("remoteAddr", r.RemoteAddr))
	if err := api.manager.OpenPath(id); err != nil {
		zap.L().Error("open download path failed", zap.Int64("id", id), zap.Error(err))
		writeError(w, err)
		return
	}
	zap.L().Info("open download path completed", zap.Int64("id", id))
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) handleFile(w http.ResponseWriter, r *http.Request) {
	item, err := api.manager.Get(routeID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	if item.Status != domain.StatusCompleted {
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": "文件尚未下载完成"})
		return
	}
	http.ServeFile(w, r, item.OutputPath)
}

func (api *API) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	id := routeID(r)
	item, err := api.manager.Get(id)
	if err != nil {
		writeError(w, err)
		return
	}
	if item.ThumbnailURL != protectedThumbnailURL(id) {
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": "本地封面不存在"})
		return
	}
	thumbnailPath := api.manager.ThumbnailPath(id)
	if _, err := os.Stat(thumbnailPath); err != nil {
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": "本地封面不存在"})
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Del("Content-Type")
	http.ServeFile(w, r, thumbnailPath)
}

func publicizeCompletedThumbnails(items []domain.DownloadItem) {
	for i := range items {
		if items[i].ThumbnailURL == protectedThumbnailURL(items[i].ID) {
			items[i].ThumbnailURL = publicThumbnailURL(items[i].ID)
		}
	}
}

func protectedThumbnailURL(id int64) string {
	return fmt.Sprintf("/api/downloads/%d/thumbnail", id)
}

func publicThumbnailURL(id int64) string {
	return fmt.Sprintf("/api/public/downloads/%d/thumbnail", id)
}

func (api *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, jsonResponse{"code": "missing_token", "error": "缺少访问令牌"})
			return
		}
		if _, err := api.tokens.Verify(token); err != nil {
			writeJSON(w, http.StatusUnauthorized, jsonResponse{"code": "invalid_token", "error": "访问令牌无效"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jsonMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}

func extractToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return r.URL.Query().Get("token")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeJSON(w, http.StatusNotFound, jsonResponse{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, jsonResponse{"error": err.Error()})
	}
}

func routeID(r *http.Request) int64 {
	value := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(value, 10, 64)
	return id
}

func parseInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

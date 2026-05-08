package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"example.com/downgo/internal/auth"
	"example.com/downgo/internal/config"
	"example.com/downgo/internal/db"
	"example.com/downgo/internal/deps"
	"example.com/downgo/internal/domain"
	"example.com/downgo/internal/download"
)

type API struct {
	settings *config.Service
	manager  *download.Manager
	deps     *deps.Service
	tokens   *auth.TokenManager
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

type jsonResponse map[string]any

func NewRouter(api *API, assets embed.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(jsonMiddleware)

	r.Post("/api/auth/login", api.handleLogin)

	r.Group(func(protected chi.Router) {
		protected.Use(api.authMiddleware)
		protected.Get("/api/settings", api.handleGetSettings)
		protected.Put("/api/settings", api.handlePutSettings)
		protected.Get("/api/tools/dependencies", api.handleGetDependencies)
		protected.Post("/api/tools/dependencies/install", api.handleInstallDependencies)
		protected.Post("/api/downloads/inspect", api.handleInspect)
		protected.Post("/api/downloads", api.handleCreate)
		protected.Get("/api/downloads", api.handleList)
		protected.Get("/api/downloads/events", api.handleEvents)
		protected.Post("/api/downloads/{id}/cancel", api.handleCancel)
		protected.Post("/api/downloads/{id}/retry", api.handleRetry)
		protected.Delete("/api/downloads/{id}", api.handleDelete)
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

func NewAPI(settings *config.Service, manager *download.Manager, depsService *deps.Service, tokens *auth.TokenManager) *API {
	return &API{settings: settings, manager: manager, deps: depsService, tokens: tokens}
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

func (api *API) handleGetDependencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.deps.Status())
}

func (api *API) handleInstallDependencies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, api.deps.InstallMissing(r.Context()))
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
	page := parseInt(r.URL.Query().Get("page"), 1)
	pageSize := parseInt(r.URL.Query().Get("pageSize"), 20)

	result, err := api.manager.List(view, page, pageSize)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, jsonResponse{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
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

func (api *API) handleCancel(w http.ResponseWriter, r *http.Request) {
	item, err := api.manager.Cancel(routeID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
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

func (api *API) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, jsonResponse{"error": "缺少访问令牌"})
			return
		}
		if _, err := api.tokens.Verify(token); err != nil {
			writeJSON(w, http.StatusUnauthorized, jsonResponse{"error": "访问令牌无效"})
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

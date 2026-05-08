package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"

	"example.com/downgo/internal/db"
	"example.com/downgo/internal/download"
)

func TestShutdownCancelsActiveRequestsBeforeHTTPShutdown(t *testing.T) {
	appCtx, appCancel := context.WithCancel(context.Background())
	requestDone := make(chan struct{})

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			<-r.Context().Done()
			close(requestDone)
		}),
		BaseContext: func(net.Listener) context.Context { return appCtx },
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}

	application := &App{
		logger:    zap.NewNop(),
		store:     &db.Store{},
		http:      server,
		manager:   &download.Manager{},
		appCtx:    appCtx,
		appCancel: appCancel,
		status:    "运行中",
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Serve(listener)
	}()

	resp, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer resp.Body.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := application.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("request was not canceled during shutdown")
	}

	_, _ = io.Copy(io.Discard, resp.Body)

	select {
	case err := <-serverErr:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("server returned unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after shutdown")
	}
}

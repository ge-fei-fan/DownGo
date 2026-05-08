package download

import (
	"context"
	"testing"
	"time"
)

func TestManagerShutdownCancelsActiveJobsAndWaits(t *testing.T) {
	t.Parallel()

	jobCtx, jobCancel := context.WithCancel(context.Background())
	manager := &Manager{
		active: map[int64]*activeJob{
			1: {cancel: jobCancel},
		},
	}

	manager.jobs.Add(1)
	done := make(chan struct{})
	go func() {
		defer manager.jobs.Done()
		<-jobCtx.Done()
		close(done)
	}()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected active job to be canceled during shutdown")
	}

	if !manager.shuttingDown {
		t.Fatal("expected manager to be marked as shutting down")
	}
}

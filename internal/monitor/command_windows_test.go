package monitor

import (
	"context"
	"testing"
)

func TestHiddenCommandContextHidesWindow(t *testing.T) {
	t.Parallel()

	cmd := hiddenCommandContext(context.Background(), "powershell", "-NoProfile")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v, want HideWindow=true", cmd.SysProcAttr)
	}
}

package uucapture

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeLog(t *testing.T, history, live []string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, lines []string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	h, l := write("history.ndjson", history), write("live.ndjson", live)
	script := filepath.Join(dir, "log")
	body := "#!/bin/sh\nif [ \"$1\" = show ]; then cat '" + h + "'; exit 0; fi\ncat '" + l + "'\nexec sleep 30\n"
	if err := os.WriteFile(script, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestWatcherReplaysThenFollows(t *testing.T) {
	history := []string{
		line("09:00:00.000000", scDisplay("79"), 100),
		line("09:00:00.100000", startCapture, 100),
	}
	live := []string{
		line("09:00:10.000000", stopCapture, 100),
		line("09:00:10.040000", scDisplay("5"), 100),
		line("09:00:10.050000", startCapture, 100),
	}
	w := NewWatcher(func() int32 { return 100 }, t.Logf)
	w.logTool, w.logPrefix = "/bin/sh", []string{fakeLog(t, history, live)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	now := ts(t, "09:00:20.000")
	deadline := time.After(5 * time.Second)
	for {
		active, showing, known := w.Sample(now, 100)
		if known && active && showing == 5 {
			break
		}
		select {
		case <-w.Changed():
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("watcher never saw display 5: active %v showing %d known %v", active, showing, known)
		}
	}
	if active, _, known := w.Sample(now, 200); !known || active {
		t.Errorf("after the server changed: active %v known %v", active, known)
	}
}

func TestWatcherUnknownWhileNotRunning(t *testing.T) {
	w := NewWatcher(func() int32 { return 0 }, t.Logf)
	if _, _, known := w.Sample(time.Now(), 0); known {
		t.Error("a watcher that never ran says it knows")
	}
}

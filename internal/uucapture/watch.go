package uucapture

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const ReplayWindow = "4h"

var (
	restartDelays = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}
	stableAfter   = time.Minute
)

type Watcher struct {
	logTool   string
	logPrefix []string
	serverPID func() int32
	logf      func(format string, args ...any)

	mu        sync.Mutex
	gen       int
	running   bool
	replaying bool
	live      []Record
	state     State
	pid       int32
	changed   chan struct{}
}

func NewWatcher(serverPID func() int32, logf func(format string, args ...any)) *Watcher {
	return &Watcher{logTool: "/usr/bin/log", serverPID: serverPID, logf: logf, changed: make(chan struct{}, 1)}
}

func (w *Watcher) Changed() <-chan struct{} { return w.changed }

func (w *Watcher) Sample(now time.Time, server int32) (active bool, showing uint32, known bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running || w.replaying {
		return false, 0, false
	}
	if server != w.pid {
		if w.pid != 0 {
			w.logf("uucapture: UURemoteServer changed from pid %d to %d; starting over", w.pid, server)
			w.state = State{}
		}
		w.pid = server
	}
	return w.state.Active(now), w.state.Streaming(now), true
}

func (w *Watcher) Run(ctx context.Context) {
	failures := 0
	for {
		started := time.Now()
		err := w.stream(ctx)
		if ctx.Err() != nil {
			return
		}
		ran := time.Since(started)
		if ran >= stableAfter {
			failures = 1
		} else {
			failures++
		}
		delay := restartDelays[min(failures, len(restartDelays))-1]
		w.logf("uucapture: log stream ended after %s (%v); restarting in %s", ran.Round(time.Second), err, delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (w *Watcher) stream(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, w.logTool, w.args("stream", "--style", "ndjson", "--predicate", Predicate())...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limited{w: &stderr, n: 300}
	if err := cmd.Start(); err != nil {
		return err
	}
	w.mu.Lock()
	w.gen++
	gen := w.gen
	w.running, w.replaying, w.live, w.state = true, true, nil, State{}
	w.pid = w.serverPID()
	w.mu.Unlock()
	go w.replay(ctx, gen)

	r := bufio.NewReaderSize(out, 64<<10)
	for {
		line, err := r.ReadBytes('\n')
		if rec, ok := Parse(line); ok {
			w.receive(gen, rec)
		}
		if err != nil {
			break
		}
	}
	err = cmd.Wait()
	w.mu.Lock()
	if w.gen == gen {
		w.running = false
	}
	w.mu.Unlock()
	w.notify()
	if msg := strings.TrimSpace(stderr.String()); msg != "" && err != nil {
		return &streamError{err: err, msg: msg}
	}
	return err
}

func (w *Watcher) receive(gen int, r Record) {
	w.mu.Lock()
	if w.gen != gen {
		w.mu.Unlock()
		return
	}
	if w.replaying {
		w.live = append(w.live, r)
	} else {
		w.state.Apply(r)
	}
	w.mu.Unlock()
	w.notify()
}

func (w *Watcher) replay(ctx context.Context, gen int) {
	out, err := exec.CommandContext(ctx, w.logTool, w.args("show", "--last", ReplayWindow, "--style", "ndjson", "--predicate", Predicate())...).Output()
	var history []Record
	if err != nil {
		w.logf("uucapture: could not read the last %s of the log: %v", ReplayWindow, err)
	} else {
		for line := range bytes.SplitSeq(out, []byte{'\n'}) {
			if rec, ok := Parse(line); ok {
				history = append(history, rec)
			}
		}
	}
	w.mu.Lock()
	if w.gen != gen {
		w.mu.Unlock()
		return
	}
	w.state = Resumed(history, w.live, w.pid)
	w.replaying, w.live = false, nil
	now := time.Now()
	active, showing := w.state.Active(now), w.state.Streaming(now)
	w.mu.Unlock()
	w.logf("uucapture: read %d records from the last %s; streaming %v, display %d", len(history), ReplayWindow, active, showing)
	w.notify()
}

func (w *Watcher) args(a ...string) []string {
	return append(append([]string(nil), w.logPrefix...), a...)
}

func (w *Watcher) notify() {
	select {
	case w.changed <- struct{}{}:
	default:
	}
}

type limited struct {
	w io.Writer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

type streamError struct {
	err error
	msg string
}

func (e *streamError) Error() string { return e.err.Error() + ": " + e.msg }
func (e *streamError) Unwrap() error { return e.err }

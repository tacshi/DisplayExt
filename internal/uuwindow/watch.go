package uuwindow

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

type Reading struct {
	Trusted  bool
	Device   string
	NoWindow bool
	Reopened bool
	Err      string
}

type Watcher struct {
	find     func() int32
	read     func(pid int32) ([]Element, error)
	trust    func() bool
	reopen   func() error
	minimize func(pid int32) error
	pause    time.Duration
	active   atomic.Bool
	wake     chan struct{}

	reopened  bool
	minimized bool
}

func NewWatcher(find func() int32) *Watcher {
	return &Watcher{find: find, read: Read, trust: Trusted, reopen: Reopen, minimize: Minimize,
		pause: 800 * time.Millisecond, wake: make(chan struct{}, 1)}
}

func (w *Watcher) SetActive(on bool) {
	if w.active.Swap(on) != on {
		w.Wake()
	}
}

func (w *Watcher) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Watcher) Run(ctx context.Context, every time.Duration, onChange func(Reading)) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	var last Reading
	started := false
	for {
		r := w.sample()
		if !started || r != last {
			started, last = true, r
			onChange(r)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-w.wake:
		}
	}
}

func (w *Watcher) sample() Reading {
	r := Reading{Trusted: w.trust()}
	if !w.active.Load() {
		w.reopened, w.minimized = false, false
		return r
	}
	if !r.Trusted {
		return r
	}
	pid := w.find()
	if pid == 0 {
		r.Err = "UU远程 is not running"
		return r
	}
	wins, err := w.read(pid)
	if errors.Is(err, ErrNoWindows) && !w.reopened && w.reopen != nil {
		w.reopened = true
		if rerr := w.reopen(); rerr == nil {
			time.Sleep(w.pause)
			wins, err = w.read(pid)
		}
	}
	if err == nil && w.reopened && !w.minimized && w.minimize != nil {
		w.minimized = true
		_ = w.minimize(pid)
	}
	r.Reopened = w.reopened
	switch {
	case errors.Is(err, ErrNotTrusted):
		r.Trusted = false
	case errors.Is(err, ErrNoWindows):
		r.NoWindow, r.Err = true, err.Error()
	case err != nil:
		r.Err = err.Error()
	default:
		if names := Controllers(wins); len(names) > 0 {
			r.Device = names[0]
		} else {
			r.Err = "no controlling device in UU远程's window"
		}
	}
	return r
}

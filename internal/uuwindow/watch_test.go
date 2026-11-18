package uuwindow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWatcherReadsOnlyWhileActive(t *testing.T) {
	reads := 0
	w := &Watcher{
		find:  func() int32 { return 7 },
		trust: func() bool { return true },
		read: func(pid int32) ([]Element, error) {
			reads++
			if pid != 7 {
				return nil, errors.New("wrong pid")
			}
			return []Element{node(Other, text("本机被控中"), node(Table, controllerRow("Rocky的iPhone")))}, nil
		},
		wake: make(chan struct{}, 1),
	}
	if r := w.sample(); r.Device != "" || reads != 0 || !r.Trusted {
		t.Fatalf("inactive sample %+v after %d reads", r, reads)
	}
	w.SetActive(true)
	if r := w.sample(); r.Device != "Rocky的iPhone" || r.Err != "" {
		t.Errorf("active sample %+v", r)
	}
}

func TestWatcherReports(t *testing.T) {
	trusted := false
	w := &Watcher{
		find:  func() int32 { return 7 },
		trust: func() bool { return trusted },
		read:  func(int32) ([]Element, error) { return nil, ErrNoWindows },
		wake:  make(chan struct{}, 1),
	}
	w.SetActive(true)
	if r := w.sample(); r.Trusted || r.Err != "" {
		t.Errorf("untrusted sample %+v", r)
	}
	trusted = true
	if r := w.sample(); !r.Trusted || !r.NoWindow || r.Err == "" || r.Device != "" {
		t.Errorf("no windows sample %+v", r)
	}

	got := make(chan Reading, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx, time.Hour, func(r Reading) { got <- r })
	select {
	case r := <-got:
		if !r.Trusted {
			t.Errorf("first reading %+v", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no first reading")
	}
}

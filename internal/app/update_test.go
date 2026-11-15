package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"displayext/locales"
)

func standInForUpdates(t *testing.T, look func(context.Context) (*newerVersion, error)) (*App, chan func()) {
	t.Helper()
	old, every := lookForUpdate, updateEvery
	t.Cleanup(func() { lookForUpdate, updateEvery = old, every })
	lookForUpdate = look
	posted := make(chan func(), 64)
	a := &App{updatable: true, post: func(fn func()) { posted <- fn }}
	return a, posted
}

func settle(t *testing.T, a *App, posted chan func()) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for a.updateBusy {
		select {
		case fn := <-posted:
			fn()
		case <-deadline:
			t.Fatal("the look for an update never ended")
		}
	}
}

func TestLooksForUpdatesWhileRunning(t *testing.T) {
	looks := make(chan struct{}, 8)
	a, posted := standInForUpdates(t, func(context.Context) (*newerVersion, error) {
		looks <- struct{}{}
		return nil, nil
	})
	updateEvery = time.Millisecond
	a.keepLookingForUpdates()
	deadline := time.After(5 * time.Second)
	for n := 0; n < 3; {
		select {
		case <-looks:
			n++
		case fn := <-posted:
			fn()
		case <-deadline:
			t.Fatalf("%d looks for a newer version, want 3", n)
		}
	}
}

func TestNoUpdatesNoLook(t *testing.T) {
	a, _ := standInForUpdates(t, func(context.Context) (*newerVersion, error) {
		t.Error("looked for an update in an app that cannot update")
		return nil, nil
	})
	a.updatable = false
	a.keepLookingForUpdates()
	if a.updateBusy {
		t.Error("busy with an update it cannot install")
	}
}

func TestInstalledUpdateWaitsForRestart(t *testing.T) {
	installs := 0
	a, posted := standInForUpdates(t, func(context.Context) (*newerVersion, error) {
		return &newerVersion{"0.2.0", func(context.Context) error { installs++; return nil }}, nil
	})
	a.findUpdate()
	settle(t, a, posted)
	if a.updateReady != "0.2.0" || installs != 1 {
		t.Fatalf("ready %q after %d installs", a.updateReady, installs)
	}
	item := a.updateMenuItem()
	if item.Label != locales.MenuRestartToUpdate() || item.Subtitle != locales.MenuUpdateReady("0.2.0") {
		t.Errorf("menu offers %q (%q)", item.Label, item.Subtitle)
	}
	relaunched := 0
	old := relaunchApp
	relaunchApp = func() { relaunched++ }
	t.Cleanup(func() { relaunchApp = old })
	item.Click(item, nil)
	if relaunched != 1 {
		t.Errorf("relaunched %d times", relaunched)
	}
	a.findUpdate()
	a.keepLookingForUpdates()
	if a.updateBusy || installs != 1 {
		t.Error("looked again with an update waiting for a restart")
	}
}

func TestFailedInstallLooksAgainLater(t *testing.T) {
	fail := true
	a, posted := standInForUpdates(t, func(context.Context) (*newerVersion, error) {
		return &newerVersion{"0.2.0", func(context.Context) error {
			if fail {
				return errors.New("disk full")
			}
			return nil
		}}, nil
	})
	a.findUpdate()
	settle(t, a, posted)
	if a.updateReady != "" {
		t.Fatalf("a failed install waits as %q", a.updateReady)
	}
	fail = false
	a.findUpdate()
	settle(t, a, posted)
	if a.updateReady != "0.2.0" {
		t.Errorf("the next look left %q ready", a.updateReady)
	}
}

func TestFailedCheckChangesNothing(t *testing.T) {
	a, posted := standInForUpdates(t, func(context.Context) (*newerVersion, error) {
		return nil, errors.New("offline")
	})
	a.findUpdate()
	settle(t, a, posted)
	if a.updateReady != "" {
		t.Errorf("an offline check left %q ready", a.updateReady)
	}
}

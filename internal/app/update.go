package app

import (
	"context"
	"log"
	"time"

	"displayext/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
)

var (
	firstUpdateLook = 5 * time.Second
	updateEvery     = 4 * time.Hour
	updateTimeout   = 15 * time.Minute
)

type newerVersion struct {
	version string
	install func(context.Context) error
}

var lookForUpdate = func(ctx context.Context) (*newerVersion, error) {
	up, err := mygo.Updater.Check(ctx)
	if err != nil || up == nil {
		return nil, err
	}
	return &newerVersion{up.Version, func(ctx context.Context) error { return up.Install(ctx, nil) }}, nil
}

var relaunchApp = func() { mygo.App.Relaunch() }

func (a *App) startUpdates() {
	a.updatable = mygo.Updater.Enabled()
	updater.OnInstalled(func() {
		if v := updater.AvailableVersion(); v != "" {
			a.updateInstalled(v)
		}
	})
	a.later(firstUpdateLook, a.keepLookingForUpdates)
}

func (a *App) later(d time.Duration, fn func()) {
	time.AfterFunc(d, func() { a.post(fn) })
}

func (a *App) keepLookingForUpdates() {
	if !a.updatable || a.updateReady != "" {
		return
	}
	a.findUpdate()
	a.later(updateEvery, a.keepLookingForUpdates)
}

func (a *App) findUpdate() {
	if a.updateBusy || a.updateReady != "" {
		return
	}
	a.updateBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
		defer cancel()
		up, err := lookForUpdate(ctx)
		if err != nil {
			log.Printf("update: %v", err)
		}
		installed := ""
		if err == nil && up != nil {
			if err := up.install(ctx); err != nil {
				log.Printf("update %s: %v", up.version, err)
			} else {
				log.Printf("update %s installed; it runs after a restart", up.version)
				installed = up.version
			}
		}
		a.post(func() {
			a.updateBusy = false
			if installed != "" {
				a.updateInstalled(installed)
			}
		})
	}()
}

func (a *App) updateInstalled(version string) {
	if a.updateReady == version {
		return
	}
	a.updateReady = version
	a.changed()
}

func (a *App) updateMenuItem() *mygo.MenuItem {
	if a.updateReady != "" {
		return &mygo.MenuItem{
			Label:    locales.MenuRestartToUpdate(),
			Subtitle: locales.MenuUpdateReady(a.updateReady),
			Click:    func(*mygo.MenuItem, *mygo.Window) { relaunchApp() },
		}
	}
	return updater.MenuItem()
}

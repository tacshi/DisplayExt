package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"displayext/internal/autoswitch"
	"displayext/internal/config"
	"displayext/internal/manager"
	"displayext/internal/uucapture"
	"displayext/internal/uudetect"
	"displayext/internal/uuwindow"
	"displayext/internal/vdisplay"
	"displayext/locales"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

type App struct {
	name        string
	cfgPath     string
	cfg         config.Config
	firstRun    bool
	configKept  string
	unavailable string

	mgr      *manager.Manager
	sw       *autoswitch.Switcher
	uu       uudetect.Status
	uuScreen string
	devices  *uuwindow.Watcher
	trusted  bool

	tray     *mygo.Tray
	win      *mygo.Window
	settings *Settings
	quitting bool
	active   bool
	stopUU   context.CancelFunc

	post        func(func())
	updatable   bool
	updateBusy  bool
	updateReady string
}

func Run() error {
	name := appName(mygo.App.Name())
	mygo.App.SetName(name)
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	logw := openLog(name)
	log.SetOutput(logw)
	log.Printf("DisplayExt starting (pid %d)", os.Getpid())

	a := &App{name: name, sw: autoswitch.New(), post: mygo.RunOnMain}
	if err := a.loadConfig(); err != nil {
		return err
	}
	if err := vdisplay.Available(); err != nil {
		a.unavailable = err.Error()
		log.Println("virtual displays unavailable:", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	state := filepath.Join(filepath.Dir(a.cfgPath), "screens-off.json")
	manager.Recover(state)
	if err := manager.Watch(exe, state, LogPath(name)); err != nil {
		log.Printf("screens: could not start the watcher: %v", err)
	}
	a.mgr = manager.New(exe, logw, state, func(string) { mygo.RunOnMain(a.changed) })

	mygo.Use(native.New(updater.Options{DisableAutomaticChecks: true}))
	mygo.App.OnSecondInstance(func([]string, string) { a.showSettings() })
	mygo.App.OnLocaleChanged(func(string) { a.changed() })
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		if a.stopUU != nil {
			a.stopUU()
		}
		a.mgr.StopAll()
	})
	mygo.App.WhenReady(a.ready)
	return mygo.App.Run()
}

func (a *App) loadConfig() error {
	path, err := config.Path(a.name)
	if err != nil {
		return err
	}
	a.cfgPath = path
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		a.firstRun = true
	}
	cfg, err := config.Load(path)
	if errors.Is(err, config.ErrDamaged) {
		kept, moveErr := config.SetAside(path)
		if moveErr != nil {
			return fmt.Errorf("%w; could not move it aside: %w", err, moveErr)
		}
		log.Printf("%v; kept it as %s and started over", err, kept)
		cfg, err = config.Default(), nil
		a.configKept, a.firstRun = kept, true
	}
	if err != nil {
		return err
	}
	a.cfg = cfg
	if a.firstRun {
		return config.Save(path, cfg)
	}
	return nil
}

func (a *App) ready() {
	var err error
	a.tray, err = mygo.NewTray(mygo.TrayOptions{
		ID:             "displayext",
		Icon:           trayIcon(false),
		IconIsTemplate: true,
		ToolTip:        a.name,
	})
	if err != nil {
		log.Fatal("tray: ", err)
	}
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) { a.tray.Destroy() })

	a.settings = NewSettings(a)
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         a.name,
		Width:         820,
		Height:        600,
		MinWidth:      720,
		MinHeight:     520,
		Hidden:        true,
		StateKey:      "settings",
		TitleBarStyle: mygo.TitleBarHidden,
		Content:       ui.View(a.settings.View),
	})
	a.win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			a.win.Hide()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	a.stopUU = cancel
	rule := uudetect.Rule{
		Process:   a.cfg.UU.Process,
		Assertion: a.cfg.UU.Assertion,
		Grace:     time.Duration(a.cfg.UU.GraceSeconds) * time.Second,
	}
	capture := uucapture.NewWatcher(func() int32 {
		snap, err := uudetect.TakeSnapshot(rule.Process)
		if err != nil {
			return 0
		}
		return snap.Server()
	}, log.Printf)
	go capture.Run(ctx)
	a.devices = uuwindow.NewWatcher(func() int32 {
		snap, err := uudetect.TakeSnapshot(uuwindow.Process)
		if err != nil {
			return 0
		}
		return snap.Server()
	})
	go a.devices.Run(ctx, 1500*time.Millisecond, func(r uuwindow.Reading) { mygo.RunOnMain(func() { a.deviceRead(r) }) })
	go uudetect.Watch(ctx, rule, 2*time.Second, capture,
		func(st uudetect.Status) { mygo.RunOnMain(func() { a.uuChanged(st) }) },
		func(err error) { log.Println("uudetect:", err) })

	a.startUpdates()
	a.apply()
	if a.configKept != "" || (a.firstRun && !mygo.App.WasOpenedAtLogin()) {
		a.showSettings()
	}
}

func (a *App) apply() {
	if a.unavailable == "" {
		a.mgr.Sync(a.wanted())
	}
	a.changed()
}

func (a *App) wanted() []config.Display {
	var want []config.Display
	for _, d := range a.cfg.Displays {
		if d.Check() == config.NoProblem && a.sw.Want(d) {
			d.ScreensOff = d.ScreensOff && a.sw.Connected()
			want = append(want, d)
		}
	}
	return want
}

func (a *App) changed() {
	if a.tray == nil {
		return
	}
	a.tray.SetMenu(a.menu())
	active := slices.ContainsFunc(a.cfg.Displays, func(d config.Display) bool { return a.mgr.Status(d.ID).Mounted })
	if active != a.active {
		if err := a.tray.SetIcon(trayIcon(active), true); err != nil {
			log.Println("tray icon:", err)
		}
		a.active = active
	}
	if a.win != nil && a.win.IsVisible() {
		a.settings.Refresh()
		a.win.Invalidate()
	}
}

func (a *App) uuChanged(st uudetect.Status) {
	a.uu = st
	a.uuScreen = a.screenName(st.Showing)
	if st.Showing != 0 {
		log.Printf("UU远程: %s, showing display %d (%s)", st.State, st.Showing, a.uuScreen)
	} else {
		log.Println("UU远程:", st.State)
	}
	connected := st.State == uudetect.Connected
	a.devices.SetActive(connected)
	if a.sw.SetConnected(connected) {
		if connected {
			a.waitForDevice()
		}
		a.apply()
		return
	}
	a.changed()
}

const deviceWait = 3 * time.Second

func (a *App) waitForDevice() {
	if !a.trusted {
		a.sw.SetBlind(true)
		return
	}
	time.AfterFunc(deviceWait, func() {
		mygo.RunOnMain(func() {
			if a.sw.Connected() && a.sw.Device() == "" && a.sw.SetBlind(true) {
				log.Printf("UU远程: could not tell which device connected within %s; mounting every display that follows UU远程", deviceWait)
				a.apply()
			}
		})
	})
}

func (a *App) deviceRead(r uuwindow.Reading) {
	if r.Trusted != a.trusted {
		log.Printf("Accessibility allowed: %v", r.Trusted)
	}
	a.trusted = r.Trusted
	if r.Err != "" {
		log.Println("UU远程 window:", r.Err)
	}
	changed := false
	if !r.Trusted {
		changed = a.sw.SetBlind(true)
	}
	if r.Device != "" {
		if a.cfg.SawDevice(r.Device) {
			_ = a.saveConfig()
		}
		if a.sw.SetDevice(r.Device) {
			log.Printf("UU远程: connected from %s", r.Device)
			changed = true
		}
	}
	if changed {
		a.apply()
		return
	}
	a.changed()
}

func (a *App) showSettings() {
	a.settings.Refresh()
	a.win.Show()
	a.win.Focus()
}

func (a *App) saveConfig() error {
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		log.Println("saving config:", err)
		return err
	}
	return nil
}

func (a *App) menu() *mygo.Menu {
	items := []*mygo.MenuItem{
		{Label: locales.MenuUUStatus(uuLabel(a.uu, a.sw.Device(), a.uuScreen)), Disabled: true},
		mygo.Separator(),
	}
	if a.unavailable != "" {
		items = append(items, &mygo.MenuItem{Label: locales.MenuUnavailable(), Disabled: true, ToolTip: a.unavailable})
	}
	for _, d := range a.cfg.Displays {
		st := a.mgr.Status(d.ID)
		want := a.sw.Want(d)
		item := &mygo.MenuItem{
			ID:       d.ID,
			Label:    d.Label(),
			Type:     mygo.MenuItemCheckbox,
			Checked:  want,
			Disabled: st.Busy || a.unavailable != "" || (!want && d.Check() != config.NoProblem),
			Subtitle: subtitle(d, st),
			Click: func(*mygo.MenuItem, *mygo.Window) {
				a.SetMounted(d.ID, !a.Wanted(d.ID))
			},
		}
		items = append(items, item)
	}
	if len(a.cfg.Displays) == 0 {
		items = append(items, &mygo.MenuItem{Label: locales.MenuNoDisplays(), Disabled: true})
	}
	items = append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: locales.MenuSettings(), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.showSettings() }},
		&mygo.MenuItem{
			Label:   locales.MenuOpenAtLogin(),
			Type:    mygo.MenuItemCheckbox,
			Checked: mygo.App.OpenAtLogin(),
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				if err := a.SetOpenAtLogin(it.IsChecked()); err != nil {
					log.Println("open at login:", err)
				}
				a.changed()
			},
		},
		mygo.Separator(),
		a.updateMenuItem(),
		&mygo.MenuItem{Role: mygo.RoleQuit, Label: locales.MenuQuit(a.name)},
	)
	return mygo.NewMenu(items)
}

func subtitle(d config.Display, st manager.Status) string {
	switch {
	case d.Check() != config.NoProblem:
		return locales.MenuError(problemText(d))
	case st.Busy:
		return locales.Working()
	case st.Err != "":
		return locales.MenuError(st.Err)
	case d.Trigger == config.TriggerUU && d.ConnectFrom != "":
		return locales.MenuFollowsDevice(d.ConnectFrom)
	case d.Trigger == config.TriggerUU:
		return locales.MenuFollowsUU()
	}
	return ""
}

func (a *App) Displays() []config.Display      { return a.cfg.Displays }
func (a *App) Status(id string) manager.Status { return a.mgr.Status(id) }
func (a *App) SettingsReset() string           { return a.configKept }
func (a *App) UUStatus() uudetect.Status       { return a.uu }
func (a *App) UUScreen() string                { return a.uuScreen }
func (a *App) UUDevice() string                { return a.sw.Device() }
func (a *App) Devices() []string               { return a.cfg.Devices }
func (a *App) AccessibilityAllowed() bool      { return a.trusted }

func (a *App) AllowAccessibility() {
	if !uuwindow.AskForTrust() {
		if err := mygo.Shell.OpenExternal(accessibilitySettings); err != nil {
			log.Println("opening Accessibility settings:", err)
		}
	}
	a.devices.Wake()
}

const accessibilitySettings = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"

func (a *App) Unavailable() string          { return a.unavailable }
func (a *App) Version() string              { return mygo.App.Version() }
func (a *App) LogFile() string              { return LogPath(a.name) }
func (a *App) RevealLog()                   { mygo.Shell.ShowItemInFolder(LogPath(a.name)) }
func (a *App) Language() string             { return mygo.App.ChosenLocale() }
func (a *App) SetLanguage(tag string)       { mygo.App.SetLocale(tag) }
func (a *App) OpenAtLogin() bool            { return mygo.App.OpenAtLogin() }
func (a *App) SetOpenAtLogin(on bool) error { return mygo.App.SetOpenAtLogin(on) }

func (a *App) screenName(id uint32) string {
	if id == 0 {
		return ""
	}
	for _, d := range a.cfg.Displays {
		if a.mgr.Status(d.ID).DisplayID == id {
			return d.Name
		}
	}
	if name := vdisplay.ScreenName(id); name != "" {
		return name
	}
	return locales.ScreenNumber(int(id))
}

func (a *App) Wanted(id string) bool {
	d, ok := a.cfg.Display(id)
	return ok && a.sw.Want(*d)
}

func (a *App) Save(d config.Display) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if old, ok := a.cfg.Display(d.ID); ok {
		if old.Trigger != d.Trigger || old.ConnectFrom != d.ConnectFrom {
			a.sw.Forget(d.ID)
		}
		d.Mounted = old.Mounted
		*old = d
	} else {
		a.cfg.Displays = append(a.cfg.Displays, d)
	}
	if err := a.saveConfig(); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	a.apply()
	return nil
}

func (a *App) Delete(id string) {
	a.cfg.Displays = slices.DeleteFunc(a.cfg.Displays, func(d config.Display) bool { return d.ID == id })
	a.sw.Forget(id)
	_ = a.saveConfig()
	a.apply()
}

func (a *App) SetMounted(id string, mounted bool) {
	d, ok := a.cfg.Display(id)
	if !ok {
		return
	}
	if d.Trigger == config.TriggerManual {
		d.Mounted = mounted
		_ = a.saveConfig()
	} else {
		a.sw.Toggle(*d, mounted)
	}
	a.apply()
}

func LogPath(appName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Logs", appName, appName+".log")
}

func openLog(appName string) io.Writer {
	path := LogPath(appName)
	if path == "" || os.MkdirAll(filepath.Dir(path), 0o750) != nil {
		return os.Stderr
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > 1<<20 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(filepath.Clean(path), flags, 0o600)
	if err != nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, f)
}

func appName(bundle string) string {
	if strings.HasPrefix(bundle, "DisplayExt") {
		return bundle
	}
	return "DisplayExt"
}

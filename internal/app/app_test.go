package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"displayext/internal/autoswitch"
	"displayext/internal/config"
	"displayext/internal/manager"
)

func TestAppName(t *testing.T) {
	cases := map[string]string{
		"DisplayExt":       "DisplayExt",
		"DisplayExt Debug": "DisplayExt Debug",
		"displayext":       "DisplayExt",
		"main":             "DisplayExt",
		"":                 "DisplayExt",
	}
	for bundle, want := range cases {
		if got := appName(bundle); got != want {
			t.Errorf("appName(%q) = %q, want %q", bundle, got, want)
		}
	}
}

func testApp(t *testing.T, ds ...config.Display) *App {
	t.Helper()
	m := manager.New("", io.Discard, "", nil)
	t.Cleanup(m.StopAll)
	return &App{
		cfgPath: filepath.Join(t.TempDir(), "config.json"),
		cfg:     config.Config{Displays: ds, UU: config.DefaultUURule()},
		sw:      autoswitch.New(),
		mgr:     m,
	}
}

func TestSaveKeepsMountedState(t *testing.T) {
	d := config.NewDisplay("Manual", config.Presets[0])
	a := testApp(t, d)
	a.SetMounted(d.ID, true)

	edited := d
	edited.RefreshHz = 120
	if err := a.Save(edited); err != nil {
		t.Fatal(err)
	}
	got := a.cfg.Displays[0]
	if !got.Mounted || got.RefreshHz != 120 {
		t.Errorf("after saving an edit made before mounting: %+v", got)
	}
	if !a.Wanted(d.ID) {
		t.Error("the display is no longer wanted after the save")
	}
}

func TestWantedSkipsInvalidDisplays(t *testing.T) {
	good := config.NewDisplay("Good", config.Presets[0])
	bad := config.NewDisplay("Bad", config.Presets[0])
	good.Mounted, bad.Mounted, bad.Width = true, true, 100
	a := testApp(t, good, bad)
	want := a.wanted()
	if len(want) != 1 || want[0].ID != good.ID {
		t.Errorf("wanted %+v, want only %q", want, good.Name)
	}
	if !a.Wanted(bad.ID) {
		t.Error("an invalid display lost its mounted setting")
	}
}

func TestLoadConfigSetsDamagedFileAside(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := &App{name: "DisplayExt Test"}
	path, err := config.Path(a.name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	damaged := []byte(`{"displays":[{"name":`)
	if err := os.WriteFile(path, damaged, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.loadConfig(); err != nil {
		t.Fatal(err)
	}
	if a.configKept == "" || !a.firstRun {
		t.Fatalf("damaged settings not reported: kept %q, first run %v", a.configKept, a.firstRun)
	}
	kept, err := os.ReadFile(filepath.Clean(a.configKept))
	if err != nil || string(kept) != string(damaged) {
		t.Errorf("the damaged file was not kept as it was: %q, %v", kept, err)
	}
	if _, err := config.Load(path); err != nil {
		t.Errorf("the fresh settings do not load: %v", err)
	}
	if len(a.cfg.Displays) != 1 || a.cfg.Displays[0].Name != "iPad" {
		t.Errorf("did not start over with the defaults: %+v", a.cfg)
	}
}

func TestLoadConfigKeepsOtherErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a := &App{name: "DisplayExt Test"}
	path, err := config.Path(a.name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := a.loadConfig(); err == nil || errors.Is(err, config.ErrDamaged) {
		t.Errorf("a settings path that cannot be read gave %v", err)
	}
	if a.configKept != "" {
		t.Error("moved aside a file that was not damaged")
	}
}

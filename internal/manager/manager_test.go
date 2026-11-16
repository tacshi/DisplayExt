package manager

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"displayext/internal/config"
	"displayext/internal/host"
	"displayext/internal/vdisplay"
)

const (
	fakeEnv    = "DISPLAYEXT_FAKE_HELPER"
	startsEnv  = "DISPLAYEXT_FAKE_STARTS"
	restoreEnv = "DISPLAYEXT_FAKE_RESTORE"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == host.Flag {
		os.Exit(fakeHelper())
	}
	if len(os.Args) > 1 && os.Args[1] == RestoreFlag {
		_ = os.WriteFile(filepath.Clean(os.Getenv(restoreEnv)), []byte(strings.Join(os.Args[1:], " ")), 0o600)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func record(what string) {
	if path := os.Getenv(startsEnv); path != "" {
		if f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, _ = fmt.Fprintln(f, what)
			_ = f.Close()
		}
	}
}

func fakeHelper() int {
	record("start " + strings.Join(os.Args[3:], " "))
	mode := os.Getenv(fakeEnv)
	if len(os.Args) > 3 {
		fmt.Printf(`{"displayID":42,"screensOff":%s}`+"\n", os.Args[3])
	} else if mode != "fail" {
		fmt.Println(`{"displayID":42}`)
	}
	switch mode {
	case "fail":
		fmt.Println(`{"error":"nope"}`)
		return 1
	case "die":
		time.Sleep(200 * time.Millisecond)
		return 3
	}
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var c host.Command
		if json.Unmarshal(in.Bytes(), &c) != nil {
			continue
		}
		b, _ := json.Marshal(c.ScreensOff)
		record("screens " + string(b))
		switch {
		case mode == "screens-fail" && len(c.ScreensOff) > 0:
			fmt.Println(`{"screensError":"nope"}`)
		default:
			r, _ := json.Marshal(host.Ready{ScreensOff: c.ScreensOff})
			fmt.Println(string(r))
		}
	}
	if mode == "left-off" {
		return 1
	}
	return 0
}

func newManager(t *testing.T, mode string) *Manager {
	t.Helper()
	t.Setenv(fakeEnv, mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := New(exe, io.Discard, filepath.Join(t.TempDir(), "screens-off.json"), nil)
	m.modes, m.setMode = nil, nil
	m.others, m.active, m.turnOn = nil, nil, nil
	t.Cleanup(m.StopAll)
	return m
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func running(m *Manager) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.running)
}

func TestMountUnmount(t *testing.T) {
	m := newManager(t, "ok")
	d := config.Default().Displays[0]

	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	if st := m.Status(d.ID); st.DisplayID != 42 || st.Err != "" || st.Busy {
		t.Fatalf("status after mount: %+v", st)
	}

	m.mu.Lock()
	h := m.running[d.ID]
	m.mu.Unlock()

	m.Sync(nil)
	waitFor(t, "unmount", func() bool { return !m.Status(d.ID).Mounted && running(m) == 0 })
	select {
	case <-h.done:
	case <-time.After(5 * time.Second):
		t.Fatal("helper still running after unmount")
	}
	if st := m.Status(d.ID); st.Err != "" {
		t.Errorf("deliberate unmount reported an error: %+v", st)
	}
}

func TestRedefineRestarts(t *testing.T) {
	m := newManager(t, "ok")
	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	m.mu.Lock()
	first := m.running[d.ID]
	m.mu.Unlock()

	d.RefreshHz = 144
	m.Sync([]config.Display{d})
	waitFor(t, "restart", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		h := m.running[d.ID]
		return h != nil && h != first && strings.Contains(h.spec, `"refreshHz":144`)
	})
	select {
	case <-first.done:
	case <-time.After(5 * time.Second):
		t.Fatal("old helper still running after the redefinition")
	}

	m.mu.Lock()
	second := m.running[d.ID]
	m.mu.Unlock()
	m.Sync([]config.Display{d})
	time.Sleep(200 * time.Millisecond)
	m.mu.Lock()
	same := m.running[d.ID] == second
	m.mu.Unlock()
	if !same {
		t.Error("an unchanged definition restarted the helper")
	}
}

func TestCreateFailure(t *testing.T) {
	m := newManager(t, "fail")
	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "failure", func() bool { return m.Status(d.ID).Err != "" })
	if st := m.Status(d.ID); st.Err != "nope" || st.Mounted {
		t.Errorf("status %+v", st)
	}
	if running(m) != 0 {
		t.Error("a failed helper is counted as running")
	}
}

func TestHelperDies(t *testing.T) {
	m := newManager(t, "die")
	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "death reported", func() bool { return strings.Contains(m.Status(d.ID).Err, "unexpectedly") })
	if m.Status(d.ID).Mounted || running(m) != 0 {
		t.Errorf("dead helper still counted: %+v", m.Status(d.ID))
	}
}

func TestStopAll(t *testing.T) {
	m := newManager(t, "ok")
	a := config.Default().Displays[0]
	b := config.NewDisplay("Second", config.Presets[2])
	m.Sync([]config.Display{a, b})
	waitFor(t, "both mounted", func() bool { return m.Status(a.ID).Mounted && m.Status(b.ID).Mounted })
	m.mu.Lock()
	hs := []*helper{m.running[a.ID], m.running[b.ID]}
	m.mu.Unlock()
	m.StopAll()
	for _, h := range hs {
		select {
		case <-h.done:
		default:
			t.Error("StopAll returned with a helper still running")
		}
	}
}

func idle(m *Manager) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.dirty
}

func TestFailureWaitsForAnotherTry(t *testing.T) {
	starts := filepath.Join(t.TempDir(), "starts")
	t.Setenv(startsEnv, starts)
	count := func() int {
		b, _ := os.ReadFile(filepath.Clean(starts))
		return strings.Count(string(b), "start")
	}
	m := newManager(t, "fail")
	d := config.Default().Displays[0]
	other := config.NewDisplay("Other", config.Presets[2])

	m.Sync([]config.Display{d})
	waitFor(t, "failure", func() bool { return m.Status(d.ID).Err != "" })

	t.Setenv(fakeEnv, "ok")
	m.Sync([]config.Display{d, other})
	waitFor(t, "other mounted", func() bool { return m.Status(other.ID).Mounted })
	if n := count(); n != 2 {
		t.Errorf("%d helpers started, want 2: the failed display was tried again unasked", n)
	}
	if st := m.Status(d.ID); st.Err != "nope" {
		t.Errorf("the failure was forgotten: %+v", st)
	}

	m.Sync([]config.Display{other})
	waitFor(t, "failure cleared", func() bool { return idle(m) && m.Status(d.ID) == Status{} })

	m.Sync([]config.Display{d, other})
	waitFor(t, "second try mounted", func() bool { return m.Status(d.ID).Mounted })
	if n := count(); n != 3 {
		t.Errorf("%d helpers started, want 3", n)
	}
}

func TestEditRetriesFailure(t *testing.T) {
	m := newManager(t, "fail")
	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "failure", func() bool { return m.Status(d.ID).Err != "" })

	t.Setenv(fakeEnv, "ok")
	d.RefreshHz = 120
	m.Sync([]config.Display{d})
	waitFor(t, "edited display mounted", func() bool { return m.Status(d.ID).Mounted })
}

func TestUnwantedFailureIsCleared(t *testing.T) {
	m := newManager(t, "fail")
	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "failure", func() bool { return m.Status(d.ID).Err != "" })
	m.Sync(nil)
	waitFor(t, "failure cleared", func() bool { return idle(m) && m.Status(d.ID) == Status{} })
}

func TestOtherScreensKeepTheirMode(t *testing.T) {
	m := newManager(t, "ok")
	sharp := vdisplay.Mode{PointsW: 2560, PointsH: 1440, PixelsW: 5120, PixelsH: 2880, RefreshHz: 60}
	blurry := vdisplay.Mode{PointsW: 1920, PointsH: 1080, PixelsW: 3840, PixelsH: 2160, RefreshHz: 60}

	var mu sync.Mutex
	cur, armed, restored := sharp, true, 0
	m.modes = func() map[uint32]vdisplay.Mode {
		mu.Lock()
		defer mu.Unlock()
		out := map[uint32]vdisplay.Mode{1: cur}
		if armed {
			cur, armed = blurry, false
		}
		return out
	}
	m.setMode = func(id uint32, want vdisplay.Mode) error {
		mu.Lock()
		defer mu.Unlock()
		if id != 1 {
			t.Errorf("restored screen %d, want 1", id)
		}
		cur = want
		restored++
		return nil
	}
	state := func() (vdisplay.Mode, int) {
		mu.Lock()
		defer mu.Unlock()
		return cur, restored
	}

	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "restored after mount", func() bool { c, n := state(); return n == 1 && c.Same(sharp) })

	mu.Lock()
	armed = true
	mu.Unlock()
	m.Sync(nil)
	waitFor(t, "restored after unmount", func() bool { c, n := state(); return n == 2 && c.Same(sharp) })
}

type fakeScreens struct {
	mu     sync.Mutex
	dark   bool
	broken bool
	turned [][]uint32
}

func (f *fakeScreens) install(m *Manager) {
	m.others = func() []uint32 { return []uint32{7, 9} }
	m.active = func() []uint32 {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.dark {
			return []uint32{42}
		}
		return []uint32{7, 9, 42}
	}
	m.turnOn = func(ids []uint32) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.turned = append(f.turned, ids)
		if f.broken {
			return fmt.Errorf("CGCompleteDisplayConfiguration: CGError 1001")
		}
		f.dark = false
		return nil
	}
}

func (f *fakeScreens) calls() [][]uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.turned)
}

func stateIDs(t *testing.T, m *Manager) []uint32 {
	t.Helper()
	b, err := os.ReadFile(m.state)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var st screensState
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	if st.PID != os.Getpid() {
		t.Errorf("state file names pid %d, want %d", st.PID, os.Getpid())
	}
	return st.Screens
}

func TestScreensOffPassedAndRecorded(t *testing.T) {
	starts := filepath.Join(t.TempDir(), "starts")
	t.Setenv(startsEnv, starts)
	m := newManager(t, "ok")
	f := &fakeScreens{}
	f.install(m)
	d := config.Default().Displays[0]
	d.ScreensOff = true

	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	b, _ := os.ReadFile(filepath.Clean(starts))
	if !strings.Contains(string(b), "[7,9]") {
		t.Errorf("helper was started with %q, want the screens [7,9]", b)
	}
	if got := stateIDs(t, m); !slices.Equal(got, []uint32{7, 9}) {
		t.Errorf("state file holds %v while mounted", got)
	}

	m.Sync(nil)
	waitFor(t, "unmount", func() bool { return running(m) == 0 && idle(m) })
	if got := stateIDs(t, m); got != nil {
		t.Errorf("state file still holds %v after unmount", got)
	}
	if c := f.calls(); len(c) != 0 {
		t.Errorf("screens the helper had turned back on were turned on again: %v", c)
	}
}

func TestScreensOffNotAskedFor(t *testing.T) {
	starts := filepath.Join(t.TempDir(), "starts")
	t.Setenv(startsEnv, starts)
	m := newManager(t, "ok")
	(&fakeScreens{}).install(m)
	d := config.Default().Displays[0]

	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	b, _ := os.ReadFile(filepath.Clean(starts))
	if strings.Contains(string(b), "[") {
		t.Errorf("helper was given screens to turn off: %q", b)
	}
	if got := stateIDs(t, m); got != nil {
		t.Errorf("state file holds %v", got)
	}
}

func TestScreensBackOnWhenHelperDies(t *testing.T) {
	m := newManager(t, "die")
	f := &fakeScreens{dark: true}
	f.install(m)
	d := config.Default().Displays[0]
	d.ScreensOff = true

	m.Sync([]config.Display{d})
	waitFor(t, "death reported", func() bool { return strings.Contains(m.Status(d.ID).Err, "unexpectedly") })
	if c := f.calls(); len(c) != 1 || !slices.Equal(c[0], []uint32{7, 9}) {
		t.Errorf("turned on %v, want [[7 9]]", c)
	}
	if got := stateIDs(t, m); got != nil {
		t.Errorf("state file still holds %v", got)
	}
}

func TestQuitTrustsTheHelper(t *testing.T) {
	m := newManager(t, "ok")
	f := &fakeScreens{}
	f.install(m)
	d := config.Default().Displays[0]
	d.ScreensOff = true

	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	f.mu.Lock()
	f.dark = true
	f.mu.Unlock()
	m.StopAll()
	if c := f.calls(); len(c) != 0 {
		t.Errorf("screens the helper had turned back on were turned on again: %v", c)
	}
	if got := stateIDs(t, m); got != nil {
		t.Errorf("state file still holds %v after quitting", got)
	}
}

func TestScreensBackOnWhenHelperCannot(t *testing.T) {
	m := newManager(t, "left-off")
	f := &fakeScreens{}
	f.install(m)
	d := config.Default().Displays[0]
	d.ScreensOff = true

	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	f.mu.Lock()
	f.dark = true
	f.mu.Unlock()
	m.StopAll()
	if c := f.calls(); len(c) != 1 || !slices.Equal(c[0], []uint32{7, 9}) {
		t.Errorf("turned on %v, want [[7 9]]", c)
	}
	if got := stateIDs(t, m); got != nil {
		t.Errorf("state file still holds %v", got)
	}
}

func TestStateKeptWhenScreensStayOff(t *testing.T) {
	m := newManager(t, "die")
	f := &fakeScreens{dark: true, broken: true}
	f.install(m)
	d := config.Default().Displays[0]
	d.ScreensOff = true

	m.Sync([]config.Display{d})
	waitFor(t, "death reported", func() bool { return strings.Contains(m.Status(d.ID).Err, "unexpectedly") })
	if c := f.calls(); len(c) != 1 {
		t.Errorf("turned on %v, want one try", c)
	}
	if got := stateIDs(t, m); !slices.Equal(got, []uint32{7, 9}) {
		t.Errorf("state file holds %v, want [7 9] left for the watcher", got)
	}
}

func TestScreensBackOnWhenHelperFails(t *testing.T) {
	m := newManager(t, "fail")
	f := &fakeScreens{dark: true}
	f.install(m)
	d := config.Default().Displays[0]
	d.ScreensOff = true

	m.Sync([]config.Display{d})
	waitFor(t, "failure", func() bool { return m.Status(d.ID).Err != "" })
	if c := f.calls(); len(c) != 1 {
		t.Errorf("turned on %v, want one call", c)
	}
}

func current(m *Manager, id string) *helper {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running[id]
}

func TestScreensFollowTheSession(t *testing.T) {
	starts := filepath.Join(t.TempDir(), "starts")
	t.Setenv(startsEnv, starts)
	m := newManager(t, "ok")
	f := &fakeScreens{}
	f.install(m)
	d := config.Default().Displays[0]

	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	h := current(m, d.ID)

	d.ScreensOff = true
	m.Sync([]config.Display{d})
	waitFor(t, "screens off", func() bool { return idle(m) && slices.Equal(stateIDs(t, m), []uint32{7, 9}) })
	waitFor(t, "helper told", func() bool {
		b, _ := os.ReadFile(filepath.Clean(starts))
		return strings.Contains(string(b), "screens [7,9]")
	})

	d.ScreensOff = false
	m.Sync([]config.Display{d})
	waitFor(t, "screens on", func() bool {
		b, _ := os.ReadFile(filepath.Clean(starts))
		return idle(m) && strings.Contains(string(b), "screens []") && stateIDs(t, m) == nil
	})
	if current(m, d.ID) != h {
		t.Error("the display was remounted to change its screens")
	}
	if st := m.Status(d.ID); !st.Mounted || st.ScreensErr != "" {
		t.Errorf("status %+v", st)
	}
	if c := f.calls(); len(c) != 0 {
		t.Errorf("the app turned screens on itself: %v", c)
	}
}

func TestScreensOffFailureReported(t *testing.T) {
	m := newManager(t, "screens-fail")
	f := &fakeScreens{}
	f.install(m)
	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })

	d.ScreensOff = true
	m.Sync([]config.Display{d})
	waitFor(t, "error reported", func() bool { return m.Status(d.ID).ScreensErr == "nope" })
	if got := stateIDs(t, m); got != nil {
		t.Errorf("state file holds %v after the helper failed", got)
	}
	m.mu.Lock()
	dark := len(m.running[d.ID].off) > 0
	m.mu.Unlock()
	if dark {
		t.Error("screens counted as off after the helper failed")
	}
}

func TestScreensRestoredWhenHelperDiesWhileOff(t *testing.T) {
	m := newManager(t, "ok")
	f := &fakeScreens{}
	f.install(m)
	d := config.Default().Displays[0]
	m.Sync([]config.Display{d})
	waitFor(t, "mount", func() bool { return m.Status(d.ID).Mounted })
	d.ScreensOff = true
	m.Sync([]config.Display{d})
	waitFor(t, "screens off", func() bool { return idle(m) && slices.Equal(stateIDs(t, m), []uint32{7, 9}) })

	f.mu.Lock()
	f.dark = true
	f.mu.Unlock()
	_ = current(m, d.ID).cmd.Process.Kill()
	waitFor(t, "death reported", func() bool { return strings.Contains(m.Status(d.ID).Err, "unexpectedly") })
	if c := f.calls(); len(c) != 1 || !slices.Equal(c[0], []uint32{7, 9}) {
		t.Errorf("turned on %v, want [[7 9]]", c)
	}
}

func writeStateFile(t *testing.T, pid int, ids ...uint32) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "screens-off.json")
	b, _ := json.Marshal(screensState{PID: pid, Screens: ids})
	if err := os.WriteFile(state, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRecoverTurnsDarkScreensOn(t *testing.T) {
	state := writeStateFile(t, 123, 3, 7, 9)
	var got []uint32
	recoverWith(state, 0, func() []uint32 { return []uint32{3, 42} }, func(ids []uint32) error { got = ids; return nil })
	if !slices.Equal(got, []uint32{7, 9}) {
		t.Errorf("turned on %v, want [7 9]", got)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Error("state file kept after recovery")
	}

	called := false
	recoverWith(state, 0, func() []uint32 { return nil }, func([]uint32) error { called = true; return nil })
	if called {
		t.Error("recovered without a state file")
	}
}

func TestRestoreOnlyForItsOwner(t *testing.T) {
	state := writeStateFile(t, 500, 7)
	called := false
	recoverWith(state, 400, func() []uint32 { return nil }, func([]uint32) error { called = true; return nil })
	if called {
		t.Error("a watcher restored screens another run turned off")
	}
	if _, err := os.Stat(state); err != nil {
		t.Error("a watcher removed another run's state file")
	}
	recoverWith(state, 500, func() []uint32 { return nil }, func([]uint32) error { called = true; return nil })
	if !called {
		t.Error("the owner's watcher did not restore")
	}
}

func TestWatcherRestoresAfterExit(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	t.Setenv(restoreEnv, marker)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	owner := exec.Command("/bin/sleep", "1")
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	watch := exec.Command("/bin/sh", "-c", watchScript, strconv.Itoa(owner.Process.Pid), exe, "STATE", "LOG")
	if err := watch.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("watcher ran while its owner was alive")
	}
	_ = owner.Wait()
	_ = watch.Wait()
	b, err := os.ReadFile(filepath.Clean(marker))
	if err != nil {
		t.Fatal("watcher never ran the restore")
	}
	if want := RestoreFlag + " STATE " + strconv.Itoa(owner.Process.Pid) + " LOG"; string(b) != want {
		t.Errorf("watcher ran %q, want %q", b, want)
	}
}

package manager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"displayext/internal/config"
	"displayext/internal/host"
)

const (
	fakeEnv   = "DISPLAYEXT_FAKE_HELPER"
	startsEnv = "DISPLAYEXT_FAKE_STARTS"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == host.Flag {
		os.Exit(fakeHelper())
	}
	os.Exit(m.Run())
}

func fakeHelper() int {
	if path := os.Getenv(startsEnv); path != "" {
		if f, err := os.OpenFile(filepath.Clean(path), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			_, _ = fmt.Fprintln(f, "start")
			_ = f.Close()
		}
	}
	switch os.Getenv(fakeEnv) {
	case "fail":
		fmt.Println(`{"error":"nope"}`)
		return 1
	case "die":
		fmt.Println(`{"displayID":42}`)
		time.Sleep(200 * time.Millisecond)
		return 3
	default:
		fmt.Println(`{"displayID":42}`)
		_, _ = io.Copy(io.Discard, os.Stdin)
		return 0
	}
}

func newManager(t *testing.T, mode string) *Manager {
	t.Helper()
	t.Setenv(fakeEnv, mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m := New(exe, io.Discard, nil)
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

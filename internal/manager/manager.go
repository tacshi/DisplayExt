package manager

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"displayext/internal/config"
	"displayext/internal/host"
	"displayext/internal/vdisplay"
)

const (
	readyTimeout = 20 * time.Second
	stopTimeout  = 8 * time.Second
	settleTime   = 2 * time.Second
	settlePoll   = 200 * time.Millisecond
	maxRestores  = 2
)

type Status struct {
	Mounted   bool
	Busy      bool
	DisplayID uint32

	Err string

	ModeErr string

	ScreensErr string
}

type Manager struct {
	exe      string
	stderr   io.Writer
	state    string
	onChange func(id string)

	modes   func() map[uint32]vdisplay.Mode
	setMode func(id uint32, m vdisplay.Mode) error
	others  func() []uint32
	active  func() []uint32
	turnOn  func(ids []uint32) error

	mu      sync.Mutex
	status  map[string]Status
	running map[string]*helper
	failed  map[string]string
	off     map[string][]uint32
	pending []config.Display
	dirty   bool
	wake    chan struct{}
}

type helper struct {
	spec    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	replies chan string
	done    chan struct{}
	off     []uint32
	before  map[uint32]vdisplay.Mode

	stopping bool
}

func New(exe string, stderr io.Writer, state string, onChange func(id string)) *Manager {
	m := &Manager{
		exe:      exe,
		stderr:   stderr,
		state:    state,
		onChange: onChange,
		modes:    vdisplay.OtherModes,
		setMode:  vdisplay.SetMode,
		others:   vdisplay.OtherDisplays,
		active:   vdisplay.ActiveDisplays,
		turnOn:   turnOnForSession,
		status:   map[string]Status{},
		running:  map[string]*helper{},
		failed:   map[string]string{},
		off:      map[string][]uint32{},
		wake:     make(chan struct{}, 1),
	}
	go m.loop()
	return m
}

func (m *Manager) Sync(want []config.Display) {
	m.mu.Lock()
	m.pending = append([]config.Display(nil), want...)
	m.dirty = true
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) Status(id string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status[id]
}

func (m *Manager) StopAll() {
	m.mu.Lock()
	m.pending, m.dirty = nil, false
	hs := make(map[string]*helper, len(m.running))
	for id, h := range m.running {
		h.stopping = true
		hs[id] = h
	}
	m.mu.Unlock()
	if len(hs) == 0 {
		return
	}
	before := m.snapshot()
	for _, h := range hs {
		before = withEarlier(before, h.before)
	}
	var wg sync.WaitGroup
	for _, h := range hs {
		wg.Go(func() { stop(h) })
	}
	wg.Wait()
	m.keep(before)
}

func (m *Manager) loop() {
	for range m.wake {
		for {
			m.mu.Lock()
			if !m.dirty {
				m.mu.Unlock()
				break
			}
			want := m.pending
			m.dirty = false
			m.mu.Unlock()
			m.reconcile(want)
		}
	}
}

func (m *Manager) reconcile(want []config.Display) {
	wanted := make(map[string]bool, len(want))
	specs := make(map[string]string, len(want))
	screensOff := make(map[string]bool, len(want))
	for _, d := range want {
		screensOff[d.ID] = d.ScreensOff
		wanted[d.ID] = true
		b, err := json.Marshal(vdisplay.FromConfig(d))
		if err != nil {
			m.set(d.ID, Status{Err: err.Error()})
			continue
		}
		specs[d.ID] = string(b)
	}

	m.mu.Lock()
	var toStop, cleared []string
	for id, h := range m.running {
		if specs[id] != h.spec {
			toStop = append(toStop, id)
		}
	}
	for id, spec := range m.failed {
		if specs[id] != spec {
			delete(m.failed, id)
		}
	}
	for id := range m.status {
		if !wanted[id] && m.running[id] == nil {
			delete(m.status, id)
			cleared = append(cleared, id)
		}
	}
	m.mu.Unlock()
	for _, id := range cleared {
		m.notify(id)
	}
	for _, id := range toStop {
		m.stopOne(id)
	}

	for _, d := range want {
		spec, ok := specs[d.ID]
		if !ok {
			continue
		}
		m.mu.Lock()
		h, up := m.running[d.ID]
		failed := m.failed[d.ID] == spec
		dark := up && len(h.off) > 0
		m.mu.Unlock()
		switch {
		case !up && !failed:
			m.start(d.ID, spec, screensOff[d.ID])
		case up && screensOff[d.ID] && !dark:
			m.screensOff(d.ID, h)
		case up && !screensOff[d.ID] && dark:
			m.screensOn(d.ID, h)
		}
	}
}

func (m *Manager) screensOff(id string, h *helper) {
	if m.others == nil {
		return
	}
	off := m.others()
	if len(off) == 0 {
		return
	}
	m.markOff(id, off)
	log.Printf("display %s: will turn off screens %v", id, off)
	r, err := m.ask(h, off)
	if err == nil && r.ScreensError != "" {
		err = errors.New(r.ScreensError)
	}
	if err != nil {
		log.Printf("display %s: screens not turned off: %v", id, err)
		m.restore(id, off)
		m.setScreensErr(id, err.Error())
		return
	}
	m.mu.Lock()
	h.off = r.ScreensOff
	m.mu.Unlock()
	log.Printf("display %s: turned off screens %v", id, r.ScreensOff)
	m.setScreensErr(id, "")
}

func (m *Manager) screensOn(id string, h *helper) {
	m.mu.Lock()
	off := h.off
	m.mu.Unlock()
	before := withEarlier(m.snapshot(), h.before)
	r, err := m.ask(h, nil)
	m.mu.Lock()
	h.off = nil
	m.mu.Unlock()
	if err == nil && r.ScreensError != "" {
		err = errors.New(r.ScreensError)
	}
	if err != nil {
		log.Printf("display %s: the helper could not turn screens back on: %v", id, err)
		m.restore(id, off)
	} else {
		log.Printf("display %s: turned screens %v back on", id, off)
		m.forget(id)
	}
	m.keep(before)
	m.setScreensErr(id, "")
}

func (m *Manager) ask(h *helper, off []uint32) (host.Ready, error) {
	var r host.Ready
	b, err := json.Marshal(host.Command{ScreensOff: off})
	if err != nil {
		return r, err
	}
	for drained := false; !drained; {
		select {
		case _, ok := <-h.replies:
			drained = !ok
		default:
			drained = true
		}
	}
	if _, err := fmt.Fprintf(h.stdin, "%s\n", b); err != nil {
		return r, err
	}
	select {
	case line, ok := <-h.replies:
		if !ok {
			return r, errors.New("the helper exited")
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return r, fmt.Errorf("helper said %q: %w", strings.TrimSpace(line), err)
		}
		return r, nil
	case <-time.After(readyTimeout):
		return r, fmt.Errorf("the helper did not answer within %s", readyTimeout)
	}
}

func (m *Manager) setScreensErr(id, msg string) {
	m.mu.Lock()
	st, ok := m.status[id]
	if ok {
		st.ScreensErr = msg
		m.status[id] = st
	}
	m.mu.Unlock()
	if ok {
		m.notify(id)
	}
}

func (m *Manager) stopOne(id string) {
	m.mu.Lock()
	h := m.running[id]
	if h == nil {
		m.mu.Unlock()
		return
	}
	h.stopping = true
	st := m.status[id]
	st.Busy = true
	m.status[id] = st
	m.mu.Unlock()
	m.notify(id)

	before := withEarlier(m.snapshot(), h.before)
	stop(h)
	log.Printf("display %s: unmounted", id)
	m.keep(before)

	m.mu.Lock()
	if m.running[id] == h {
		delete(m.running, id)
	}
	delete(m.status, id)
	m.mu.Unlock()
	m.notify(id)
}

func stop(h *helper) {
	_ = h.stdin.Close()
	select {
	case <-h.done:
		return
	case <-time.After(stopTimeout):
	}
	_ = h.cmd.Process.Signal(syscall.SIGKILL)
	<-h.done
}

func (m *Manager) start(id, spec string, screensOff bool) {
	m.set(id, Status{Busy: true})

	log.Printf("display %s: starting helper", id)
	before := m.snapshot()
	args := []string{host.Flag, spec}
	var off []uint32
	if screensOff && m.others != nil {
		off = m.others()
	}
	if len(off) > 0 {
		b, err := json.Marshal(off)
		if err != nil {
			m.fail(id, spec, err.Error())
			return
		}
		args = append(args, string(b))
		m.markOff(id, off)
		log.Printf("display %s: will turn off screens %v", id, off)
	}
	cmd := exec.Command(m.exe, args...)
	cmd.Stderr = m.stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.fail(id, spec, err.Error())
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.fail(id, spec, err.Error())
		return
	}
	if err := cmd.Start(); err != nil {
		m.restore(id, off)
		m.fail(id, spec, err.Error())
		return
	}
	h := &helper{spec: spec, cmd: cmd, stdin: stdin, replies: make(chan string, 1), done: make(chan struct{}), off: off, before: before}

	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		if sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
		for sc.Scan() {
			select {
			case h.replies <- sc.Text():
			default:
			}
		}
		_, _ = io.Copy(io.Discard, stdout)
		close(h.replies)
		clean := cmd.Wait() == nil
		m.mu.Lock()
		left := m.off[id]
		m.mu.Unlock()
		if clean {
			m.forget(id)
		} else {
			m.restore(id, left)
		}
		close(h.done)
	}()

	var ready host.Ready
	select {
	case line, ok := <-lines:
		switch {
		case !ok:
			ready.Error = "helper exited before reporting"
		default:
			if err := json.Unmarshal([]byte(line), &ready); err != nil {
				ready.Error = fmt.Sprintf("helper said %q: %v", strings.TrimSpace(line), err)
			}
		}
	case <-time.After(readyTimeout):
		ready.Error = fmt.Sprintf("display did not come up within %s", readyTimeout)
	}
	if ready.Error != "" {
		log.Printf("display %s: %s", id, ready.Error)
		h.stopping = true
		stop(h)
		m.fail(id, spec, ready.Error)
		m.keep(before)
		return
	}
	log.Printf("display %s: mounted as display %d (pid %d)", id, ready.DisplayID, cmd.Process.Pid)
	if ready.ModeError != "" {
		log.Printf("display %s: mode not applied: %s", id, ready.ModeError)
	}
	switch {
	case ready.ScreensError != "":
		log.Printf("display %s: screens not turned off: %s", id, ready.ScreensError)
	case len(ready.ScreensOff) > 0:
		log.Printf("display %s: turned off screens %v", id, ready.ScreensOff)
	}

	m.mu.Lock()
	h.off = ready.ScreensOff
	m.running[id] = h
	m.status[id] = Status{Mounted: true, DisplayID: ready.DisplayID, ModeErr: ready.ModeError, ScreensErr: ready.ScreensError}
	m.mu.Unlock()
	if len(off) > 0 && len(ready.ScreensOff) == 0 {
		m.restore(id, off)
	}
	m.notify(id)
	m.keep(before)

	go func() {
		<-h.done
		m.mu.Lock()
		if h.stopping || m.running[id] != h {
			m.mu.Unlock()
			return
		}
		delete(m.running, id)
		log.Printf("display %s: helper exited unexpectedly", id)
		m.status[id] = Status{Err: "the display's helper process exited unexpectedly"}
		m.failed[id] = h.spec
		m.mu.Unlock()
		m.notify(id)
		if len(h.off) > 0 {
			m.keep(h.before)
		}
	}()
}

func (m *Manager) fail(id, spec, msg string) {
	m.mu.Lock()
	m.status[id] = Status{Err: msg}
	m.failed[id] = spec
	m.mu.Unlock()
	m.notify(id)
}

func (m *Manager) set(id string, st Status) {
	m.mu.Lock()
	m.status[id] = st
	m.mu.Unlock()
	m.notify(id)
}

func (m *Manager) notify(id string) {
	if m.onChange != nil {
		m.onChange(id)
	}
}

func (m *Manager) snapshot() map[uint32]vdisplay.Mode {
	if m.modes == nil {
		return nil
	}
	return m.modes()
}

func (m *Manager) keep(before map[uint32]vdisplay.Mode) {
	if len(before) == 0 || m.modes == nil || m.setMode == nil {
		return
	}
	tries := map[uint32]int{}
	for deadline := time.Now().Add(settleTime); ; {
		for id, cur := range m.modes() {
			want, ok := before[id]
			if !ok || cur.Same(want) || tries[id] >= maxRestores {
				continue
			}
			tries[id]++
			log.Printf("screen %d: macOS switched it to %s, restoring %s", id, cur, want)
			if err := m.setMode(id, want); err != nil {
				log.Printf("screen %d: could not restore its mode: %v", id, err)
			}
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(settlePoll)
	}
}

func withEarlier(now, earlier map[uint32]vdisplay.Mode) map[uint32]vdisplay.Mode {
	if len(earlier) == 0 {
		return now
	}
	out := maps.Clone(earlier)
	maps.Copy(out, now)
	return out
}

func turnOnForSession(ids []uint32) error { return vdisplay.TurnOn(ids, vdisplay.ScopeSession) }

func (m *Manager) markOff(id string, ids []uint32) {
	m.mu.Lock()
	m.off[id] = ids
	all := m.allOff()
	m.mu.Unlock()
	m.writeState(all)
}

func (m *Manager) restore(id string, ids []uint32) {
	if len(ids) > 0 && m.active != nil && m.turnOn != nil {
		active := m.active()
		var dark []uint32
		for _, s := range ids {
			if !slices.Contains(active, s) {
				dark = append(dark, s)
			}
		}
		if len(dark) > 0 {
			log.Printf("display %s: turning screens %v back on", id, dark)
			if err := m.turnOn(dark); err != nil {
				log.Printf("display %s: %v", id, err)
				return
			}
		}
	}
	m.forget(id)
}

func (m *Manager) forget(id string) {
	m.mu.Lock()
	if _, ok := m.off[id]; !ok {
		m.mu.Unlock()
		return
	}
	delete(m.off, id)
	all := m.allOff()
	m.mu.Unlock()
	m.writeState(all)
}

func (m *Manager) allOff() []uint32 {
	var all []uint32
	for _, ids := range m.off {
		all = append(all, ids...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

func (m *Manager) writeState(ids []uint32) {
	if m.state == "" {
		return
	}
	if len(ids) == 0 {
		if err := os.Remove(m.state); err != nil && !os.IsNotExist(err) {
			log.Printf("screens: %v", err)
		}
		return
	}
	b, err := json.Marshal(screensState{PID: os.Getpid(), Screens: ids})
	if err == nil {
		err = os.MkdirAll(filepath.Dir(m.state), 0o750)
	}
	if err == nil {
		err = os.WriteFile(m.state, b, 0o600)
	}
	if err != nil {
		log.Printf("screens: could not record screens turned off: %v", err)
	}
}

const RestoreFlag = "--restore-screens"

type screensState struct {
	PID     int      `json:"pid"`
	Screens []uint32 `json:"screens"`
}

func Recover(state string) { recoverWith(state, 0, vdisplay.ActiveDisplays, turnOnForSession) }

const watchScript = `while kill -0 "$0" 2>/dev/null; do sleep 1; done; exec "$1" ` + RestoreFlag + ` "$2" "$0" "$3"`

func Watch(exe, state, logPath string) error {
	cmd := exec.Command("/bin/sh", "-c", watchScript, strconv.Itoa(os.Getpid()), exe, state, logPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func RunRestore(args []string) int {
	if len(args) != 3 {
		return 2
	}
	owner, err := strconv.Atoi(args[1])
	if err != nil || owner <= 0 {
		return 2
	}
	if _, err := os.Stat(args[0]); err != nil {
		return 0
	}
	if f, err := os.OpenFile(filepath.Clean(args[2]), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		defer func() { _ = f.Close() }()
		log.SetOutput(f)
	}
	recoverWith(args[0], owner, vdisplay.ActiveDisplays, turnOnForSession)
	return 0
}

func recoverWith(state string, owner int, activeNow func() []uint32, turnOn func([]uint32) error) {
	b, err := os.ReadFile(filepath.Clean(state))
	if err != nil {
		return
	}
	var st screensState
	if err := json.Unmarshal(b, &st); err == nil {
		if owner != 0 && st.PID != owner {
			return
		}
		active := activeNow()
		var dark []uint32
		for _, id := range st.Screens {
			if !slices.Contains(active, id) {
				dark = append(dark, id)
			}
		}
		if len(dark) > 0 {
			log.Printf("screens %v were left off by DisplayExt (pid %d); turning them back on", dark, st.PID)
			if err := turnOn(dark); err != nil {
				log.Printf("screens: %v", err)
			}
		}
	}
	_ = os.Remove(state)
}

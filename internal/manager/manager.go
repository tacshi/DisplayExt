package manager

import (
	"bufio"
	"encoding/json/v2"
	"fmt"
	"io"
	"log"
	"os/exec"
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
	stopTimeout  = 4 * time.Second
)

type Status struct {
	Mounted   bool
	Busy      bool
	DisplayID uint32

	Err string

	ModeErr string
}

type Manager struct {
	exe      string
	stderr   io.Writer
	onChange func(id string)

	mu      sync.Mutex
	status  map[string]Status
	running map[string]*helper
	failed  map[string]string
	pending []config.Display
	dirty   bool
	wake    chan struct{}
}

type helper struct {
	spec  string
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}

	stopping bool
}

func New(exe string, stderr io.Writer, onChange func(id string)) *Manager {
	m := &Manager{
		exe:      exe,
		stderr:   stderr,
		onChange: onChange,
		status:   map[string]Status{},
		running:  map[string]*helper{},
		failed:   map[string]string{},
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
	var wg sync.WaitGroup
	for _, h := range hs {
		wg.Go(func() { stop(h) })
	}
	wg.Wait()
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
	for _, d := range want {
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
		_, up := m.running[d.ID]
		failed := m.failed[d.ID] == spec
		m.mu.Unlock()
		if !up && !failed {
			m.start(d.ID, spec)
		}
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

	stop(h)
	log.Printf("display %s: unmounted", id)

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

func (m *Manager) start(id, spec string) {
	m.set(id, Status{Busy: true})

	log.Printf("display %s: starting helper", id)
	cmd := exec.Command(m.exe, host.Flag, spec)
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
		m.fail(id, spec, err.Error())
		return
	}
	h := &helper{spec: spec, cmd: cmd, stdin: stdin, done: make(chan struct{})}

	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		if sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
		_, _ = io.Copy(io.Discard, stdout)
		_ = cmd.Wait()
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
		return
	}
	log.Printf("display %s: mounted as display %d (pid %d)", id, ready.DisplayID, cmd.Process.Pid)
	if ready.ModeError != "" {
		log.Printf("display %s: mode not applied: %s", id, ready.ModeError)
	}

	m.mu.Lock()
	m.running[id] = h
	m.status[id] = Status{Mounted: true, DisplayID: ready.DisplayID, ModeErr: ready.ModeError}
	m.mu.Unlock()
	m.notify(id)

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

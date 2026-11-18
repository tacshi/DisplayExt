package host

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"slices"
	"sync/atomic"
	"syscall"
	"time"

	"displayext/internal/vdisplay"

	"github.com/go-macos/objc"
)

const Flag = "--display-host"

const StrategyEnv = "DISPLAYEXT_HOST_STRATEGY"

const blackCheck = 500 * time.Millisecond

type Command struct {
	ScreensOff []uint32       `json:"screensOff"`
	Spec       *vdisplay.Spec `json:"spec,omitzero"`
}

type Ready struct {
	DisplayID uint32 `json:"displayID,omitzero"`
	ModeError string `json:"modeError,omitempty"`
	Error     string `json:"error,omitempty"`

	ScreensOff   []uint32 `json:"screensOff,omitempty"`
	ScreensError string   `json:"screensError,omitempty"`

	Remount bool `json:"remount,omitzero"`
}

func Run(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) < 1 || len(args) > 2 {
		report(stdout, Ready{Error: "usage: " + Flag + " <spec-json> [screens-off-json]"})
		return 2
	}
	var spec vdisplay.Spec
	if err := json.UnmarshalRead(bytes.NewReader([]byte(args[0])), &spec); err != nil {
		report(stdout, Ready{Error: fmt.Sprintf("bad spec: %v", err)})
		return 2
	}
	var off []uint32
	if len(args) == 2 {
		if err := json.UnmarshalRead(bytes.NewReader([]byte(args[1])), &off); err != nil {
			report(stdout, Ready{Error: fmt.Sprintf("bad screen list: %v", err)})
			return 2
		}
	}

	if os.Getenv(StrategyEnv) == "main" {
		return serve(spec, off, stdin, stdout)
	}
	objc.App()
	var done atomic.Bool
	var code atomic.Int32
	go func() {
		runtime.LockOSThread()
		code.Store(int32(serve(spec, off, stdin, stdout)))
		done.Store(true)
	}()
	objc.RunAppLoop(1, done.Load)
	return int(code.Load())
}

func serve(spec vdisplay.Spec, off []uint32, stdin io.Reader, stdout io.Writer) int {
	d, err := vdisplay.Open(spec)
	if err != nil {
		report(stdout, Ready{Error: err.Error()})
		return 1
	}
	r := Ready{DisplayID: d.ID()}
	if d.ModeErr != nil {
		r.ModeError = d.ModeErr.Error()
	}
	sc := &screens{master: d.ID()}
	sc.apply(off, &r)
	report(stdout, r)

	cmds := make(chan Command)
	go func() {
		defer close(cmds)
		in := bufio.NewScanner(stdin)
		for in.Scan() {
			var c Command
			if json.Unmarshal(in.Bytes(), &c) == nil {
				cmds <- c
			}
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	hold := time.NewTicker(holdCheck)
	defer hold.Stop()
	keep := keeper{on: d.ModeErr == nil}
	for running := true; running; {
		select {
		case now := <-hold.C:
			keep.check(d, spec, now)
		case c, ok := <-cmds:
			if !ok {
				running = false
				break
			}
			if c.Spec != nil {
				r := reshape(d, spec, *c.Spec)
				if !r.Remount && r.ModeError == "" {
					spec = *c.Spec
					keep = keeper{on: true}
				}
				report(stdout, r)
				break
			}
			var r Ready
			sc.apply(c.ScreensOff, &r)
			report(stdout, r)
		case <-sig:
			running = false
		}
	}
	err = sc.turnOn()
	_ = d.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper: turning screens back on:", err)
		return 1
	}
	return 0
}

func reshape(d *vdisplay.Display, cur, next vdisplay.Spec) Ready {
	r := Ready{DisplayID: d.ID()}
	if !next.Fits(cur) {
		r.Remount = true
		return r
	}
	if !next.SameModes(cur) {
		if err := d.Reapply(next, vdisplay.DefaultOptions); err != nil {
			r.ModeError = err.Error()
			return r
		}
		fmt.Fprintf(os.Stderr, "helper: display %d has a new mode list\n", d.ID())
	}
	if err := d.ApplyMode(next); err != nil {
		r.ModeError = err.Error()
		return r
	}
	fmt.Fprintf(os.Stderr, "helper: display %d changed in place to %d×%d\n", d.ID(), next.PointsW, next.PointsH)
	return r
}

const (
	holdCheck  = time.Second
	holdWindow = 30 * time.Second
	holdTimes  = 3
)

type keeper struct {
	on    bool
	fixes []time.Time
}

func (k *keeper) allow(now time.Time) bool {
	k.fixes = slices.DeleteFunc(k.fixes, func(t time.Time) bool { return now.Sub(t) >= holdWindow })
	if len(k.fixes) >= holdTimes {
		return false
	}
	k.fixes = append(k.fixes, now)
	return true
}

func (k *keeper) check(d *vdisplay.Display, spec vdisplay.Spec, now time.Time) {
	if !k.on {
		return
	}
	cur, ok := vdisplay.CurrentMode(d.ID())
	if !ok || cur.Same(spec.Want()) {
		return
	}
	if !k.allow(now) {
		return
	}
	fmt.Fprintf(os.Stderr, "helper: display %d was switched to %s; putting back %d×%d\n", d.ID(), cur, spec.PointsW, spec.PointsH)
	if err := d.ApplyMode(spec); err != nil {
		fmt.Fprintln(os.Stderr, "helper: putting the mode back:", err)
	}
}

type screens struct {
	master uint32
	off    []uint32
	scope  vdisplay.Scope
	black  chan struct{}
	done   chan struct{}
}

func (s *screens) apply(off []uint32, r *Ready) {
	if len(off) == 0 {
		if err := s.turnOn(); err != nil {
			r.ScreensError = err.Error()
		}
		return
	}
	if len(s.off) == 0 {
		if err := s.turnOff(off); err != nil {
			r.ScreensError = err.Error()
		}
	}
	r.ScreensOff = s.off
}

func (s *screens) turnOff(ids []uint32) error {
	scope, err := vdisplay.TurnOff(ids, s.master)
	if err != nil {
		return err
	}
	s.off, s.scope = ids, scope
	fmt.Fprintf(os.Stderr, "helper: turned off screens %v %s\n", ids, scope)
	if err := vdisplay.Blackout(ids); err != nil {
		fmt.Fprintln(os.Stderr, "helper: blacking out screens:", err)
	}
	s.black, s.done = make(chan struct{}), make(chan struct{})
	go func(black, done chan struct{}) {
		defer close(done)
		vdisplay.KeepBlack(ids, blackCheck, black)
	}(s.black, s.done)
	return nil
}

func (s *screens) turnOn() error {
	if len(s.off) == 0 {
		return nil
	}
	close(s.black)
	<-s.done
	vdisplay.RestoreColors()
	err := vdisplay.TurnOn(s.off, s.scope)
	if err == nil {
		fmt.Fprintf(os.Stderr, "helper: turned screens %v back on\n", s.off)
	}
	s.off = nil
	return err
}

func report(w io.Writer, r Ready) {
	if r.Error != "" {
		fmt.Fprintln(os.Stderr, "helper:", r.Error)
	}
	if r.ModeError != "" {
		fmt.Fprintln(os.Stderr, "helper: mode:", r.ModeError)
	}
	if r.ScreensError != "" {
		fmt.Fprintln(os.Stderr, "helper: screens:", r.ScreensError)
	}
	b, _ := json.Marshal(r)
	_, _ = fmt.Fprintf(w, "%s\n", b)
}

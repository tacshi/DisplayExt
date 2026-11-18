package uudetect

import (
	"context"
	"slices"
	"time"
)

type State int

const (
	NotRunning State = iota

	Idle

	Connected
)

func (s State) String() string {
	switch s {
	case NotRunning:
		return "not running"
	case Idle:
		return "not connected"
	case Connected:
		return "connected"
	}
	return "unknown"
}

type Rule struct {
	Process   string
	Assertion string

	Grace time.Duration
}

type Snapshot struct {
	Processes  map[int32]string
	Assertions map[int32][]string
}

type Sample struct {
	Running   bool
	Held      bool
	Streaming bool
	Showing   uint32
}

type Status struct {
	State   State
	Showing uint32
}

type Capture interface {
	Sample(now time.Time, server int32) (active bool, showing uint32, known bool)
	Changed() <-chan struct{}
}

func (s Snapshot) Server() int32 {
	var pid int32
	for p := range s.Processes {
		if pid == 0 || p < pid {
			pid = p
		}
	}
	return pid
}

func (r Rule) Sample(s Snapshot) Sample {
	var out Sample
	for pid, name := range s.Processes {
		if name != r.Process {
			continue
		}
		out.Running = true
		if slices.Contains(s.Assertions[pid], r.Assertion) {
			out.Held = true
		}
	}
	return out
}

type Detector struct {
	rule     Rule
	state    State
	started  bool
	lastHeld time.Time
}

func NewDetector(r Rule) *Detector { return &Detector{rule: r} }

func (d *Detector) State() State { return d.state }

func (d *Detector) Step(now time.Time, s Sample) (State, bool) {
	next := d.state
	switch {
	case s.Held || s.Streaming:
		d.lastHeld = now
		next = Connected
	case d.state == Connected && now.Sub(d.lastHeld) < d.rule.Grace:
	case s.Running:
		next = Idle
	default:
		next = NotRunning
	}
	changed := !d.started || next != d.state
	d.started = true
	d.state = next
	return next, changed
}

func Watch(ctx context.Context, r Rule, interval time.Duration, capture Capture, onChange func(Status), onError func(error)) {
	d := NewDetector(r)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var wake <-chan struct{}
	if capture != nil {
		wake = capture.Changed()
	}
	var last Status
	started := false
	for {
		snap, err := TakeSnapshot(r.Process)
		if err != nil {
			if onError != nil {
				onError(err)
			}
		} else {
			now := time.Now()
			sample := r.Sample(snap)
			if capture != nil {
				if active, showing, known := capture.Sample(now, snap.Server()); known {
					sample.Streaming, sample.Showing = active, showing
				}
			}
			st, _ := d.Step(now, sample)
			next := Status{State: st}
			if st == Connected {
				next.Showing = sample.Showing
			}
			if !started || next != last {
				started, last = true, next
				onChange(next)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-wake:
		}
	}
}

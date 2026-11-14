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
	Running bool
	Held    bool
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
	case s.Held:
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

func Watch(ctx context.Context, r Rule, interval time.Duration, onChange func(State), onError func(error)) {
	d := NewDetector(r)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		snap, err := TakeSnapshot(r.Process)
		if err != nil {
			if onError != nil {
				onError(err)
			}
		} else if st, changed := d.Step(time.Now(), r.Sample(snap)); changed {
			onChange(st)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

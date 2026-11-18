package uucapture

import "time"

const RestartGrace = 3 * time.Second

type State struct {
	active     bool
	pendingEnd time.Time
	described  uint32
	captured   uint32
	sameStream bool
}

func (s *State) Apply(r Record) {
	s.settle(r.Time)
	switch r.Kind {
	case DisplayDescribed:
		s.described = r.Display
	case CaptureStarted:
		if !s.active || !s.sameStream {
			s.captured = s.described
		}
		s.sameStream = true
		s.active = true
		s.pendingEnd = time.Time{}
	case EncoderCreated:
		s.pendingEnd = time.Time{}
	case CaptureStopped, EncoderEnded:
		s.sameStream = false
		if s.active && s.pendingEnd.IsZero() {
			s.pendingEnd = r.Time
		}
	}
}

func (s State) Active(now time.Time) bool {
	if !s.active {
		return false
	}
	return s.pendingEnd.IsZero() || now.Sub(s.pendingEnd) <= RestartGrace
}

func (s State) Streaming(now time.Time) uint32 {
	if s.Active(now) {
		return s.captured
	}
	return 0
}

func (s *State) settle(at time.Time) {
	if s.active && !s.Active(at) {
		*s = State{described: s.described}
	}
}

func Resumed(history, live []Record, server int32) State {
	var s State
	for _, r := range history {
		if server != 0 && r.PID == server {
			s.Apply(r)
		}
	}
	var after time.Time
	if n := len(history); n > 0 {
		after = history[n-1].Time
	}
	for _, r := range live {
		if r.Time.After(after) {
			s.Apply(r)
		}
	}
	return s
}

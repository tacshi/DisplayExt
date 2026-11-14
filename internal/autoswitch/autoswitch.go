package autoswitch

import "displayext/internal/config"

type Switcher struct {
	connected bool

	override map[string]bool
}

func New() *Switcher { return &Switcher{override: map[string]bool{}} }

func (s *Switcher) Connected() bool { return s.connected }

func (s *Switcher) SetConnected(connected bool) bool {
	if connected == s.connected {
		return false
	}
	s.connected = connected
	clear(s.override)
	return true
}

func (s *Switcher) Toggle(d config.Display, mounted bool) {
	if d.Trigger == config.TriggerUU {
		s.override[d.ID] = mounted
	}
}

func (s *Switcher) Forget(id string) { delete(s.override, id) }

func (s *Switcher) Want(d config.Display) bool {
	if d.Trigger != config.TriggerUU {
		return d.Mounted
	}
	if v, ok := s.override[d.ID]; ok {
		return v
	}
	return s.connected
}

package autoswitch

import "displayext/internal/config"

type Switcher struct {
	connected bool
	device    string
	blind     bool
	guess     string

	override map[string]bool
}

func New() *Switcher { return &Switcher{override: map[string]bool{}} }

func (s *Switcher) Connected() bool { return s.connected }

func (s *Switcher) SetConnected(connected bool) bool {
	if connected == s.connected {
		return false
	}
	s.connected = connected
	s.device, s.blind, s.guess = "", false, ""
	clear(s.override)
	return true
}

func (s *Switcher) Device() string { return s.device }

func (s *Switcher) SetDevice(name string) bool {
	if !s.connected || name == s.device {
		return false
	}
	s.device = name
	return true
}

func (s *Switcher) SetBlind(guess string) bool {
	if !s.connected || (s.blind && guess == s.guess) {
		return false
	}
	s.blind, s.guess = true, guess
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
	switch {
	case !s.connected:
		return false
	case d.ConnectFrom == "":
		return true
	case s.device != "":
		return d.ConnectFrom == s.device
	case !s.blind:
		return false
	}
	return s.guess == "" || d.ConnectFrom == s.guess
}

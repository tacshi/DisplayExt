package autoswitch

import "displayext/internal/config"

type override struct {
	remote  config.Trigger
	mounted bool
}

type Switcher struct {
	connected map[config.Trigger]bool
	device    string
	blind     bool
	guess     string

	override map[string]override
}

func New() *Switcher {
	return &Switcher{connected: map[config.Trigger]bool{}, override: map[string]override{}}
}

func (s *Switcher) Connected(remote config.Trigger) bool { return s.connected[remote] }

func (s *Switcher) AnyConnected() bool {
	for _, c := range s.connected {
		if c {
			return true
		}
	}
	return false
}

func (s *Switcher) SetConnected(remote config.Trigger, connected bool) bool {
	if !remote.Remote() || connected == s.connected[remote] {
		return false
	}
	s.connected[remote] = connected
	if remote == config.TriggerUU {
		s.device, s.blind, s.guess = "", false, ""
	}
	for id, o := range s.override {
		if o.remote == remote {
			delete(s.override, id)
		}
	}
	return true
}

func (s *Switcher) Device() string { return s.device }

func (s *Switcher) SetDevice(name string) bool {
	if !s.connected[config.TriggerUU] || name == s.device {
		return false
	}
	s.device = name
	return true
}

func (s *Switcher) SetBlind(guess string) bool {
	if !s.connected[config.TriggerUU] || (s.blind && guess == s.guess) {
		return false
	}
	s.blind, s.guess = true, guess
	return true
}

func (s *Switcher) Toggle(d config.Display, mounted bool) {
	if d.Trigger.Remote() {
		s.override[d.ID] = override{remote: d.Trigger, mounted: mounted}
	}
}

func (s *Switcher) Forget(id string) { delete(s.override, id) }

func (s *Switcher) Want(d config.Display) bool {
	if !d.Trigger.Remote() {
		return d.Mounted
	}
	if o, ok := s.override[d.ID]; ok {
		return o.mounted
	}
	switch {
	case !s.connected[d.Trigger]:
		return false
	case d.Trigger != config.TriggerUU || d.ConnectFrom == "":
		return true
	case s.device != "":
		return d.ConnectFrom == s.device
	case !s.blind:
		return false
	}
	return s.guess == "" || d.ConnectFrom == s.guess
}

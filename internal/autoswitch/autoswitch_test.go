package autoswitch

import (
	"testing"

	"displayext/internal/config"
)

func displays() (uu, manual config.Display) {
	uu = config.NewDisplay("iPad", config.Presets[0])
	uu.Trigger = config.TriggerUU
	manual = config.NewDisplay("Desk", config.Presets[2])
	return uu, manual
}

func TestFollowsSession(t *testing.T) {
	s := New()
	uu, manual := displays()
	if s.Want(uu) || s.Want(manual) {
		t.Fatal("nothing should be mounted before a session")
	}
	if !s.SetConnected(true) {
		t.Fatal("connect not reported as a change")
	}
	if !s.Want(uu) {
		t.Error("UU display not mounted during a session")
	}
	if s.Want(manual) {
		t.Error("manual display mounted by a session")
	}
	if s.SetConnected(true) {
		t.Error("repeated connect reported as a change")
	}
	s.SetConnected(false)
	if s.Want(uu) {
		t.Error("UU display still mounted after the session")
	}
}

func TestManualDisplayUsesConfig(t *testing.T) {
	s := New()
	_, manual := displays()
	manual.Mounted = true
	if !s.Want(manual) {
		t.Error("manual display with Mounted=true not wanted")
	}
	s.Toggle(manual, false)
	if !s.Want(manual) {
		t.Error("Toggle overrode a manual display's config state")
	}
}

func TestOverrideUntilNextSessionChange(t *testing.T) {
	s := New()
	uu, _ := displays()

	s.Toggle(uu, true)
	if !s.Want(uu) {
		t.Fatal("manual mount ignored")
	}
	s.SetConnected(true)
	if !s.Want(uu) {
		t.Fatal("not mounted during session")
	}
	s.Toggle(uu, false)
	if s.Want(uu) {
		t.Fatal("manual unmount ignored during session")
	}
	s.SetConnected(false)
	s.SetConnected(true)
	if !s.Want(uu) {
		t.Error("next session did not mount the display")
	}
}

func TestForget(t *testing.T) {
	s := New()
	uu, _ := displays()
	s.Toggle(uu, true)
	s.Forget(uu.ID)
	if s.Want(uu) {
		t.Error("override survived Forget")
	}
}

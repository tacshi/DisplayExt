package vdisplay

import (
	"fmt"
	"slices"
	"time"
)

type Scope int

const (
	ScopeApp Scope = iota
	ScopeSession
)

func (s Scope) String() string {
	if s == ScopeApp {
		return "with app scope"
	}
	return "with session scope"
}

var (
	offWait = 3 * time.Second
	onWait  = 5 * time.Second
)

func TurnOff(ids []uint32) (Scope, error) {
	if len(ids) == 0 {
		return ScopeApp, nil
	}
	if !slices.ContainsFunc(ActiveDisplays(), func(id uint32) bool { return !slices.Contains(ids, id) }) {
		return ScopeApp, fmt.Errorf("no other screen would be left on")
	}
	for _, scope := range []Scope{ScopeApp, ScopeSession} {
		if err := setEnabled(ids, false, scope); err != nil {
			_ = TurnOn(ids, ScopeSession)
			return scope, err
		}
		if waitFor(ids, false, offWait) {
			if len(ActiveDisplays()) == 0 {
				_ = TurnOn(ids, ScopeSession)
				return scope, fmt.Errorf("no screen was left on")
			}
			return scope, nil
		}
	}
	_ = TurnOn(ids, ScopeSession)
	return ScopeSession, fmt.Errorf("macOS kept screens %v on", ids)
}

func TurnOn(ids []uint32, scope Scope) error {
	if len(ids) == 0 {
		return nil
	}
	err := setEnabled(ids, true, scope)
	if !waitFor(ids, true, onWait) && err == nil {
		err = fmt.Errorf("screens %v did not come back within %s", ids, onWait)
	}
	return err
}

func waitFor(ids []uint32, on bool, timeout time.Duration) bool {
	for deadline := time.Now().Add(timeout); ; {
		active := ActiveDisplays()
		done := true
		for _, id := range ids {
			if slices.Contains(active, id) != on {
				done = false
				break
			}
		}
		if done {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

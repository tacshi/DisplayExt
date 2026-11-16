package vdisplay

import (
	"errors"
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

func TurnOff(ids []uint32, master uint32) (Scope, error) {
	if len(ids) == 0 {
		return ScopeApp, nil
	}
	if !slices.Contains(ActiveDisplays(), master) {
		return ScopeApp, fmt.Errorf("display %d is not on", master)
	}
	for _, scope := range []Scope{ScopeApp, ScopeSession} {
		if err := setMirror(ids, master, scope); err != nil {
			_ = TurnOn(ids, ScopeSession)
			return scope, err
		}
		if waitFor(ids, false, offWait) {
			if !slices.Contains(ActiveDisplays(), master) {
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
	err := errors.Join(setMirror(ids, noDisplay, scope), setEnabled(ids, true, scope))
	if waitFor(ids, true, onWait) {
		return nil
	}
	if err == nil {
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

func KeepBlack(ids []uint32, every time.Duration, done <-chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		var lit []uint32
		for _, id := range ids {
			if !isBlack(id) {
				lit = append(lit, id)
			}
		}
		if len(lit) > 0 {
			_ = Blackout(lit)
		}
		select {
		case <-done:
			return
		case <-t.C:
		}
	}
}

const noDisplay = 0

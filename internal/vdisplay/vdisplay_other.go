//go:build !darwin

package vdisplay

import (
	"errors"
	"time"
)

var errUnsupported = errors.New("vdisplay: virtual displays need macOS")

type Display struct{ ModeErr error }

func (d *Display) ID() uint32 { return 0 }

func (d *Display) Close() error { return nil }

func Available() error { return errUnsupported }

func Open(Spec) (*Display, error) { return nil, errUnsupported }

func OpenWith(Spec, Options) (*Display, error) { return nil, errUnsupported }

func ActiveDisplays() []uint32 { return nil }

func IsActive(uint32) bool { return false }

func Create(Spec, Options) (*Display, error) { return nil, errUnsupported }

func (d *Display) WaitActive(time.Duration) bool { return false }

func (d *Display) ApplyMode(Spec) error { return errUnsupported }

func CurrentMode(uint32) (Mode, bool) { return Mode{}, false }

func OtherModes() map[uint32]Mode { return nil }

func SetMode(uint32, Mode) error { return errUnsupported }

func OtherDisplays() []uint32 { return nil }

func setEnabled([]uint32, bool, Scope) error { return errUnsupported }

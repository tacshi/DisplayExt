//go:build !darwin

package uuwindow

import "errors"

var (
	ErrNotTrusted = errors.New("uuwindow: DisplayExt is not allowed to use Accessibility")
	ErrNoWindows  = errors.New("uuwindow: UU远程 has no windows")
)

func Trusted() bool { return false }

func AskForTrust() bool { return false }

func Read(int32) ([]Element, error) { return nil, ErrNotTrusted }

//go:build !darwin

package uudetect

import "errors"

func TakeSnapshot(process string) (Snapshot, error) {
	return Snapshot{}, errors.New("uudetect: only supported on macOS")
}

func (r Rule) Snapshot() (Snapshot, error) { return TakeSnapshot(r.Process) }

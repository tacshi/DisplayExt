package config

import (
	"fmt"
	"math"
	"slices"
)

type Aspect struct{ W, H int }

var Presets = []Aspect{{4, 3}, {16, 10}, {16, 9}, {3, 2}, {21, 9}}

const MaxRatio = 4

func (a Aspect) String() string { return fmt.Sprintf("%d:%d", a.W, a.H) }

func (a Aspect) Valid() bool {
	if a.W <= 0 || a.H <= 0 {
		return false
	}
	r := float64(a.W) / float64(a.H)
	return r >= 1.0/MaxRatio && r <= MaxRatio
}

type Size struct{ W, H int }

func (s Size) String() string { return fmt.Sprintf("%d×%d", s.W, s.H) }

var sizeTargets = []int{1024, 1280, 1440, 1600, 1920, 2048, 2304, 2560, 2880, 3200}

const (
	MinHiDPILong  = 800
	MinHiDPIShort = 525
	CompactShort  = 540
)

func HiDPIOK(w, h int) bool {
	return max(w, h) >= MinHiDPILong && min(w, h) >= MinHiDPIShort
}

func (a Aspect) Sizes(hidpi bool) []Size {
	if !a.Valid() {
		return nil
	}
	var out []Size
	add := func(s Size) {
		if sizeOK(s.W, s.H) && (!hidpi || HiDPIOK(s.W, s.H)) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	compact := Size{W: CompactShort, H: CompactShort}
	if a.W >= a.H {
		compact.W = int(math.Round(float64(CompactShort) * float64(a.W) / float64(a.H)))
	} else {
		compact.H = int(math.Round(float64(CompactShort) * float64(a.H) / float64(a.W)))
	}
	if HiDPIOK(compact.W, compact.H) {
		add(compact)
	}
	for _, t := range sizeTargets {
		var s Size
		if a.W >= a.H {
			s.W = roundTo(t, 8)
			s.H = int(math.Round(float64(s.W) * float64(a.H) / float64(a.W)))
		} else {
			s.H = roundTo(t, 8)
			s.W = int(math.Round(float64(s.H) * float64(a.W) / float64(a.H)))
		}
		if max(s.W, s.H) <= max(compact.W, compact.H) {
			continue
		}
		add(s)
	}
	return out
}

func (a Aspect) DefaultSize() Size {
	sizes := a.Sizes(true)
	if len(sizes) == 0 {
		sizes = a.Sizes(false)
	}
	for _, s := range sizes {
		if max(s.W, s.H) == 2048 {
			return s
		}
	}
	if len(sizes) > 0 {
		return sizes[len(sizes)/2]
	}
	return Size{1920, 1080}
}

func (d Display) AspectOf() Aspect { return Aspect{W: d.AspectW, H: d.AspectH} }

const PixelBox = 6400

func (d Display) Modes() []Size {
	out := []Size{{W: d.Width, H: d.Height}}
	for _, s := range d.AspectOf().Sizes(d.HiDPI) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func (d Display) Box() (w, h int) {
	pw, ph := d.PixelSize()
	side := max(PixelBox, pw, ph)
	return side, side
}

func roundTo(v, step int) int { return (v + step/2) / step * step }

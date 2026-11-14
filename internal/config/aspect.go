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

func (a Aspect) Sizes() []Size {
	if !a.Valid() {
		return nil
	}
	var out []Size
	for _, t := range sizeTargets {
		var s Size
		if a.W >= a.H {
			s.W = roundTo(t, 8)
			s.H = int(math.Round(float64(s.W) * float64(a.H) / float64(a.W)))
		} else {
			s.H = roundTo(t, 8)
			s.W = int(math.Round(float64(s.H) * float64(a.W) / float64(a.H)))
		}
		if !sizeOK(s.W, s.H) {
			continue
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func (a Aspect) DefaultSize() Size {
	sizes := a.Sizes()
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

func (d Display) ExtraModes() []Size {
	maxW, maxH := d.PixelSize()
	scale := 1
	if d.HiDPI {
		scale = 2
	}
	var out []Size
	for _, s := range d.AspectOf().Sizes() {
		p := Size{s.W * scale, s.H * scale}
		if p.W > maxW || p.H > maxH || (p.W == maxW && p.H == maxH) {
			continue
		}
		out = append(out, p)
	}
	return out
}

func roundTo(v, step int) int { return (v + step/2) / step * step }

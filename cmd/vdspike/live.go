package main

import (
	"fmt"
	"slices"
	"time"

	"displayext/internal/config"
	"displayext/internal/vdisplay"
)

const (
	liveBox    = 6400
	liveSerial = 0x44580012
	liveHold   = 3 * time.Second
)

func liveSpec(a config.Aspect, size config.Size, hidpi bool, hz int) vdisplay.Spec {
	scale := 1
	if hidpi {
		scale = 2
	}
	extra := a.Sizes(hidpi)
	return vdisplay.Spec{
		Name:       "DisplayExt Live Test",
		PointsW:    size.W,
		PointsH:    size.H,
		HiDPI:      hidpi,
		PixelsW:    size.W * scale,
		PixelsH:    size.H * scale,
		MaxPixelsW: liveBox,
		MaxPixelsH: liveBox,
		RefreshHz:  hz,
		Serial:     liveSerial,
		Extra:      extra,
		SizeMMW:    float64(size.W) * 25.4 / 110,
		SizeMMH:    float64(size.H) * 25.4 / 110,
	}
}

type liveStep struct {
	name    string
	reapply bool
	spec    vdisplay.Spec
}

func liveSteps() []liveStep {
	four, ten, tall := config.Aspect{W: 4, H: 3}, config.Aspect{W: 16, H: 10}, config.Aspect{W: 3, H: 4}
	return []liveStep{
		{"size 1024×768, same mode list", false, liveSpec(four, config.Size{W: 1024, H: 768}, true, 60)},
		{"size 1600×1200, same mode list", false, liveSpec(four, config.Size{W: 1600, H: 1200}, true, 60)},
		{"size 2880×2160, same mode list", false, liveSpec(four, config.Size{W: 2880, H: 2160}, true, 60)},
		{"size 2048×1536, same mode list", false, liveSpec(four, config.Size{W: 2048, H: 1536}, true, 60)},
		{"ratio 16:10 at 2048×1280, new mode list", true, liveSpec(ten, config.Size{W: 2048, H: 1280}, true, 60)},
		{"refresh 120 Hz, new mode list", true, liveSpec(ten, config.Size{W: 2048, H: 1280}, true, 120)},
		{"HiDPI off at 2048×1280, new mode list", true, liveSpec(ten, config.Size{W: 2048, H: 1280}, false, 120)},
		{"portrait 3:4 at 1536×2048, new mode list", true, liveSpec(tall, config.Size{W: 1536, H: 2048}, true, 60)},
		{"back to 4:3 at 2048×1536, new mode list", true, liveSpec(four, config.Size{W: 2048, H: 1536}, true, 60)},
	}
}

func runLive() int {
	fmt.Printf("Live-change test — %s\n", time.Now().Format(time.DateTime))
	fmt.Println("A test display is created once, then changed in place step by step.")
	fmt.Println("If UU远程 is showing it, note after each step whether the picture follows and taps land right.")
	first := liveSpec(config.Aspect{W: 4, H: 3}, config.Size{W: 2048, H: 1536}, true, 60)
	start := time.Now()
	d, err := vdisplay.Open(first)
	if err != nil {
		fmt.Println("Could not create the test display:", err)
		return 1
	}
	id := d.ID()
	fmt.Printf("\n[0] created display %d with a %d×%d pixel box in %s; mode %s",
		id, liveBox, liveBox, time.Since(start).Round(100*time.Millisecond), modeOf(id))
	if d.ModeErr != nil {
		fmt.Printf(" (mode not applied: %v)", d.ModeErr)
	}
	fmt.Println()
	time.Sleep(liveHold)

	failed := 0
	for i, st := range liveSteps() {
		fmt.Printf("\n[%d] %s\n", i+1, st.name)
		start := time.Now()
		if st.reapply {
			if err := d.Reapply(st.spec, vdisplay.DefaultOptions); err != nil {
				fmt.Println("  applySettings failed:", err)
				failed++
				continue
			}
			fmt.Printf("  new mode list applied in %s; now %s\n", time.Since(start).Round(10*time.Millisecond), modeOf(id))
		}
		err := d.ApplyMode(st.spec)
		took := time.Since(start).Round(10 * time.Millisecond)
		time.Sleep(500 * time.Millisecond)
		m, _ := vdisplay.CurrentMode(id)
		want := vdisplay.Mode{PointsW: st.spec.PointsW, PointsH: st.spec.PointsH, PixelsW: st.spec.PixelsW, PixelsH: st.spec.PixelsH, RefreshHz: float64(st.spec.RefreshHz)}
		active := slices.Contains(vdisplay.ActiveDisplays(), id)
		fmt.Printf("  took %s; display %d active %v; mode %s\n", took, id, active, m)
		switch {
		case err != nil:
			fmt.Println("  => FAIL:", err)
			failed++
		case !active || !m.Same(want):
			fmt.Printf("  => FAIL: wanted %s\n", want)
			failed++
		default:
			fmt.Println("  => WORKS")
		}
		time.Sleep(liveHold)
	}

	start = time.Now()
	_ = d.Close()
	for time.Since(start) < 60*time.Second && slices.Contains(vdisplay.ActiveDisplays(), id) {
		time.Sleep(100 * time.Millisecond)
	}
	if slices.Contains(vdisplay.ActiveDisplays(), id) {
		fmt.Printf("\nThe display was still there 60s after it was released.\n")
		failed++
	} else {
		fmt.Printf("\nThe display went away %s after it was released.\n", time.Since(start).Round(100*time.Millisecond))
	}
	fmt.Printf("%d of %d steps failed.\n", failed, len(liveSteps()))
	if failed > 0 {
		return 1
	}
	return 0
}

func modeOf(id uint32) string {
	if m, ok := vdisplay.CurrentMode(id); ok {
		return m.String()
	}
	return "unknown"
}

func runModes() int {
	fmt.Printf("Mode list probe — %s\n", time.Now().Format(time.DateTime))
	fmt.Println("An iPhone 15 Pro display (540×921 HiDPI) is created twice; every mode macOS offers is listed.")
	fmt.Println("Flags: native 0x02000000, default 0x00000004, valid 0x00000001, safe 0x00000002.")
	a := config.Aspect{W: 393, H: 670}
	d := config.NewDisplay("DisplayExt Modes Test", a)
	d.Width, d.Height, d.Serial = 540, 921, 0x44580041
	newSpec := vdisplay.FromConfig(d)
	oldSpec := newSpec
	oldSpec.Serial = 0x44580042
	oldSpec.MaxPixelsW, oldSpec.MaxPixelsH = 0, 0
	oldSpec.Extra = nil
	oldSpec.PointsW, oldSpec.PointsH = oldSpec.PixelsW, oldSpec.PixelsH
	for i, c := range []struct {
		name string
		spec vdisplay.Spec
	}{
		{"before: 1080×1842 registered in pixels, box 1080×1842", oldSpec},
		{"now: sizes registered in points, box 6400×6400", newSpec},
	} {
		fmt.Printf("\n[%d/2] %s\n", i+1, c.name)
		disp, err := vdisplay.Create(c.spec, vdisplay.DefaultOptions)
		if err != nil {
			fmt.Println("  could not create it:", err)
			continue
		}
		id := disp.ID()
		disp.WaitActive(8 * time.Second)
		time.Sleep(time.Second)
		fmt.Printf("  display %d comes up at %s\n", id, modeOf(id))
		cur, _ := vdisplay.CurrentMode(id)
		for _, m := range vdisplay.ListModes(id) {
			mark := " "
			if m.Same(cur) {
				mark = "*"
			}
			fmt.Printf("  %s %-32s flags %#010x usable %v\n", mark, m.String(), m.IOFlags, m.Usable)
		}
		_ = disp.Close()
		for t := time.Now(); time.Since(t) < 5*time.Second && slices.Contains(vdisplay.ActiveDisplays(), id); {
			time.Sleep(100 * time.Millisecond)
		}
	}
	return 0
}

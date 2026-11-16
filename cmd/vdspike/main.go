package main

import (
	"bufio"
	"encoding/json/v2"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"displayext/internal/config"
	"displayext/internal/vdisplay"

	"github.com/go-macos/objc"
)

func init() { runtime.LockOSThread() }

type variant struct {
	Name string
	Opts vdisplay.Options
	Spec vdisplay.Spec
}

type childReport struct {
	ID        uint32 `json:"id,omitzero"`
	Err       string `json:"err,omitempty"`
	InProcess bool   `json:"inProcess,omitzero"`
	Mode      string `json:"mode,omitempty"`
	ModeErr   string `json:"modeErr,omitempty"`
	Done      bool   `json:"done,omitzero"`
}

func specs() (ipad, small vdisplay.Spec) {
	d := config.Default().Displays[0]
	d.Name, d.Serial = "DisplayExt Test", 0x44580010
	ipad = vdisplay.FromConfig(d)
	s := config.NewDisplay("DisplayExt Test", config.Presets[2])
	s.Width, s.Height, s.HiDPI, s.Serial = 1920, 1080, false, 0x44580011
	small = vdisplay.FromConfig(s)
	return ipad, small
}

func variants() []variant {
	ipad, small := specs()
	return []variant{
		{"main queue + sRGB color (app default)", vdisplay.Options{}, ipad},
		{"same, 1920×1080 without HiDPI", vdisplay.Options{}, small},
		{"Go library style: private queue, no color", vdisplay.Options{PrivateQueue: true, NoColor: true}, ipad},
		{"private queue + sRGB color", vdisplay.Options{PrivateQueue: true}, ipad},
		{"main queue, no color", vdisplay.Options{NoColor: true}, ipad},
		{"no color + transferFunction 0 (BetterDisplay style)", vdisplay.Options{NoColor: true, TransferFunction: 1}, ipad},
		{"sRGB color + transferFunction 0", vdisplay.Options{TransferFunction: 1}, ipad},
	}
}

func main() {
	child := flag.String("child", "", "internal: run one variant (JSON)")
	list := flag.Bool("list", false, "internal: print the active display IDs")
	only := flag.Int("only", 0, "run only variant N (1-based)")
	screens := flag.Bool("screens", false, "test turning the other screens off and back on")
	turnOn := flag.String("turn-on", "", "turn these screens on (JSON)")
	alone := flag.Bool("screens-alone", false, "test what macOS does when a crash leaves no screen on")
	offChild := flag.String("screens-child", "", "internal: turn these screens off (JSON), then crash")
	aloneChild := flag.String("screens-alone-child", "", "internal: mount a display, turn these screens off (JSON), then crash")
	flag.Parse()

	switch {
	case *turnOn != "":
		var ids []uint32
		if err := json.Unmarshal([]byte(*turnOn), &ids); err != nil {
			fmt.Println("bad screen list:", err)
			os.Exit(2)
		}
		if err := vdisplay.TurnOn(ids, vdisplay.ScopeSession); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case *screens:
		os.Exit(onAppLoop(runScreens))
	case *alone:
		os.Exit(onAppLoop(runAlone))
	case *offChild != "":
		os.Exit(runOffChild(*offChild, false))
	case *aloneChild != "":
		os.Exit(runOffChild(*aloneChild, true))
	case *list:
		for _, id := range vdisplay.ActiveDisplays() {
			fmt.Println(id)
		}
	case *child != "":
		os.Exit(runChild(*child))
	default:
		os.Exit(runMatrix(*only))
	}
}

func runChild(arg string) int {
	var v variant
	if err := json.Unmarshal([]byte(arg), &v); err != nil {
		fmt.Println(`{"err":"bad variant"}`)
		return 2
	}
	objc.App()
	var done atomic.Bool
	go func() {
		runtime.LockOSThread()
		defer done.Store(true)
		say := func(r childReport) { b, _ := json.Marshal(r); fmt.Printf("%s\n", b) }
		d, err := vdisplay.Create(v.Spec, v.Opts)
		if err != nil {
			say(childReport{Err: err.Error(), Done: true})
			return
		}
		say(childReport{ID: d.ID()})
		r := childReport{InProcess: d.WaitActive(8 * time.Second), Done: true}
		if r.InProcess {
			if err := d.ApplyMode(v.Spec); err != nil {
				r.ModeErr = err.Error()
			}
			time.Sleep(500 * time.Millisecond)
			if m, ok := vdisplay.CurrentMode(d.ID()); ok {
				r.Mode = m.String()
			}
		}
		say(r)
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = d.Close()
	}()
	objc.RunAppLoop(1, done.Load)
	return 0
}

func activeIDs(exe string) []uint32 {
	out, err := exec.Command(exe, "-list").Output()
	if err != nil {
		return nil
	}
	var ids []uint32
	for line := range strings.Lines(string(out)) {
		var id uint32
		if _, err := fmt.Sscan(line, &id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func runMatrix(only int) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Printf("macOS virtual display test — %s\n", time.Now().Format(time.DateTime))
	fmt.Println("Each variant shows a test display for a few seconds.")
	vs := variants()
	worked := false
	for i, v := range vs {
		if only > 0 && i+1 != only {
			continue
		}
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(vs), v.Name)
		if runOne(exe, v) {
			worked = true
		}
	}
	if !worked {
		fmt.Println("\nNo variant produced an active display.")
		return 1
	}
	return 0
}

func runOne(exe string, v variant) bool {
	arg, _ := json.Marshal(v)
	cmd := exec.Command(exe, "-child", string(arg))
	cmd.Stderr = os.Stderr
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		fmt.Println("  start failed:", err)
		return false
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	}()

	reports := make(chan childReport, 4)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var r childReport
			if json.Unmarshal(sc.Bytes(), &r) == nil {
				reports <- r
			}
		}
		close(reports)
	}()

	first, ok := <-reports
	if !ok || first.Err != "" {
		fmt.Printf("  create failed: %s\n", first.Err)
		return false
	}
	fmt.Printf("  created display %d\n", first.ID)

	external := false
	start := time.Now()
	var final childReport
	gotFinal := false
	for time.Since(start) < 10*time.Second && (!gotFinal || !external) {
		if !external && slices.Contains(activeIDs(exe), first.ID) {
			external = true
			fmt.Printf("  other processes see it after %s\n", time.Since(start).Round(100*time.Millisecond))
		}
		select {
		case r, ok := <-reports:
			if ok && r.Done {
				final, gotFinal = r, true
			}
		case <-time.After(300 * time.Millisecond):
		}
	}
	if !external {
		fmt.Println("  other processes never saw it")
	}
	switch {
	case !gotFinal:
		fmt.Println("  creating process: no answer")
	case final.InProcess:
		fmt.Printf("  creating process sees it; mode %s", final.Mode)
		if final.ModeErr != "" {
			fmt.Printf(" (mode switch failed: %s)", final.ModeErr)
		}
		fmt.Println()
	default:
		fmt.Println("  creating process never saw it active")
	}
	verdict := "FAIL"
	if external {
		verdict = "WORKS"
	}
	fmt.Println("  =>", verdict)

	_ = stdin.Close()
	for t := time.Now(); time.Since(t) < 6*time.Second; time.Sleep(300 * time.Millisecond) {
		if !slices.Contains(activeIDs(exe), first.ID) {
			break
		}
	}
	return external
}

func onAppLoop(f func() int) int {
	objc.App()
	var done atomic.Bool
	var code atomic.Int32
	go func() {
		runtime.LockOSThread()
		code.Store(int32(f()))
		done.Store(true)
	}()
	objc.RunAppLoop(1, done.Load)
	return int(code.Load())
}

func runOffChild(arg string, mount bool) int {
	var ids []uint32
	if err := json.Unmarshal([]byte(arg), &ids); err != nil {
		fmt.Println("bad screen list:", err)
		return 2
	}
	return onAppLoop(func() int {
		if mount {
			ipad, _ := specs()
			d, err := vdisplay.Open(ipad)
			if err != nil {
				fmt.Println("  child: could not create the test display:", err)
				return 1
			}
			fmt.Printf("  child: test display %d is up\n", d.ID())
		}
		scope, err := vdisplay.TurnOff(ids)
		if err != nil {
			fmt.Println("  child: could not turn them off:", err)
			return 1
		}
		fmt.Printf("  child: turned off %v %s; now crashing\n", ids, scope)
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		return 0
	})
}

func screensOn(ids []uint32) bool {
	active := vdisplay.ActiveDisplays()
	for _, id := range ids {
		if !slices.Contains(active, id) {
			return false
		}
	}
	return true
}

func runScreens() int {
	fmt.Printf("Screens-off test — %s\n", time.Now().Format(time.DateTime))
	ids := vdisplay.OtherDisplays()
	if len(ids) == 0 {
		fmt.Println("No other screens are on.")
		return 1
	}
	fmt.Printf("Other screens: %v\n", ids)
	ipad, _ := specs()
	d, err := vdisplay.Open(ipad)
	if err != nil {
		fmt.Println("Could not create the test display:", err)
		return 1
	}
	defer func() { _ = d.Close() }()
	fmt.Printf("Test display %d is up; it stays up for the whole test.\n", d.ID())
	failed := false

	fmt.Println("\n[1/2] Turn off and back on, normally")
	start := time.Now()
	scope, err := vdisplay.TurnOff(ids)
	switch {
	case err != nil:
		fmt.Println("  turning off failed:", err)
		failed = true
	default:
		fmt.Printf("  off after %s, %s\n", time.Since(start).Round(100*time.Millisecond), scope)
		fmt.Printf("  active now: %v\n", vdisplay.ActiveDisplays())
		time.Sleep(4 * time.Second)
		start = time.Now()
		if err := vdisplay.TurnOn(ids, scope); err != nil {
			fmt.Println("  turning back on failed:", err)
			failed = true
		} else {
			fmt.Printf("  back on after %s\n", time.Since(start).Round(100*time.Millisecond))
		}
	}
	if !screensOn(ids) {
		_ = vdisplay.TurnOn(ids, vdisplay.ScopeSession)
	}
	time.Sleep(2 * time.Second)

	fmt.Println("\n[2/2] A process turns them off and is killed")
	exe, _ := os.Executable()
	arg, _ := json.Marshal(ids)
	cmd := exec.Command(exe, "-screens-child", string(arg))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	_ = cmd.Run()
	start = time.Now()
	for time.Since(start) < 6*time.Second && !screensOn(ids) {
		time.Sleep(100 * time.Millisecond)
	}
	if screensOn(ids) {
		fmt.Printf("  => macOS turned them back on by itself after %s\n", time.Since(start).Round(100*time.Millisecond))
	} else {
		fmt.Println("  => macOS left them off; turning them back on")
		if err := vdisplay.TurnOn(ids, vdisplay.ScopeSession); err != nil {
			fmt.Println("  turning back on failed:", err)
		}
	}
	fmt.Printf("\nActive at the end: %v\n", vdisplay.ActiveDisplays())
	if failed {
		return 1
	}
	return 0
}

func runAlone() int {
	fmt.Printf("No-screen-left test — %s\n", time.Now().Format(time.DateTime))
	ids := vdisplay.OtherDisplays()
	if all := vdisplay.ActiveDisplays(); len(ids) == 0 || len(all) != len(ids) {
		fmt.Printf("Active screens %v include virtual ones %v. Quit DisplayExt and try again.\n", all, ids)
		return 1
	}
	fmt.Printf("Screens: %v\n", ids)
	fmt.Println("A child mounts a test display, turns these off and is killed, leaving no screen on.")
	exe, _ := os.Executable()
	arg, _ := json.Marshal(ids)
	cmd := exec.Command(exe, "-screens-alone-child", string(arg))
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	_ = cmd.Run()
	start := time.Now()
	for time.Since(start) < 8*time.Second && !screensOn(ids) {
		time.Sleep(100 * time.Millisecond)
	}
	code := 0
	if screensOn(ids) {
		fmt.Printf("  => macOS turned them back on by itself after %s\n", time.Since(start).Round(100*time.Millisecond))
	} else {
		code = 1
		fmt.Printf("  => macOS left no screen on for 8s (active: %v); turning them back on\n", vdisplay.ActiveDisplays())
		if err := vdisplay.TurnOn(ids, vdisplay.ScopeSession); err != nil {
			fmt.Println("  turning back on failed:", err)
		}
	}
	fmt.Printf("Active at the end: %v\n", vdisplay.ActiveDisplays())
	return code
}

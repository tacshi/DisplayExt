//go:build darwin

package vdisplay

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/go-macos/objc"
)

const (
	coreGraphics = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"
	libObjC      = "/usr/lib/libobjc.A.dylib"
	libSystem    = "/usr/lib/libSystem.B.dylib"
)

type (
	cgPoint struct{ X, Y float64 }
	cgSize  struct{ W, H float64 }
)

var (
	whitePoint   = cgPoint{0.3127, 0.3290}
	redPrimary   = cgPoint{0.64, 0.33}
	greenPrimary = cgPoint{0.30, 0.60}
	bluePrimary  = cgPoint{0.15, 0.06}
)

var (
	loadOnce sync.Once
	loadErr  error

	mainQueue              uintptr
	dispatchQueueCreate    func(label string, attr uintptr) uintptr
	classGetInstanceMethod func(cls, sel uintptr) uintptr
	cgGetActiveDisplayList func(max uint32, ids *uint32, n *uint32) int32

	cgDisplayCopyAllDisplayModes  func(id uint32, options objc.ID) objc.ID
	cgDisplayCopyDisplayMode      func(id uint32) uintptr
	cgDisplayModeRelease          func(mode uintptr)
	cgDisplayModeGetWidth         func(mode uintptr) uint64
	cgDisplayModeGetHeight        func(mode uintptr) uint64
	cgDisplayModeGetPixelWidth    func(mode uintptr) uint64
	cgDisplayModeGetPixelHeight   func(mode uintptr) uint64
	cgDisplayModeGetRefreshRate   func(mode uintptr) float64
	cgDisplayVendorNumber         func(id uint32) uint32
	cgBeginDisplayConfiguration   func(config *uintptr) int32
	cgConfigureDisplayWithMode    func(config uintptr, id uint32, mode uintptr, options uintptr) int32
	cgCompleteDisplayConfig       func(config uintptr, option uint32) int32
	cgCancelDisplayConfiguration  func(config uintptr) int32
	cgsConfigureDisplayEnabled    func(config uintptr, id uint32, enabled bool) int32
	cgConfigureDisplayMirror      func(config uintptr, id uint32, master uint32) int32
	cgDisplayMirrorsDisplay       func(id uint32) uint32
	cgSetDisplayTransferByFormula func(id uint32, rMin, rMax, rGamma, gMin, gMax, gGamma, bMin, bMax, bGamma float32) int32
	cgDisplayRestoreColorSync     func()
	cgGetDisplayTransferByTable   func(id uint32, capacity uint32, r, g, b *float32, n *uint32) int32
)

const (
	cgConfigureForAppOnly  = 0
	cgConfigureForSession  = 1
	cgConfigurePermanently = 2
)

func load() error {
	loadOnce.Do(func() {
		if err := objc.Load(objc.Foundation, coreGraphics); err != nil {
			loadErr = err
			return
		}
		cg, err := purego.Dlopen(coreGraphics, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("vdisplay: dlopen CoreGraphics: %w", err)
			return
		}
		lo, err := purego.Dlopen(libObjC, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("vdisplay: dlopen libobjc: %w", err)
			return
		}
		sys, err := purego.Dlopen(libSystem, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("vdisplay: dlopen libSystem: %w", err)
			return
		}
		if mainQueue, err = purego.Dlsym(sys, "_dispatch_main_q"); err != nil {
			loadErr = fmt.Errorf("vdisplay: main dispatch queue: %w", err)
			return
		}
		purego.RegisterLibFunc(&dispatchQueueCreate, sys, "dispatch_queue_create")
		purego.RegisterLibFunc(&classGetInstanceMethod, lo, "class_getInstanceMethod")
		purego.RegisterLibFunc(&cgGetActiveDisplayList, cg, "CGGetActiveDisplayList")
		purego.RegisterLibFunc(&cgDisplayCopyAllDisplayModes, cg, "CGDisplayCopyAllDisplayModes")
		purego.RegisterLibFunc(&cgDisplayCopyDisplayMode, cg, "CGDisplayCopyDisplayMode")
		purego.RegisterLibFunc(&cgDisplayModeRelease, cg, "CGDisplayModeRelease")
		purego.RegisterLibFunc(&cgDisplayModeGetWidth, cg, "CGDisplayModeGetWidth")
		purego.RegisterLibFunc(&cgDisplayModeGetHeight, cg, "CGDisplayModeGetHeight")
		purego.RegisterLibFunc(&cgDisplayModeGetPixelWidth, cg, "CGDisplayModeGetPixelWidth")
		purego.RegisterLibFunc(&cgDisplayModeGetPixelHeight, cg, "CGDisplayModeGetPixelHeight")
		purego.RegisterLibFunc(&cgDisplayModeGetRefreshRate, cg, "CGDisplayModeGetRefreshRate")
		purego.RegisterLibFunc(&cgDisplayVendorNumber, cg, "CGDisplayVendorNumber")
		purego.RegisterLibFunc(&cgBeginDisplayConfiguration, cg, "CGBeginDisplayConfiguration")
		purego.RegisterLibFunc(&cgConfigureDisplayWithMode, cg, "CGConfigureDisplayWithDisplayMode")
		purego.RegisterLibFunc(&cgCompleteDisplayConfig, cg, "CGCompleteDisplayConfiguration")
		purego.RegisterLibFunc(&cgCancelDisplayConfiguration, cg, "CGCancelDisplayConfiguration")
		purego.RegisterLibFunc(&cgConfigureDisplayMirror, cg, "CGConfigureDisplayMirrorOfDisplay")
		purego.RegisterLibFunc(&cgDisplayMirrorsDisplay, cg, "CGDisplayMirrorsDisplay")
		purego.RegisterLibFunc(&cgSetDisplayTransferByFormula, cg, "CGSetDisplayTransferByFormula")
		purego.RegisterLibFunc(&cgDisplayRestoreColorSync, cg, "CGDisplayRestoreColorSyncSettings")
		purego.RegisterLibFunc(&cgGetDisplayTransferByTable, cg, "CGGetDisplayTransferByTable")
		if _, err := purego.Dlsym(cg, "CGSConfigureDisplayEnabled"); err == nil {
			purego.RegisterLibFunc(&cgsConfigureDisplayEnabled, cg, "CGSConfigureDisplayEnabled")
		}
	})
	return loadErr
}

var required = map[string][]string{
	"CGVirtualDisplayDescriptor": {"init", "setName:", "setMaxPixelsWide:", "setMaxPixelsHigh:",
		"setSizeInMillimeters:", "setSerialNum:", "setProductID:", "setVendorID:", "setQueue:",
		"setWhitePoint:", "setRedPrimary:", "setGreenPrimary:", "setBluePrimary:"},
	"CGVirtualDisplay":         {"initWithDescriptor:", "applySettings:", "displayID"},
	"CGVirtualDisplaySettings": {"init", "setModes:", "setHiDPI:"},
	"CGVirtualDisplayMode":     {"initWithWidth:height:refreshRate:", "initWithWidth:height:refreshRate:transferFunction:"},
}

func Available() error {
	if err := load(); err != nil {
		return err
	}
	for class, sels := range required {
		cls := uintptr(objc.GetClass(class))
		if cls == 0 {
			return fmt.Errorf("vdisplay: class %s is missing from this macOS", class)
		}
		for _, sel := range sels {
			if classGetInstanceMethod(cls, uintptr(objc.Sel(sel))) == 0 {
				return fmt.Errorf("vdisplay: %s does not respond to %s on this macOS", class, sel)
			}
		}
	}
	return nil
}

type Display struct {
	mu  sync.Mutex
	obj objc.ID
	id  uint32

	ModeErr error
}

func (d *Display) ID() uint32 { return d.id }

func (d *Display) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.obj != 0 {
		d.obj.Send(objc.Sel("release"))
		d.obj = 0
	}
	return nil
}

func Open(s Spec) (*Display, error) { return OpenWith(s, DefaultOptions) }

func OpenWith(s Spec, o Options) (*Display, error) {
	d, err := Create(s, o)
	if err != nil {
		return nil, err
	}
	timeout := o.ActiveTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	if !d.WaitActive(timeout) {
		_ = d.Close()
		return nil, fmt.Errorf("vdisplay: display %d was created but macOS did not activate it within %s", d.id, timeout)
	}
	d.ModeErr = d.ApplyMode(s)
	return d, nil
}

func Create(s Spec, o Options) (*Display, error) {
	if err := Available(); err != nil {
		return nil, err
	}
	return create(s, o)
}

func (d *Display) WaitActive(timeout time.Duration) bool { return waitActive(d.id, timeout) }

func (d *Display) ApplyMode(s Spec) error { return applyMode(d.id, s) }

func modeOf(m uintptr) Mode {
	return Mode{
		PointsW:   int(cgDisplayModeGetWidth(m)),
		PointsH:   int(cgDisplayModeGetHeight(m)),
		PixelsW:   int(cgDisplayModeGetPixelWidth(m)),
		PixelsH:   int(cgDisplayModeGetPixelHeight(m)),
		RefreshHz: cgDisplayModeGetRefreshRate(m),
	}
}

func CurrentMode(id uint32) (Mode, bool) {
	if load() != nil {
		return Mode{}, false
	}
	m := cgDisplayCopyDisplayMode(id)
	if m == 0 {
		return Mode{}, false
	}
	defer cgDisplayModeRelease(m)
	return modeOf(m), true
}

func OtherModes() map[uint32]Mode {
	out := map[uint32]Mode{}
	for _, id := range OtherDisplays() {
		if m, ok := CurrentMode(id); ok {
			out[id] = m
		}
	}
	return out
}

func OtherDisplays() []uint32 {
	var out []uint32
	for _, id := range ActiveDisplays() {
		if cgDisplayVendorNumber(id) != VendorID {
			out = append(out, id)
		}
	}
	return out
}

func setEnabled(ids []uint32, on bool, scope Scope) error {
	if err := load(); err != nil {
		return err
	}
	if cgsConfigureDisplayEnabled == nil {
		return fmt.Errorf("vdisplay: this macOS cannot turn screens off")
	}
	if len(ids) == 0 {
		return nil
	}
	var cfg uintptr
	if e := cgBeginDisplayConfiguration(&cfg); e != 0 {
		return fmt.Errorf("CGBeginDisplayConfiguration: CGError %d", e)
	}
	for _, id := range ids {
		if e := cgsConfigureDisplayEnabled(cfg, id, on); e != 0 {
			cgCancelDisplayConfiguration(cfg)
			return fmt.Errorf("CGSConfigureDisplayEnabled(%d, %t): CGError %d", id, on, e)
		}
	}
	opt := uint32(cgConfigureForSession)
	if scope == ScopeApp {
		opt = cgConfigureForAppOnly
	}
	if e := cgCompleteDisplayConfig(cfg, opt); e != 0 {
		return fmt.Errorf("CGCompleteDisplayConfiguration: CGError %d", e)
	}
	return nil
}

func setMirror(ids []uint32, master uint32, scope Scope) error {
	if err := load(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	var cfg uintptr
	if e := cgBeginDisplayConfiguration(&cfg); e != 0 {
		return fmt.Errorf("CGBeginDisplayConfiguration: CGError %d", e)
	}
	for _, id := range ids {
		if e := cgConfigureDisplayMirror(cfg, id, master); e != 0 {
			cgCancelDisplayConfiguration(cfg)
			return fmt.Errorf("CGConfigureDisplayMirrorOfDisplay(%d, %d): CGError %d", id, master, e)
		}
	}
	opt := uint32(cgConfigureForSession)
	if scope == ScopeApp {
		opt = cgConfigureForAppOnly
	}
	if e := cgCompleteDisplayConfig(cfg, opt); e != 0 {
		return fmt.Errorf("CGCompleteDisplayConfiguration: CGError %d", e)
	}
	return nil
}

func MirrorMaster(id uint32) uint32 {
	if load() != nil {
		return 0
	}
	return cgDisplayMirrorsDisplay(id)
}

func Blackout(ids []uint32) error {
	if err := load(); err != nil {
		return err
	}
	for _, id := range ids {
		if e := cgSetDisplayTransferByFormula(id, 0, 0, 1, 0, 0, 1, 0, 0, 1); e != 0 {
			return fmt.Errorf("CGSetDisplayTransferByFormula(%d): CGError %d", id, e)
		}
	}
	return nil
}

func isBlack(id uint32) bool {
	if load() != nil {
		return false
	}
	var r, g, b [256]float32
	var n uint32
	if cgGetDisplayTransferByTable(id, uint32(len(r)), &r[0], &g[0], &b[0], &n) != 0 || n == 0 {
		return false
	}
	return r[n-1] == 0 && g[n-1] == 0 && b[n-1] == 0
}

func RestoreColors() {
	if load() == nil {
		cgDisplayRestoreColorSync()
	}
}

func SetMode(id uint32, m Mode) error {
	if err := load(); err != nil {
		return err
	}
	return setMode(id, func(c Mode) bool {
		return c.PointsW == m.PointsW && c.PointsH == m.PointsH && c.PixelsW == m.PixelsW && c.PixelsH == m.PixelsH
	}, m.RefreshHz)
}

func create(s Spec, o Options) (*Display, error) {
	var (
		d   *Display
		err error
	)
	objc.AutoreleasePool(func() {
		desc := objc.ClassID("CGVirtualDisplayDescriptor").Send(objc.Sel("alloc")).Send(objc.Sel("init"))
		if desc == 0 {
			err = fmt.Errorf("vdisplay: could not allocate a descriptor")
			return
		}
		defer desc.Send(objc.Sel("release"))
		desc.Send(objc.Sel("setName:"), objc.NSString(s.Name))
		maxW, maxH := s.PixelsW, s.PixelsH
		if s.MaxPixelsW > 0 && s.MaxPixelsH > 0 {
			maxW, maxH = s.MaxPixelsW, s.MaxPixelsH
		}
		desc.Send(objc.Sel("setMaxPixelsWide:"), uint32(maxW))
		desc.Send(objc.Sel("setMaxPixelsHigh:"), uint32(maxH))
		objc.Send[objc.ID](desc, objc.Sel("setSizeInMillimeters:"), cgSize{s.SizeMMW, s.SizeMMH})
		desc.Send(objc.Sel("setVendorID:"), uint32(VendorID))
		desc.Send(objc.Sel("setProductID:"), uint32(ProductID))
		desc.Send(objc.Sel("setSerialNum:"), s.Serial)
		queue := mainQueue
		if o.PrivateQueue {
			queue = dispatchQueueCreate(fmt.Sprintf("displayext.vdisplay.%d", s.Serial), 0)
		}
		desc.Send(objc.Sel("setQueue:"), queue)
		if !o.NoColor {
			objc.Send[objc.ID](desc, objc.Sel("setWhitePoint:"), whitePoint)
			objc.Send[objc.ID](desc, objc.Sel("setRedPrimary:"), redPrimary)
			objc.Send[objc.ID](desc, objc.Sel("setGreenPrimary:"), greenPrimary)
			objc.Send[objc.ID](desc, objc.Sel("setBluePrimary:"), bluePrimary)
		}

		disp := objc.ClassID("CGVirtualDisplay").Send(objc.Sel("alloc")).Send(objc.Sel("initWithDescriptor:"), desc)
		if disp == 0 {
			err = fmt.Errorf("vdisplay: the WindowServer would not create a virtual display")
			return
		}

		settings := newSettings(s, o)
		defer settings.Send(objc.Sel("release"))
		if !objc.Send[bool](disp, objc.Sel("applySettings:"), settings) {
			disp.Send(objc.Sel("release"))
			err = fmt.Errorf("vdisplay: the WindowServer rejected the settings for a %d×%d display", s.PixelsW, s.PixelsH)
			return
		}
		d = &Display{obj: disp, id: objc.Send[uint32](disp, objc.Sel("displayID"))}
	})
	return d, err
}

func newSettings(s Spec, o Options) objc.ID {
	settings := objc.ClassID("CGVirtualDisplaySettings").Send(objc.Sel("alloc")).Send(objc.Sel("init"))
	modes := objc.ClassID("NSMutableArray").Send(objc.Sel("array"))
	for _, m := range s.Modes() {
		alloc := objc.ClassID("CGVirtualDisplayMode").Send(objc.Sel("alloc"))
		var mode objc.ID
		if o.TransferFunction > 0 {
			mode = objc.Send[objc.ID](alloc, objc.Sel("initWithWidth:height:refreshRate:transferFunction:"),
				uint32(m.W), uint32(m.H), float64(s.RefreshHz), uint32(o.TransferFunction-1))
		} else {
			mode = objc.Send[objc.ID](alloc, objc.Sel("initWithWidth:height:refreshRate:"),
				uint32(m.W), uint32(m.H), float64(s.RefreshHz))
		}
		modes.Send(objc.Sel("addObject:"), mode)
		mode.Send(objc.Sel("release"))
	}
	settings.Send(objc.Sel("setModes:"), modes)
	var hidpi uint32
	if s.HiDPI {
		hidpi = 1
	}
	settings.Send(objc.Sel("setHiDPI:"), hidpi)
	return settings
}

func (d *Display) Reapply(s Spec, o Options) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.obj == 0 {
		return fmt.Errorf("vdisplay: display %d is closed", d.id)
	}
	var ok bool
	objc.AutoreleasePool(func() {
		settings := newSettings(s, o)
		defer settings.Send(objc.Sel("release"))
		ok = objc.Send[bool](d.obj, objc.Sel("applySettings:"), settings)
	})
	if !ok {
		return fmt.Errorf("vdisplay: the WindowServer rejected new settings for display %d", d.id)
	}
	return nil
}

func waitActive(id uint32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if IsActive(id) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		_ = objc.PumpRunLoop(0.025)
		time.Sleep(25 * time.Millisecond)
	}
}

func ActiveDisplays() []uint32 {
	if load() != nil {
		return nil
	}
	ids := make([]uint32, 64)
	var n uint32
	if cgGetActiveDisplayList(uint32(len(ids)), &ids[0], &n) != 0 {
		return nil
	}
	return ids[:n]
}

func IsActive(id uint32) bool { return slices.Contains(ActiveDisplays(), id) }

func applyMode(id uint32, s Spec) error {
	scale := 1
	if s.HiDPI {
		scale = 2
	}
	want := Mode{PointsW: s.PointsW, PointsH: s.PointsH, PixelsW: s.PointsW * scale, PixelsH: s.PointsH * scale, RefreshHz: float64(s.RefreshHz)}
	if cur, ok := CurrentMode(id); ok && cur.Same(want) {
		return nil
	}
	if err := SetMode(id, want); err != nil {
		return fmt.Errorf("vdisplay: display %d: %w", id, err)
	}
	return nil
}

func setMode(id uint32, match func(Mode) bool, hz float64) error {
	var list objc.ID
	objc.AutoreleasePool(func() {
		opts := objc.ClassID("NSDictionary").Send(objc.Sel("dictionaryWithObject:forKey:"),
			objc.ClassID("NSNumber").Send(objc.Sel("numberWithBool:"), true),
			objc.NSString("kCGDisplayResolution"))

		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			if list = cgDisplayCopyAllDisplayModes(id, opts); list != 0 && list.Send(objc.Sel("count")) > 0 {
				return
			}
			if list != 0 {
				list.Send(objc.Sel("release"))
				list = 0
			}
			_ = objc.PumpRunLoop(0.05)
			time.Sleep(50 * time.Millisecond)
		}
	})
	if list == 0 {
		return fmt.Errorf("reports no modes")
	}
	defer list.Send(objc.Sel("release"))

	var best uintptr
	bestDiff := math.Inf(1)
	n := int(list.Send(objc.Sel("count")))
	for i := range n {
		m := uintptr(list.Send(objc.Sel("objectAtIndex:"), i))
		mode := modeOf(m)
		if !match(mode) {
			continue
		}
		if diff := math.Abs(mode.RefreshHz - hz); diff < bestDiff {
			best, bestDiff = m, diff
		}
	}
	if best == 0 {
		return fmt.Errorf("does not offer that mode")
	}
	var cfg uintptr
	if e := cgBeginDisplayConfiguration(&cfg); e != 0 {
		return fmt.Errorf("CGBeginDisplayConfiguration: CGError %d", e)
	}
	if e := cgConfigureDisplayWithMode(cfg, id, best, 0); e != 0 {
		cgCancelDisplayConfiguration(cfg)
		return fmt.Errorf("CGConfigureDisplayWithDisplayMode: CGError %d", e)
	}
	if e := cgCompleteDisplayConfig(cfg, cgConfigurePermanently); e != 0 {
		return fmt.Errorf("CGCompleteDisplayConfiguration: CGError %d", e)
	}
	return nil
}

func ScreenName(id uint32) string {
	if load() != nil || objc.Load(objc.AppKit) != nil {
		return ""
	}
	var name string
	objc.AutoreleasePool(func() {
		screens := objc.ClassID("NSScreen").Send(objc.Sel("screens"))
		key := objc.NSString("NSScreenNumber")
		n := int(screens.Send(objc.Sel("count")))
		for i := range n {
			screen := screens.Send(objc.Sel("objectAtIndex:"), i)
			number := screen.Send(objc.Sel("deviceDescription")).Send(objc.Sel("objectForKey:"), key)
			if number == 0 || uint32(number.Send(objc.Sel("unsignedIntValue"))) != id {
				continue
			}
			if screen.Send(objc.Sel("respondsToSelector:"), objc.Sel("localizedName")) != 0 {
				name = objc.GoString(screen.Send(objc.Sel("localizedName")))
			}
			return
		}
	})
	return name
}

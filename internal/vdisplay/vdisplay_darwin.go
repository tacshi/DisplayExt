//go:build darwin

package vdisplay

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"displayext/internal/config"

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

	cgDisplayCopyAllDisplayModes func(id uint32, options objc.ID) objc.ID
	cgDisplayCopyDisplayMode     func(id uint32) uintptr
	cgDisplayModeRelease         func(mode uintptr)
	cgDisplayModeGetWidth        func(mode uintptr) uint64
	cgDisplayModeGetHeight       func(mode uintptr) uint64
	cgDisplayModeGetPixelWidth   func(mode uintptr) uint64
	cgDisplayModeGetPixelHeight  func(mode uintptr) uint64
	cgDisplayModeGetRefreshRate  func(mode uintptr) float64
	cgDisplaySetDisplayMode      func(id uint32, mode uintptr, options uintptr) int32
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
		purego.RegisterLibFunc(&cgDisplaySetDisplayMode, cg, "CGDisplaySetDisplayMode")
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

type Mode struct {
	PointsW, PointsH int
	PixelsW, PixelsH int
	RefreshHz        float64
}

func (m Mode) String() string {
	return fmt.Sprintf("%d×%d (%d×%d px) @%gHz", m.PointsW, m.PointsH, m.PixelsW, m.PixelsH, m.RefreshHz)
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
	return Mode{
		PointsW:   int(cgDisplayModeGetWidth(m)),
		PointsH:   int(cgDisplayModeGetHeight(m)),
		PixelsW:   int(cgDisplayModeGetPixelWidth(m)),
		PixelsH:   int(cgDisplayModeGetPixelHeight(m)),
		RefreshHz: cgDisplayModeGetRefreshRate(m),
	}, true
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
		desc.Send(objc.Sel("setMaxPixelsWide:"), uint32(s.PixelsW))
		desc.Send(objc.Sel("setMaxPixelsHigh:"), uint32(s.PixelsH))
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

		settings := objc.ClassID("CGVirtualDisplaySettings").Send(objc.Sel("alloc")).Send(objc.Sel("init"))
		defer settings.Send(objc.Sel("release"))
		modes := objc.ClassID("NSMutableArray").Send(objc.Sel("array"))
		sizes := append([]config.Size{{W: s.PixelsW, H: s.PixelsH}}, s.Extra...)
		for _, m := range slices.Compact(sizes) {
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
		if !objc.Send[bool](disp, objc.Sel("applySettings:"), settings) {
			disp.Send(objc.Sel("release"))
			err = fmt.Errorf("vdisplay: the WindowServer rejected the settings for a %d×%d display", s.PixelsW, s.PixelsH)
			return
		}
		d = &Display{obj: disp, id: objc.Send[uint32](disp, objc.Sel("displayID"))}
	})
	return d, err
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
	want := func(mode uintptr) bool {
		return int(cgDisplayModeGetWidth(mode)) == s.PointsW &&
			int(cgDisplayModeGetHeight(mode)) == s.PointsH &&
			int(cgDisplayModeGetPixelWidth(mode)) == s.PointsW*scale &&
			int(cgDisplayModeGetPixelHeight(mode)) == s.PointsH*scale
	}
	if cur := cgDisplayCopyDisplayMode(id); cur != 0 {
		ok := want(cur) && math.Abs(cgDisplayModeGetRefreshRate(cur)-float64(s.RefreshHz)) < 0.5
		cgDisplayModeRelease(cur)
		if ok {
			return nil
		}
	}

	var list objc.ID
	objc.AutoreleasePool(func() {
		opts := objc.ClassID("NSDictionary").Send(objc.Sel("dictionaryWithObject:forKey:"),
			objc.ClassID("NSNumber").Send(objc.Sel("numberWithBool:"), true),
			objc.NSString("kCGDisplayShowDuplicateLowResolutionModes"))

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
		return fmt.Errorf("vdisplay: display %d reports no modes", id)
	}
	defer list.Send(objc.Sel("release"))

	var best uintptr
	bestDiff := math.Inf(1)
	n := int(list.Send(objc.Sel("count")))
	for i := range n {
		m := uintptr(list.Send(objc.Sel("objectAtIndex:"), i))
		if !want(m) {
			continue
		}
		if diff := math.Abs(cgDisplayModeGetRefreshRate(m) - float64(s.RefreshHz)); diff < bestDiff {
			best, bestDiff = m, diff
		}
	}
	if best == 0 {
		return fmt.Errorf("vdisplay: display %d does not offer %d×%d (HiDPI %v)", id, s.PointsW, s.PointsH, s.HiDPI)
	}
	if e := cgDisplaySetDisplayMode(id, best, 0); e != 0 {
		return fmt.Errorf("vdisplay: CGDisplaySetDisplayMode: CGError %d", e)
	}
	return nil
}

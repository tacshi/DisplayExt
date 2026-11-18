//go:build darwin

package uuwindow

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/go-macos/objc"
)

const (
	appServices = "/System/Library/Frameworks/ApplicationServices.framework/ApplicationServices"
	coreFound   = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"

	maxDepth = 25
	maxNodes = 2048
	timeout  = 1
)

var (
	loadOnce sync.Once
	loadErr  error

	axIsProcessTrusted            func() bool
	axIsProcessTrustedWithOptions func(options uintptr) bool
	axCreateApplication           func(pid int32) uintptr
	axCopyAttributeValue          func(el, attr uintptr, out *uintptr) int32
	axSetMessagingTimeout         func(el uintptr, seconds float32) int32
	axElementTypeID               func() uintptr
	cfGetTypeID                   func(v uintptr) uintptr
	cfStringTypeID                func() uintptr
	cfEqual                       func(a, b uintptr) bool
	cfRelease                     func(v uintptr)
	cfRetain                      func(v uintptr) uintptr
)

func load() error {
	loadOnce.Do(func() {
		if err := objc.Load(objc.Foundation); err != nil {
			loadErr = err
			return
		}
		ax, err := purego.Dlopen(appServices, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("uuwindow: dlopen ApplicationServices: %w", err)
			return
		}
		cf, err := purego.Dlopen(coreFound, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("uuwindow: dlopen CoreFoundation: %w", err)
			return
		}
		purego.RegisterLibFunc(&axIsProcessTrusted, ax, "AXIsProcessTrusted")
		purego.RegisterLibFunc(&axIsProcessTrustedWithOptions, ax, "AXIsProcessTrustedWithOptions")
		purego.RegisterLibFunc(&axCreateApplication, ax, "AXUIElementCreateApplication")
		purego.RegisterLibFunc(&axCopyAttributeValue, ax, "AXUIElementCopyAttributeValue")
		purego.RegisterLibFunc(&axSetMessagingTimeout, ax, "AXUIElementSetMessagingTimeout")
		purego.RegisterLibFunc(&axElementTypeID, ax, "AXUIElementGetTypeID")
		purego.RegisterLibFunc(&cfGetTypeID, cf, "CFGetTypeID")
		purego.RegisterLibFunc(&cfStringTypeID, cf, "CFStringGetTypeID")
		purego.RegisterLibFunc(&cfEqual, cf, "CFEqual")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&cfRetain, cf, "CFRetain")
	})
	return loadErr
}

func Trusted() bool {
	return load() == nil && axIsProcessTrusted()
}

func AskForTrust() bool {
	if load() != nil {
		return false
	}
	var trusted bool
	objc.AutoreleasePool(func() {
		yes := objc.ClassID("NSNumber").Send(objc.Sel("numberWithBool:"), true)
		opts := objc.ClassID("NSDictionary").Send(objc.Sel("dictionaryWithObject:forKey:"), yes, objc.NSString("AXTrustedCheckOptionPrompt"))
		trusted = axIsProcessTrustedWithOptions(uintptr(opts))
	})
	return trusted
}

var (
	ErrNotTrusted = errors.New("uuwindow: DisplayExt is not allowed to use Accessibility")
	ErrNoWindows  = errors.New("uuwindow: UU远程 has no windows")
	errTooLarge   = errors.New("uuwindow: UU远程's window is too large to read")
)

func Read(pid int32) ([]Element, error) {
	if err := load(); err != nil {
		return nil, err
	}
	if !axIsProcessTrusted() {
		return nil, ErrNotTrusted
	}
	var (
		out []Element
		err error
	)
	objc.AutoreleasePool(func() {
		app := axCreateApplication(pid)
		if app == 0 {
			err = fmt.Errorf("uuwindow: no Accessibility element for pid %d", pid)
			return
		}
		defer cfRelease(app)
		axSetMessagingTimeout(app, timeout)
		wins := windows(app)
		defer func() {
			for _, w := range wins {
				cfRelease(w)
			}
		}()
		if len(wins) == 0 {
			err = ErrNoWindows
			return
		}
		w := walk{}
		for _, win := range wins {
			e, ok, werr := w.snapshot(win)
			if werr != nil {
				err = werr
				return
			}
			if ok {
				out = append(out, e)
			}
		}
	})
	return out, err
}

func windows(app uintptr) []uintptr {
	var out []uintptr
	add := func(el uintptr) {
		for _, w := range out {
			if cfEqual(w, el) {
				return
			}
		}
		out = append(out, cfRetain(el))
	}
	if list, ok := copyAttr(app, "AXWindows"); ok {
		forEach(list, add)
		cfRelease(list)
	}
	for _, key := range []string{"AXMainWindow", "AXFocusedWindow"} {
		el, ok := copyAttr(app, key)
		if !ok {
			continue
		}
		if cfGetTypeID(el) == axElementTypeID() && stringAttr(el, "AXRole") == "AXWindow" {
			add(el)
		}
		cfRelease(el)
	}
	return out
}

type walk struct {
	path  []uintptr
	count int
}

func (w *walk) snapshot(el uintptr) (Element, bool, error) {
	for _, p := range w.path {
		if cfEqual(p, el) {
			return Element{}, false, nil
		}
	}
	w.count++
	if len(w.path) >= maxDepth || w.count > maxNodes {
		return Element{}, false, errTooLarge
	}
	w.path = append(w.path, el)
	defer func() { w.path = w.path[:len(w.path)-1] }()

	e := Element{Kind: Other}
	switch stringAttr(el, "AXRole") {
	case "AXStaticText":
		if v, ok := stringValue(el); ok {
			e.Kind, e.Text = Text, v
		}
	case "AXTable":
		e.Kind = Table
	case "AXRow":
		e.Kind = Row
	}
	list, ok := copyAttr(el, "AXChildren")
	if !ok {
		return e, true, nil
	}
	defer cfRelease(list)
	var err error
	forEach(list, func(child uintptr) {
		if err != nil {
			return
		}
		c, keep, cerr := w.snapshot(child)
		if cerr != nil {
			err = cerr
			return
		}
		if keep {
			e.Children = append(e.Children, c)
		}
	})
	return e, true, err
}

func copyAttr(el uintptr, name string) (uintptr, bool) {
	var out uintptr
	if axCopyAttributeValue(el, uintptr(objc.NSString(name)), &out) != 0 || out == 0 {
		return 0, false
	}
	return out, true
}

func stringAttr(el uintptr, name string) string {
	v, ok := copyAttr(el, name)
	if !ok {
		return ""
	}
	defer cfRelease(v)
	if cfGetTypeID(v) != cfStringTypeID() {
		return ""
	}
	return objc.GoString(objc.ID(v))
}

func stringValue(el uintptr) (string, bool) {
	v, ok := copyAttr(el, "AXValue")
	if !ok {
		return "", false
	}
	defer cfRelease(v)
	if cfGetTypeID(v) != cfStringTypeID() {
		return "", false
	}
	return objc.GoString(objc.ID(v)), true
}

func forEach(list uintptr, fn func(uintptr)) {
	arr := objc.ID(list)
	n := int(arr.Send(objc.Sel("count")))
	for i := range n {
		fn(uintptr(arr.Send(objc.Sel("objectAtIndex:"), i)))
	}
}

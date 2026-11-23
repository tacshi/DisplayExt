//go:build darwin

package uudetect

import (
	"encoding/binary"
	"fmt"
	"slices"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/go-macos/objc"
)

const (
	iokitPath  = "/System/Library/Frameworks/IOKit.framework/IOKit"
	libSystem  = "/usr/lib/libSystem.B.dylib"
	assertName = "AssertName"

	ctlKern       = 1
	kernArgMax    = 8
	kernProcArgs2 = 49
)

var (
	loadOnce sync.Once
	loadErr  error

	iopmCopyAssertionsByProcess func(out *uintptr) int32
	procListAllPIDs             func(buf *int32, size int32) int32
	procName                    func(pid int32, buf *byte, size uint32) int32
	sysctl                      func(name *int32, namelen uint32, oldp *byte, oldlenp *uintptr, newp *byte, newlen uintptr) int32

	argMu  sync.Mutex
	argBuf []byte
)

func load() error {
	loadOnce.Do(func() {
		if err := objc.Load(objc.Foundation); err != nil {
			loadErr = err
			return
		}
		iokit, err := purego.Dlopen(iokitPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("uudetect: dlopen IOKit: %w", err)
			return
		}
		sys, err := purego.Dlopen(libSystem, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			loadErr = fmt.Errorf("uudetect: dlopen libSystem: %w", err)
			return
		}
		purego.RegisterLibFunc(&iopmCopyAssertionsByProcess, iokit, "IOPMCopyAssertionsByProcess")
		purego.RegisterLibFunc(&procListAllPIDs, sys, "proc_listallpids")
		purego.RegisterLibFunc(&procName, sys, "proc_name")
		purego.RegisterLibFunc(&sysctl, sys, "sysctl")
	})
	return loadErr
}

func TakeSnapshot(process string) (Snapshot, error) { return takeSnapshot(process, "") }

func (r Rule) Snapshot() (Snapshot, error) { return takeSnapshot(r.Process, r.Arg) }

func takeSnapshot(process, arg string) (Snapshot, error) {
	if err := load(); err != nil {
		return Snapshot{}, err
	}
	snap := Snapshot{Processes: map[int32]string{}, Assertions: map[int32][]string{}}
	for _, pid := range allPIDs() {
		if name := nameOf(pid); name == process && (arg == "" || hasArg(pid, arg)) {
			snap.Processes[pid] = name
		}
	}
	if len(snap.Processes) == 0 {
		return snap, nil
	}

	var dict uintptr
	if ret := iopmCopyAssertionsByProcess(&dict); ret != 0 {
		return Snapshot{}, fmt.Errorf("uudetect: IOPMCopyAssertionsByProcess: IOReturn %#x", uint32(ret))
	}
	if dict == 0 {
		return snap, nil
	}
	byPID := objc.ID(dict)
	defer byPID.Send(objc.Sel("release"))

	objc.AutoreleasePool(func() {
		key := objc.NSString(assertName)
		for pid := range snap.Processes {
			num := objc.ClassID("NSNumber").Send(objc.Sel("numberWithInt:"), pid)
			list := byPID.Send(objc.Sel("objectForKey:"), num)
			if list == 0 {
				continue
			}
			n := int(list.Send(objc.Sel("count")))
			for i := range n {
				a := list.Send(objc.Sel("objectAtIndex:"), i)
				if name := a.Send(objc.Sel("objectForKey:"), key); name != 0 {
					snap.Assertions[pid] = append(snap.Assertions[pid], objc.GoString(name))
				}
			}
		}
	})
	return snap, nil
}

func allPIDs() []int32 {
	n := procListAllPIDs(nil, 0)
	if n <= 0 {
		return nil
	}
	buf := make([]int32, n+64)
	got := procListAllPIDs(&buf[0], int32(len(buf)*4))
	if got <= 0 {
		return nil
	}
	return buf[:min(int(got), len(buf))]
}

func nameOf(pid int32) string {
	var buf [256]byte
	n := procName(pid, &buf[0], uint32(len(buf)))
	if n <= 0 {
		return ""
	}
	return string(buf[:n])
}

func hasArg(pid int32, arg string) bool {
	argMu.Lock()
	defer argMu.Unlock()
	if argBuf == nil {
		argBuf = make([]byte, argMax())
	}
	mib := [3]int32{ctlKern, kernProcArgs2, pid}
	size := uintptr(len(argBuf))
	if sysctl(&mib[0], uint32(len(mib)), &argBuf[0], &size, nil, 0) != 0 {
		return false
	}
	args := ParseProcArgs(argBuf[:min(int(size), len(argBuf))])
	return len(args) > 1 && slices.Contains(args[1:], arg)
}

func argMax() int {
	mib := [2]int32{ctlKern, kernArgMax}
	var out [4]byte
	size := uintptr(len(out))
	if sysctl(&mib[0], uint32(len(mib)), &out[0], &size, nil, 0) != 0 {
		return 1 << 20
	}
	n := int(binary.LittleEndian.Uint32(out[:]))
	if n <= 0 || n > 16<<20 {
		return 1 << 20
	}
	return n
}

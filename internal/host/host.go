package host

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sync/atomic"
	"syscall"

	"displayext/internal/vdisplay"

	"github.com/go-macos/objc"
)

const Flag = "--display-host"

const StrategyEnv = "DISPLAYEXT_HOST_STRATEGY"

type Ready struct {
	DisplayID uint32 `json:"displayID,omitzero"`
	ModeError string `json:"modeError,omitempty"`
	Error     string `json:"error,omitempty"`
}

func Run(args []string, stdin io.Reader, stdout io.Writer) int {
	if len(args) != 1 {
		report(stdout, Ready{Error: "usage: " + Flag + " <spec-json>"})
		return 2
	}
	var spec vdisplay.Spec
	if err := json.UnmarshalRead(bytes.NewReader([]byte(args[0])), &spec); err != nil {
		report(stdout, Ready{Error: fmt.Sprintf("bad spec: %v", err)})
		return 2
	}

	if os.Getenv(StrategyEnv) == "main" {
		return serve(spec, stdin, stdout)
	}
	objc.App()
	var done atomic.Bool
	var code atomic.Int32
	go func() {
		runtime.LockOSThread()
		code.Store(int32(serve(spec, stdin, stdout)))
		done.Store(true)
	}()
	objc.RunAppLoop(1, done.Load)
	return int(code.Load())
}

func serve(spec vdisplay.Spec, stdin io.Reader, stdout io.Writer) int {
	d, err := vdisplay.Open(spec)
	if err != nil {
		report(stdout, Ready{Error: err.Error()})
		return 1
	}
	r := Ready{DisplayID: d.ID()}
	if d.ModeErr != nil {
		r.ModeError = d.ModeErr.Error()
	}
	report(stdout, r)

	stop := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		stop <- struct{}{}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	select {
	case <-stop:
	case <-sig:
	}
	_ = d.Close()
	return 0
}

func report(w io.Writer, r Ready) {
	if r.Error != "" {
		fmt.Fprintln(os.Stderr, "helper:", r.Error)
	}
	if r.ModeError != "" {
		fmt.Fprintln(os.Stderr, "helper: mode:", r.ModeError)
	}
	b, _ := json.Marshal(r)
	_, _ = fmt.Fprintf(w, "%s\n", b)
}

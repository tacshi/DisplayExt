package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"displayext/internal/config"
	"displayext/internal/uucapture"
	"displayext/internal/uudetect"
)

func main() {
	once := flag.Bool("once", false, "print one snapshot and exit")
	app := flag.String("app", "uu", "the remote app to watch: uu or rustdesk")
	flag.Parse()
	r, label := config.DefaultUURule(), "UU远程"
	switch *app {
	case "uu":
	case "rustdesk":
		r, label = config.DefaultRustDeskRule(), "RustDesk"
	default:
		log.Fatalf("unknown app %q", *app)
	}
	rule := uudetect.Rule{Process: r.Process, Arg: r.Arg, Assertion: r.Assertion, Grace: time.Duration(r.GraceSeconds) * time.Second}

	if *once {
		snap, err := rule.Snapshot()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("processes: %v\nassertions: %q\nsample: %+v\n", snap.Processes, snap.Assertions, rule.Sample(snap))
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var capture uudetect.Capture
	if *app == "uu" {
		w := uucapture.NewWatcher(func() int32 {
			snap, err := rule.Snapshot()
			if err != nil {
				return 0
			}
			return snap.Server()
		}, log.Printf)
		go w.Run(ctx)
		capture = w
	}
	uudetect.Watch(ctx, rule, 2*time.Second, capture,
		func(s uudetect.Status) {
			if capture == nil {
				fmt.Println(time.Now().Format(time.TimeOnly), label+":", s.State)
				return
			}
			fmt.Println(time.Now().Format(time.TimeOnly), label+":", s.State, "showing display", s.Showing)
		},
		func(err error) { log.Println(err) })
}

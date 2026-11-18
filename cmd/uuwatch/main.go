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
	flag.Parse()
	r := config.DefaultUURule()
	rule := uudetect.Rule{Process: r.Process, Assertion: r.Assertion, Grace: time.Duration(r.GraceSeconds) * time.Second}

	if *once {
		snap, err := uudetect.TakeSnapshot(rule.Process)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("processes: %v\nassertions: %q\nsample: %+v\n", snap.Processes, snap.Assertions, rule.Sample(snap))
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	capture := uucapture.NewWatcher(func() int32 {
		snap, err := uudetect.TakeSnapshot(rule.Process)
		if err != nil {
			return 0
		}
		return snap.Server()
	}, log.Printf)
	go capture.Run(ctx)
	uudetect.Watch(ctx, rule, 2*time.Second, capture,
		func(s uudetect.Status) {
			fmt.Println(time.Now().Format(time.TimeOnly), "UU远程:", s.State, "showing display", s.Showing)
		},
		func(err error) { log.Println(err) })
}

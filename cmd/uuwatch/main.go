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
	uudetect.Watch(ctx, rule, 2*time.Second,
		func(s uudetect.State) { fmt.Println(time.Now().Format(time.TimeOnly), "UU远程:", s) },
		func(err error) { log.Println(err) })
}

package main

import (
	"log"
	"os"

	"displayext/internal/app"
	"displayext/internal/host"
	"displayext/internal/manager"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == host.Flag {
		os.Exit(host.Run(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) > 1 && os.Args[1] == manager.RestoreFlag {
		os.Exit(manager.RunRestore(os.Args[2:]))
	}
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

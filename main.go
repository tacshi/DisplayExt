package main

import (
	"log"
	"os"

	"displayext/internal/app"
	"displayext/internal/host"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == host.Flag {
		os.Exit(host.Run(os.Args[2:], os.Stdin, os.Stdout))
	}
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

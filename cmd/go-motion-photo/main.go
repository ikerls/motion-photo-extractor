package main

import (
	"os"

	"github.com/ikerls/motion-photo-extractor/internal/cli"
)

// version is set at build time by GoReleaser.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}

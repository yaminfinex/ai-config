package main

import (
	"os"

	"ai-config/tools/bottle/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}

package main

import (
	"fmt"
	"os"

	"mish/internal/buildinfo"
	"mish/internal/cli"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(buildinfo.Version)
		return
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}

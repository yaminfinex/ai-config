// Command sesh-store is the full store-side build: the fleet client command
// tree plus serve/reindex/admin and their store, index, surface, tsnet, and
// sqlite dependency trees. `just deploy-store` builds and deploys it; it is
// never published to the release channel — install.sh and `sesh update`
// distribute the slim client (./cmd/sesh) only.
package main

import (
	"os"

	"sesh/internal/cli"
	"sesh/internal/storecli"
)

func main() {
	if err := cli.Execute(storecli.Commands()...); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}

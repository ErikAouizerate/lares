// Command lares runs coding agents in a coi sandbox.
package main

import (
	"os"

	"github.com/riko/lares/internal/cli"
)

// version is the release this binary was built from, set by the Makefile with
// -ldflags "-X main.version=...". A binary built by hand reports "dev".
var version = "dev"

func main() {
	cli.Version = version

	os.Exit(cli.Execute())
}

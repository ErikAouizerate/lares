// Command lares runs coding agents in a coi sandbox.
package main

import (
	"os"

	"github.com/riko/lares/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}

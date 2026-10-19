package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// sandboxName names the sandbox everywhere: it is the coi profile lares
// installs and enters, the alias coi gives the container, and the prefix of the
// workspace directory. One name means one sandbox.
const sandboxName = "lares"

// The account coi creates inside the container, and the home it must be told
// about because the exec API does not set it.
const (
	containerUser = 1000
	containerHome = "/home/code"
)

// exitCode carries the status of a command lares ran, so that the shell sees
// the status of that command rather than lares reporting a failure.
type exitCode int

func (c exitCode) Error() string {
	return fmt.Sprintf("exit code %d", int(c))
}

// Execute runs the command line and returns the code the process exits with.
func Execute() int {
	if err := newRootCmd().Execute(); err != nil {
		var code exitCode

		if errors.As(err, &code) {
			return int(code)
		}

		fmt.Fprintf(os.Stderr, "lares: %v\n", err)

		return 1
	}

	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "lares",
		Short: "Run coding agents in a coi sandbox",
		Long: `lares runs a coi sandbox holding one persistent container.

The sandbox is entered from any repository: the repositories are mounted in the
container at their host path, so the working directory is the same inside and
outside. lares installs the coi profile declaring that sandbox, starts the
container, and runs the tool in it.

  lares pi           pi in the current directory
  lares bash         a shell in the current directory
  lares herdr        herdr in the current directory
  lares run          a command in the current directory

  lares install      install the sandbox profile
  lares build        build the image the sandbox is made from
  lares init         create the sandbox without entering it
  lares start        start the sandbox
  lares stop         stop the sandbox, keeping its state
  lares list         list the sandbox, as coi list does
  lares mount        mount a host directory in the sandbox
  lares unmount      unmount a host directory from the sandbox
  lares uninstall    remove the sandbox and the lares binary
`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newPiCmd(),
		newBashCmd(),
		newHerdrCmd(),
		newRunCmd(),
		newInstallCmd(),
		newBuildCmd(),
		newUninstallCmd(),
		newInitCmd(),
		newStartCmd(),
		newStopCmd(),
		newListCmd(),
		newMountCmd(),
		newUnmountCmd(),
	)

	return root
}

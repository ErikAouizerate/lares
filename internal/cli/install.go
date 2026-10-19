package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	lares "github.com/riko/lares"
	"github.com/spf13/cobra"
)

// coiDir returns ~/.coi, where coi keeps the profiles next to its sessions and
// logs.
func coiDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".coi"), nil
}

// profileDir returns the directory coi reads the sandbox profile from.
func profileDir() (string, error) {
	dir, err := coiDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "profiles", sandboxName), nil
}

// profileConfigPath returns the profile file declaring the sandbox.
func profileConfigPath() (string, error) {
	dir, err := profileDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "config.toml"), nil
}

// installedProfile returns the profile file, with a message saying how to
// install it when it is missing.
func installedProfile() (string, error) {
	config, err := profileConfigPath()
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(config); err != nil {
		return "", fmt.Errorf("the sandbox profile %q is not installed in %s: run `lares install`", sandboxName, filepath.Dir(config))
	}

	return config, nil
}

// workspacePath returns the host directory mounted as the workspace of the
// sandbox. It is a fixed, neutral directory so that the identity of the
// container never depends on the repository it is entered from.
func workspacePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, sandboxName+"-workspace"), nil
}

func newInstallCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the coi profile declaring the sandbox",
		Long: `Install the coi profile declaring the sandbox, with the script its image is
built from.

Both are embedded in the lares binary and written to ~/.coi/profiles/lares, so
installing needs nothing else on disk. The profile is left alone when it is
already there, because the directories mounted with ` + "`lares mount`" + ` are declared in
it.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return installProfile(force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "write the profile again, dropping the directories mounted with lares mount")

	return cmd
}

func installProfile(force bool) error {
	dir, err := profileDir()
	if err != nil {
		return err
	}

	config := filepath.Join(dir, "config.toml")
	script := filepath.Join(dir, "provision.sh")

	// An installed profile is left alone: it carries the directories mounted with
	// `lares mount`, which a reinstall would drop. Installing has to stay
	// repeatable, because the binary is updated far more often than the profile.
	if _, err := os.Stat(config); err == nil && !force {
		if same, _ := carriesThisProfile(config, script); same {
			fmt.Fprintf(os.Stderr, "lares: the profile %q is already installed in %s\n", sandboxName, config)
		} else {
			fmt.Fprintf(os.Stderr, "lares: the profile %q is already installed in %s and was left alone\n"+
				"  --force writes the one this binary carries, dropping the directories mounted with `lares mount`\n", sandboxName, config)
		}

		return nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(config, lares.Config, 0o644); err != nil {
		return err
	}

	if err := os.WriteFile(script, lares.Provision, 0o755); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "lares: installed the profile %q in %s\n", sandboxName, dir)
	fmt.Fprintf(os.Stderr, "  its image is built with: coi build --profile %s\n", sandboxName)

	return nil
}

// carriesThisProfile reports whether what is installed is what this binary
// carries, which is how a reinstall tells "nothing to do" from "the profile was
// edited here".
func carriesThisProfile(config, script string) (bool, error) {
	installed, err := os.ReadFile(config)
	if err != nil {
		return false, err
	}

	if !bytes.Equal(installed, lares.Config) {
		return false, nil
	}

	provision, err := os.ReadFile(script)
	if err != nil {
		return false, err
	}

	return bytes.Equal(provision, lares.Provision), nil
}

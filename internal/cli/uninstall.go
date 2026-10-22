package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/riko/lares/internal/coi"
	"github.com/spf13/cobra"
)

func newUninstallCmd() *cobra.Command {
	var purge bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the sandbox and put coi back the way it was",
		Long: `Remove everything lares put on the machine: the container, the image the
sandbox is built from, the coi profile, and the lares binary itself.

coi is left the way it was before lares: no profile, no container, no image.
The workspace directory is kept, because it holds pi's session history; pass
--purge to remove it too. The lares binary is removed last, which is safe on
Linux even though it is the process doing the removing.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return uninstall(purge)
		},
	}

	cmd.Flags().BoolVar(&purge, "purge", false, "also remove the workspace directory")

	return cmd
}

func uninstall(purge bool) error {
	// Read the image name before anything is removed: it comes from the profile.
	image, err := sandboxImage()
	if err != nil {
		return err
	}

	if container, err := coi.Find(sandboxName); err == nil {
		if err := coi.Run("kill", container.Name, "--force"); err != nil {
			return err
		}

		removeContainerState(container.Name)
	} else if !errors.Is(err, coi.ErrNoSandbox) {
		return err
	}

	if coi.HasImage(image) {
		if err := coi.DeleteImage(image); err != nil {
			return err
		}
	}

	dir, err := profileDir()
	if err != nil {
		return err
	}

	// The secrets file holds values the user provided for the sandbox, not
	// lares-generated configuration. Keep it so a reinstall starts from the same
	// credentials; everything else in the profile goes.
	secrets := filepath.Join(dir, secretsFileName)
	body, secretsErr := os.ReadFile(secrets)
	if secretsErr != nil && !errors.Is(secretsErr, os.ErrNotExist) {
		return secretsErr
	}

	if err := os.RemoveAll(dir); err != nil {
		return err
	}

	if secretsErr == nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}

		if err := os.WriteFile(secrets, body, 0o600); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "lares: removed the profile %s, kept %s\n", dir, secrets)
	} else {
		fmt.Fprintf(os.Stderr, "lares: removed the profile %s\n", dir)
	}

	if purge {
		workspace, err := workspacePath()
		if err != nil {
			return err
		}

		if err := os.RemoveAll(workspace); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "lares: removed %s\n", workspace)
	}

	removeSelf()

	fmt.Fprintln(os.Stderr, "lares: coi is back to its original state")

	return nil
}

// sandboxImage returns the image the installed profile builds and the sandbox
// runs, read from the profile itself so that renaming it there is enough. It
// falls back to the coi convention when the profile is not there or carries no
// image.
func sandboxImage() (string, error) {
	config, err := profileConfigPath()
	if err != nil {
		return "", err
	}

	body, err := os.ReadFile(config)
	if err != nil {
		return "coi-" + sandboxName, nil
	}

	for _, line := range strings.Split(string(body), "\n") {
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != "image" {
			continue
		}

		if name := strings.Trim(strings.TrimSpace(value), `"`); name != "" {
			return name, nil
		}
	}

	return "coi-" + sandboxName, nil
}

// removeContainerState drops the per-container files coi leaves behind when a
// container is killed: the supervisor state and the audit log. They describe a
// container that no longer exists.
func removeContainerState(name string) {
	dir, err := coiDir()
	if err != nil {
		return
	}

	for _, leftover := range []string{
		filepath.Join(dir, "run", name+".supervisor.json"),
		filepath.Join(dir, "run", name+".supervisor.lock"),
		filepath.Join(dir, "audit", name+".jsonl"),
	} {
		_ = os.Remove(leftover)
	}
}

// removeSelf deletes the lares binary. Removing a running executable is safe on
// Linux: the process keeps its own copy and finishes normally.
func removeSelf() {
	executable, err := os.Executable()
	if err != nil {
		return
	}

	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	// Only remove a binary that really is lares: `go run` executes a temporary
	// build in a cache directory, which must be left alone.
	if filepath.Base(executable) != sandboxName || strings.Contains(executable, "go-build") {
		return
	}

	if err := os.Remove(executable); err == nil {
		fmt.Fprintf(os.Stderr, "lares: removed %s\n", executable)
	}
}

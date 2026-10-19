package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/riko/lares/internal/coi"
	"github.com/riko/lares/internal/ttyproxy"
	"github.com/spf13/cobra"
)

// dotsHome is the home directory of the host dotfiles repository. Its files are
// linked into the container home rather than copied, so an edit on the host is
// picked up by the next shell inside.
const dotsHome = "/home/riko/light-workspace/dots/home"

// piAgentDir is where pi keeps its configuration, on the host and inside the
// container.
const piAgentDir = ".pi/agent"

// piConfigFiles are the host files copied into the container before pi starts.
// The container home is not a mount, so the files have to be pushed on every
// run to follow a change made on the host.
var piConfigFiles = []string{"auth.json", "settings.json", "AGENTS.md", "models.json"}

// linkScript links the dotfiles into the container home. A file that is not a
// symlink to the host is moved aside once, so that the host version wins
// without losing what the container had.
const linkScript = `
set -e
src="%s"
link() {
  rel="$1"; s="$src/$rel"; d="/home/code/$rel"
  [ -e "$s" ] || return 0
  mkdir -p "$(dirname "$d")"
  if [ -L "$d" ] && [ "$(readlink "$d")" = "$s" ]; then return 0; fi
  [ -e "$d" ] && [ ! -L "$d" ] && mv "$d" "$d.bak.$(date +%%Y%%m%%d-%%H%%M%%S)"
  ln -sfn "$s" "$d"
}
link .zshrc
link .zshenv
link .config/mise/config.toml
chown -h code:code /home/code/.zshrc /home/code/.zshenv /home/code/.config/mise/config.toml 2>/dev/null || true
`

func newPiCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "pi [args...]",
		Short:              "Run pi in the current directory",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return enter(append([]string{"pi"}, args...))
		},
	}
}

func newBashCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "bash [args...]",
		Short:              "Run a login shell in the current directory",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return enter(append([]string{"bash", "-l"}, args...))
		},
	}
}

func newHerdrCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "herdr [args...]",
		Short:              "Run herdr in the current directory",
		DisableFlagParsing: true,
		RunE: func(_ *cobra.Command, args []string) error {
			return enter(append([]string{"herdr"}, args...))
		},
	}
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the sandbox, creating it on first use",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			container, err := sandbox()
			if err != nil {
				return err
			}

			fmt.Printf("lares is running (%s)\n", container.Name)

			return nil
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the sandbox, keeping its state",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			container, err := coi.Find(sandboxName)
			if errors.Is(err, coi.ErrNoSandbox) {
				fmt.Fprintf(os.Stderr, "lares: no sandbox to stop\n")

				return nil
			}

			if err != nil {
				return err
			}

			return coi.Run("container", "stop", container.Name)
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the sandbox",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return coi.Show("list")
		},
	}
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the sandbox without entering it",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			container, err := coi.Find(sandboxName)
			if err == nil {
				fmt.Fprintf(os.Stderr, "lares: the sandbox already exists: %s\n", container.Name)

				return nil
			}

			if !errors.Is(err, coi.ErrNoSandbox) {
				return err
			}

			_, err = create()

			return err
		},
	}
}

func newMountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mount [directory]",
		Short: "Mount a host directory in the sandbox",
		Long: `Mount a host directory in the sandbox.

The directory is declared in the profile, so it comes back with every run of the
container, and mounted right away when the sandbox is running. A repository is
entered by mounting it: the mount mirrors the host path, which is what makes the
working directory the same inside and outside.

Without a directory, the current one is mounted.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return mountDirectory(directoryArg(args))
		},
	}
}

func newUnmountCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unmount [directory]",
		Short: "Unmount a host directory from the sandbox",
		Long: `Unmount a host directory from the sandbox.

The directory is removed from the profile, so a container created later no
longer mounts it, and unmounted right away when the sandbox is running. Nothing
is deleted on the host: the directory stays where it is.

Without a directory, the current one is unmounted.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return unmountDirectory(directoryArg(args))
		},
	}
}

// directoryArg returns the directory named on the command line, and the current
// one when there is none: mounting and unmounting the repository the shell is
// already in is the common case.
func directoryArg(args []string) string {
	if len(args) == 0 {
		return "."
	}

	return args[0]
}

// enter starts the tool in the container, in the current directory.
func enter(argv []string) error {
	container, err := sandbox()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if err := requireMounted(container.Name, cwd); err != nil {
		return err
	}

	linkDotfiles(container.Name)
	linkPiExtras(container.Name)
	syncPiConfig(container.Name)

	code, err := ttyproxy.Run(append(attachArgs(container.Name, cwd), argv...))
	if err != nil {
		return err
	}

	if code != 0 {
		return exitCode(code)
	}

	return nil
}

// sandbox returns the container, creating it on first use and starting it when
// it is stopped.
func sandbox() (coi.Container, error) {
	if _, err := installedProfile(); err != nil {
		return coi.Container{}, err
	}

	container, err := coi.Find(sandboxName)

	if errors.Is(err, coi.ErrNoSandbox) {
		return create()
	}

	if err != nil {
		return coi.Container{}, err
	}

	if !container.Running() {
		if err := coi.Start(container.Name); err != nil {
			return coi.Container{}, err
		}
	}

	return container, nil
}

// create builds the container by entering it once: coi creates it on the way,
// and the shell that opens is the confirmation that the image works.
func create() (coi.Container, error) {
	if _, err := installedProfile(); err != nil {
		return coi.Container{}, err
	}

	workspace, err := workspacePath()
	if err != nil {
		return coi.Container{}, err
	}

	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return coi.Container{}, err
	}

	fmt.Fprintf(os.Stderr, "lares: creating the sandbox — a shell opens inside it, type 'exit' when it is ready\n")

	if err := coi.Shell(workspace, "--profile", sandboxName, "-w", workspace, "--debug"); err != nil {
		return coi.Container{}, fmt.Errorf("creating the sandbox: %w\n  its image is built with: coi build --profile %s", err, sandboxName)
	}

	container, err := coi.Find(sandboxName)
	if err != nil {
		return coi.Container{}, fmt.Errorf("the sandbox was not created: %w", err)
	}

	return container, nil
}

// attachArgs builds the coi command that runs argv in the container, in dir,
// with a terminal.
func attachArgs(name, dir string) []string {
	termType := os.Getenv("TERM")
	if termType == "" {
		termType = "xterm-256color"
	}

	return []string{
		"coi", "container", "exec", name, "-t",
		"--user", strconv.Itoa(containerUser),
		"--env", "HOME=" + containerHome,
		"--env", "TERM=" + termType,
		"--cwd", dir, "--",
	}
}

// requireMounted refuses to enter a directory the container cannot see, rather
// than opening a tool in a directory that does not exist inside.
func requireMounted(name, dir string) error {
	mounted, err := isMounted(name, dir)
	if err != nil {
		return err
	}

	if mounted {
		return nil
	}

	config, err := profileConfigPath()
	if err != nil {
		return err
	}

	return fmt.Errorf(`%s is not mounted in the sandbox

  lares mount %s

declares it in %s and mounts it right away`, dir, dir, config)
}

// isMounted reports whether dir is one of the host paths mounted in the
// container, or lives inside one of them.
func isMounted(name, dir string) (bool, error) {
	sources, err := coi.MountSources(name)
	if err != nil {
		return false, err
	}

	for _, source := range sources {
		if dir == source || strings.HasPrefix(dir, source+string(os.PathSeparator)) {
			return true, nil
		}
	}

	return false, nil
}

// linkDotfiles links the files of the host dotfiles repository into the
// container home, when that repository is mounted.
func linkDotfiles(name string) {
	if info, err := os.Stat(dotsHome); err != nil || !info.IsDir() {
		return
	}

	mounted, err := isMounted(name, dotsHome)
	if err != nil || !mounted {
		return
	}

	if err := coi.Exec(name, "bash", "-c", fmt.Sprintf(linkScript, dotsHome)); err != nil {
		fmt.Fprintf(os.Stderr, "lares: linking the dotfiles: %v\n", err)
	}
}

// piLinkScript links the read-only pi mounts back into the agent directory.
// They are mounted outside it because coi chowns ~/.pi/agent recursively at
// setup, which fails on a read-only mount placed inside it.
const piLinkScript = `
set -e
dir=/home/code/.pi/agent
mkdir -p "$dir"
link() {
  target="$1"; link_path="$2"
  [ -e "$target" ] || return 0
  if [ -L "$link_path" ] && [ "$(readlink "$link_path")" = "$target" ]; then return 0; fi
  [ -e "$link_path" ] && [ ! -L "$link_path" ] && mv "$link_path" "$link_path.bak.$(date +%Y%m%d-%H%M%S)"
  ln -sfn "$target" "$link_path"
  chown -h code:code "$link_path"
}
link /opt/pi/extensions "$dir/extensions"
link /opt/pi/mcp-adapter.json "$dir/mcp-adapter.json"
`

// linkPiExtras wires the pi files the profile mounts under /opt into the agent
// directory, where pi expects them.
func linkPiExtras(name string) {
	if err := coi.Exec(name, "bash", "-c", piLinkScript); err != nil {
		fmt.Fprintf(os.Stderr, "lares: linking the pi configuration: %v\n", err)
	}
}

// syncPiConfig copies pi's configuration from the host into the container, so
// that a change made outside the sandbox is picked up without recreating it.
// The credentials are copied too: there is no relay, the sandbox talks to the
// providers directly with the host credential.
func syncPiConfig(name string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}

	for _, file := range piConfigFiles {
		local := filepath.Join(home, piAgentDir, file)

		if _, err := os.Stat(local); err != nil {
			continue
		}

		remote := name + ":" + path.Join(containerHome, piAgentDir, file)

		if err := coi.PushFile(local, remote); err != nil {
			fmt.Fprintf(os.Stderr, "lares: copying %s into the sandbox: %v\n", file, err)
		}
	}
}

// mountDirectory declares a host directory in the profile and mounts it in the
// container that is already running.
func mountDirectory(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	info, err := os.Stat(absolute)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", absolute)
	}

	config, err := installedProfile()
	if err != nil {
		return err
	}

	declared, err := declaresMount(config, absolute)
	if err != nil {
		return err
	}

	if declared {
		fmt.Fprintf(os.Stderr, "lares: %s is already declared in %s\n", absolute, config)
	} else if err := declareMount(config, absolute); err != nil {
		return err
	}

	container, err := coi.Find(sandboxName)

	if errors.Is(err, coi.ErrNoSandbox) {
		return nil
	}

	if err != nil {
		return err
	}

	mounted, err := isMounted(container.Name, absolute)
	if err != nil {
		return err
	}

	if mounted {
		fmt.Fprintf(os.Stderr, "lares: %s is already mounted\n", absolute)

		return nil
	}

	if err := coi.Mount(container.Name, mountDevice(absolute), absolute, absolute); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "lares: mounted %s in %s\n", absolute, container.Name)

	return nil
}

// declaresMount reports whether the profile already declares a host directory.
func declaresMount(config, dir string) (bool, error) {
	body, err := os.ReadFile(config)
	if err != nil {
		return false, err
	}

	return strings.Contains(string(body), fmt.Sprintf("host = %q", dir)), nil
}

// declareMount appends a directory to the mounts of the profile.
func declareMount(config, dir string) error {
	file, err := os.OpenFile(config, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(file, "\n[[mounts.default]]\nhost = %q\ncontainer = %q\n", dir, dir); err != nil {
		_ = file.Close()

		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "lares: declared %s in %s\n", dir, config)

	return nil
}

// unmountDirectory removes a host directory from the profile and unmounts it
// from the container that is already running.
func unmountDirectory(dir string) error {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	workspace, err := workspacePath()
	if err != nil {
		return err
	}

	// The workspace is not a mount the user declared: it is what the container
	// opens on, and unmounting it would leave the sandbox without one.
	if absolute == workspace {
		return fmt.Errorf("%s is the workspace of the sandbox, not a mount that can be removed", absolute)
	}

	config, err := installedProfile()
	if err != nil {
		return err
	}

	declared, err := undeclareMount(config, absolute)
	if err != nil {
		return err
	}

	if declared {
		fmt.Fprintf(os.Stderr, "lares: removed %s from %s\n", absolute, config)
	}

	container, err := coi.Find(sandboxName)

	if errors.Is(err, coi.ErrNoSandbox) {
		if !declared {
			return fmt.Errorf("%s is not mounted in the sandbox", absolute)
		}

		return nil
	}

	if err != nil {
		return err
	}

	device, mounted, err := coi.MountDevice(container.Name, absolute)
	if err != nil {
		return err
	}

	if !mounted {
		if !declared {
			return fmt.Errorf("%s is not mounted in the sandbox", absolute)
		}

		return nil
	}

	if err := coi.Unmount(container.Name, device); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "lares: unmounted %s from %s\n", absolute, container.Name)

	return nil
}

// undeclareMount removes the table declaring a host directory in the profile,
// so that a container created later no longer mounts it, and reports whether
// there was one.
func undeclareMount(config, dir string) (bool, error) {
	body, err := os.ReadFile(config)
	if err != nil {
		return false, err
	}

	lines := strings.Split(string(body), "\n")
	kept := make([]string, 0, len(lines))
	removed := false

	for i := 0; i < len(lines); i++ {
		if lines[i] != "[[mounts.default]]" {
			kept = append(kept, lines[i])

			continue
		}

		end := i + 1
		for end < len(lines) && !strings.HasPrefix(lines[end], "[") {
			end++
		}

		// The comments and blank lines closing a table introduce the one that
		// follows, so they stay when the table goes.
		body := end
		for body > i && (lines[body-1] == "" || strings.HasPrefix(lines[body-1], "#")) {
			body--
		}

		// A table runs until the next one, blank separator included.
		if !declaresHost(lines[i:body], dir) {
			kept = append(kept, lines[i:end]...)
			i = end - 1

			continue
		}

		removed = true
		i = end - 1

		// The blank line that separated the previous table from the removed one
		// would double the one opening what is kept of the table.
		if tail := lines[body:end]; len(tail) > 0 && len(kept) > 0 && kept[len(kept)-1] == "" {
			kept = kept[:len(kept)-1]
		}

		kept = append(kept, lines[body:end]...)
	}

	if !removed {
		return false, nil
	}

	if err := os.WriteFile(config, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		return false, err
	}

	return true, nil
}

// declaresHost reports whether a profile table declares a host directory.
func declaresHost(table []string, dir string) bool {
	wanted := fmt.Sprintf("host = %q", dir)

	for _, line := range table {
		if strings.Contains(line, wanted) {
			return true
		}
	}

	return false
}

// mountDevice names the Incus device of a mounted directory. The name is
// derived from the path, so that mounting the same directory twice is refused
// by Incus instead of adding a second device.
func mountDevice(dir string) string {
	sum := sha256.Sum256([]byte(dir))

	return "mount-" + hex.EncodeToString(sum[:4])
}

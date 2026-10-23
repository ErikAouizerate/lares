package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

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

// workspacePath returns the host directory coi mounts at /workspace in the
// sandbox. Every coi container has one, and none of the repositories lares
// mounts takes that place: the workspace is where coi writes pi's session
// history and its own protection files, so it holds no repository of ours.
//
// It sits under the state directory rather than in the home directory
// because that history is worth keeping across runs.
func workspacePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".local", "state", sandboxName, "workspace"), nil
}

func newInstallCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the coi profile declaring the sandbox",
		Long: `Install the coi profile declaring the sandbox, with the script its image is
built from.

Both are embedded in the lares binary and written to ~/.coi/profiles/lares, so
installing needs nothing else on disk. The profile carries no host path of its
own: the extras this machine has are added here, and the directories mounted
with ` + "`lares mount`" + ` are kept.

Installing converges instead of duplicating: the profile is written again from
this binary, so it picks up what a new version declares, while the directories
added with ` + "`lares mount`" + ` stay. A mount whose host path is gone is dropped,
because coi refuses to create the container when it cannot resolve one.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return installProfile(force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "write the profile the binary carries as it is, dropping the directories mounted with lares mount")

	return cmd
}

// optionalMount is a host path lares shares with the sandbox. It is not in the
// embedded profile because it only exists on some machines: one that lacks it
// must end up with no mount, and no directory created for it.
type optionalMount struct {
	host      string
	container string
}

// optionalCandidates are the extras lares knows about, whether this machine has
// them or not. They are also the mount tables lares owns: every other table in
// the installed profile comes from `lares mount` and is kept across installs.
func optionalCandidates() []optionalMount {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	return []optionalMount{
		{filepath.Join(home, ".local", "bin", "herdr"), "/usr/local/bin/herdr"},
		{filepath.Join(home, ".agents"), path.Join(containerHome, ".agents")},
		{filepath.Join(home, piAgentDir, "extensions"), "/opt/pi/extensions"},
	}
}

// optionalMounts returns the candidates this machine actually has: the herdr
// binary, the skills pi reads from ~/.agents, and the extensions pi loads.
func optionalMounts() []optionalMount {
	var mounts []optionalMount

	for _, mount := range optionalCandidates() {
		if _, err := os.Stat(mount.host); err == nil {
			mounts = append(mounts, mount)
		}
	}

	return mounts
}

// managedMountHosts returns every host path lares may write in a profile: the
// optional extras, and the ones older versions carried in the embedded profile.
// A table declaring one of them belongs to lares and is regenerated; anything
// else was added with `lares mount` and is kept.
func managedMountHosts() []string {
	hosts := []string{}

	for _, mount := range optionalCandidates() {
		hosts = append(hosts, mount.host)
	}

	if home, err := os.UserHomeDir(); err == nil {
		// Older profiles mounted the host mcp-adapter.json read-only; lares now
		// pushes the rendered file, so that mount must not come back.
		hosts = append(hosts, filepath.Join(home, piAgentDir, "mcp-adapter.json"))
	}

	return hosts
}

// mountsHeader introduces the mounts of the generated profile.
const mountsHeader = "\n" +
	"# ---------------------------------------------------------------------------\n" +
	"# Mounts: the host extras this machine has, then the directories added with\n" +
	"# `lares mount`. Installing again regenerates this part of the profile.\n" +
	"# ---------------------------------------------------------------------------\n"

// profileConfig builds the profile lares installs on this machine: the embedded
// one, the host extras it actually has, and — unless they are dropped — the
// directories added with `lares mount`, read from the profile being replaced
// with the options they carry.
func profileConfig(installed []byte, keepMounts bool) []byte {
	var body strings.Builder

	body.Write(lares.Config)
	body.WriteString(mountsHeader)

	for _, mount := range optionalMounts() {
		writeMount(&body, mount.host, mount.container)
	}

	if keepMounts {
		for _, table := range extraMounts(string(installed)) {
			body.WriteString("\n" + table + "\n")
		}
	}

	return []byte(body.String())
}

// writeMount writes one mount table of the generated part, read-only: the host
// extras are configuration, never something the sandbox writes to.
func writeMount(body *strings.Builder, host, container string) {
	fmt.Fprintf(body, "\n[[mounts.default]]\nhost = %q\ncontainer = %q\nreadonly = true\n", host, container)
}

// extraMounts returns the mount tables of an installed profile that lares does
// not manage — the directories added with `lares mount` — as they are written,
// so that the options they carry survive a reinstall. A table whose host path
// is gone is dropped: coi refuses to create the container with a mount it
// cannot resolve, and a directory removed on the host cannot be mounted anyway.
func extraMounts(installed string) []string {
	managed := managedMountHosts()
	seen := make(map[string]bool)
	tables := []string{}

	for _, table := range mountTables(installed) {
		host := tableHost(table)
		if host == "" {
			continue
		}

		if contains(managed, host) || seen[host] {
			continue
		}

		if _, err := os.Stat(expandHome(host)); err != nil {
			fmt.Fprintf(os.Stderr, "lares: dropped the mount of %s: it is not on this machine\n", host)

			continue
		}

		seen[host] = true
		tables = append(tables, strings.Join(table, "\n"))
	}

	return tables
}

// mountTables splits a profile into its mounts.* tables, each as the lines it is
// written with, comments and blank lines between tables left out.
func mountTables(body string) [][]string {
	var (
		tables  [][]string
		current []string
	)

	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") {
			if current != nil {
				tables = append(tables, withoutTrailingBlanks(current))
				current = nil
			}

			if strings.HasPrefix(trimmed, "[[mounts.") {
				current = []string{trimmed}
			}

			continue
		}

		if current != nil {
			current = append(current, trimmed)
		}
	}

	if current != nil {
		tables = append(tables, withoutTrailingBlanks(current))
	}

	return tables
}

// withoutTrailingBlanks drops the blank lines closing a table.
func withoutTrailingBlanks(table []string) []string {
	for len(table) > 1 && table[len(table)-1] == "" {
		table = table[:len(table)-1]
	}

	return table
}

// tableHost reads the host path a mount table declares.
func tableHost(table []string) string {
	for _, line := range table[1:] {
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "host" {
			return strings.Trim(strings.TrimSpace(value), `"`)
		}
	}

	return ""
}

// contains reports whether a list holds a value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}

	return false
}

// expandHome resolves the leading ~ coi accepts in a mount path, so that a
// profile written by hand is checked against the right directory.
func expandHome(host string) string {
	if !strings.HasPrefix(host, "~/") {
		return host
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return host
	}

	return filepath.Join(home, strings.TrimPrefix(host, "~/"))
}

func installProfile(force bool) error {
	dir, err := profileDir()
	if err != nil {
		return err
	}

	config := filepath.Join(dir, "config.toml")
	script := filepath.Join(dir, "provision.sh")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// The secrets are the one file in the profile lares never overwrites: they
	// are sandbox-specific values the user provides, not part of the profile.
	created, err := installSecrets(dir)
	if err != nil {
		return err
	}

	if created {
		fmt.Fprintf(os.Stderr, "lares: created %s; fill it with credentials dedicated to the sandbox\n", filepath.Join(dir, secretsFileName))
	}

	// The image script carries no user state, so it always follows the binary:
	// rebuilding the image picks it up without touching the profile.
	if err := os.WriteFile(script, lares.Provision, 0o755); err != nil {
		return err
	}

	installed, err := os.ReadFile(config)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	want := profileConfig(installed, !force)

	if bytes.Equal(installed, want) {
		fmt.Fprintf(os.Stderr, "lares: the profile %q is already installed in %s\n", sandboxName, config)
		warnUIDMapping()

		return nil
	}

	if err := os.WriteFile(config, want, 0o644); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "lares: installed the profile %q in %s\n", sandboxName, dir)
	fmt.Fprintf(os.Stderr, "  its image is built with: lares build\n")

	warnUIDMapping()

	return nil
}

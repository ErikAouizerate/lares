package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	lares "github.com/riko/lares"
)

// TestProfileConfigDeclaresOnlyTheExtrasThisMachineHas checks that the profile
// lares installs carries no path of another machine: an extra that is not here
// must not be declared, because coi refuses to create a container with a mount
// it cannot resolve.
func TestProfileConfigDeclaresOnlyTheExtrasThisMachineHas(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	body := string(profileConfig(nil, false))

	if !strings.Contains(body, fmt.Sprintf("host = %q", filepath.Join(home, ".agents"))) {
		t.Errorf("the profile does not declare the skills this machine has:\n%s", body)
	}

	for _, missing := range []string{`container = "/usr/local/bin/herdr"`, `container = "/opt/pi/extensions"`} {
		if strings.Contains(body, missing) {
			t.Errorf("the profile mounts %s, which this machine does not have:\n%s", missing, body)
		}
	}

	for _, line := range strings.Split(body, "\n") {
		host, found := strings.CutPrefix(strings.TrimSpace(line), "host = ")
		if !found {
			continue
		}

		if path := strings.Trim(host, `"`); !strings.HasPrefix(path, home) {
			t.Errorf("the profile mounts %s, which is not on this machine:\n%s", path, body)
		}
	}
}

// TestProfileConfigWithoutExtrasIsTheEmbeddedProfile checks that a machine
// without any extra gets the embedded profile and no mount.
func TestProfileConfigWithoutExtrasIsTheEmbeddedProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	body := string(profileConfig(nil, false))

	if !strings.HasPrefix(body, string(lares.Config)) {
		t.Errorf("the profile does not start with the embedded one:\n%s", body)
	}

	if strings.Contains(body, "[[mounts.default]]") {
		t.Errorf("a machine without extras got a mount:\n%s", body)
	}
}

// TestProfileConfigKeepsTheMountedDirectories checks what a reinstall has to
// preserve: the tables added with `lares mount`, with the options they carry.
func TestProfileConfigKeepsTheMountedDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := filepath.Join(home, "workspace", "repo")

	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}

	installed := fmt.Sprintf(`[container]
image = "coi-lares"

[[mounts.default]]
host = %q
container = %q

[[mounts.default]]
host = %q
container = "/usr/local/bin/herdr"
readonly = true
`, repo, repo, filepath.Join(home, ".local", "bin", "herdr"))

	body := string(profileConfig([]byte(installed), true))

	if !strings.Contains(body, fmt.Sprintf("host = %q", repo)) {
		t.Errorf("the repository mounted with lares mount was dropped:\n%s", body)
	}

	// The extras are regenerated from what this machine has, not copied from the
	// profile being replaced.
	if strings.Contains(body, `container = "/usr/local/bin/herdr"`) {
		t.Errorf("a mount lares manages was copied from the old profile:\n%s", body)
	}

	if forced := string(profileConfig([]byte(installed), false)); strings.Contains(forced, repo) {
		t.Errorf("--force kept a directory mounted with lares mount:\n%s", forced)
	}
}

// TestExtraMountsDropsWhatCannotResolve checks the convergence install relies
// on: a mount whose host path is gone goes, the others stay.
func TestExtraMountsDropsWhatCannotResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	kept := filepath.Join(home, "repo")

	if err := os.MkdirAll(kept, 0o755); err != nil {
		t.Fatal(err)
	}

	gone := filepath.Join(home, "nowhere")

	installed := fmt.Sprintf(`[container]
image = "coi-lares"

[[mounts.default]]
host = %q
container = %q

[[mounts.default]]
host = %q
container = %q
`, gone, gone, kept, kept)

	tables := extraMounts(installed)

	if len(tables) != 1 {
		t.Fatalf("extraMounts returned %d tables, want 1:\n%s", len(tables), strings.Join(tables, "\n"))
	}

	if !strings.Contains(tables[0], kept) {
		t.Errorf("the mount of a directory that exists was removed: %s", tables[0])
	}
}

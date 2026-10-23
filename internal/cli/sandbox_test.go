package cli

import (
	"os"
	"path/filepath"
	"testing"
)

const profileFixture = `[container]
image = "coi-lares"

# ---------------------------------------------------------------------------
# Repos
# ---------------------------------------------------------------------------

[[mounts.default]]
host = "/home/agent/light-workspace"
container = "/home/agent/light-workspace"

[[mounts.default]]
host = "/home/agent/some/repo"
container = "/home/agent/some/repo"

# ---------------------------------------------------------------------------
# pi extras
# ---------------------------------------------------------------------------

[[mounts.default]]
host = "/home/agent/.agents"
container = "/home/lares/.agents"
readonly = true
`

func writeProfile(t *testing.T, body string) string {
	t.Helper()

	config := filepath.Join(t.TempDir(), "config.toml")

	if err := os.WriteFile(config, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	return config
}

func TestUndeclareMountRemovesOnlyTheTable(t *testing.T) {
	config := writeProfile(t, profileFixture)

	removed, err := undeclareMount(config, "/home/agent/some/repo")
	if err != nil {
		t.Fatal(err)
	}

	if !removed {
		t.Fatal("the table declaring the directory was not found")
	}

	body, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}

	got := string(body)
	want := `[container]
image = "coi-lares"

# ---------------------------------------------------------------------------
# Repos
# ---------------------------------------------------------------------------

[[mounts.default]]
host = "/home/agent/light-workspace"
container = "/home/agent/light-workspace"

# ---------------------------------------------------------------------------
# pi extras
# ---------------------------------------------------------------------------

[[mounts.default]]
host = "/home/agent/.agents"
container = "/home/lares/.agents"
readonly = true
`

	if got != want {
		t.Errorf("the profile was left as:\n%s\nwant:\n%s", got, want)
	}
}

func TestUndeclareMountLeavesAnUndeclaredProfileAlone(t *testing.T) {
	config := writeProfile(t, profileFixture)

	removed, err := undeclareMount(config, "/home/agent/nowhere")
	if err != nil {
		t.Fatal(err)
	}

	if removed {
		t.Fatal("a directory that is not declared was reported as removed")
	}

	body, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}

	if string(body) != profileFixture {
		t.Errorf("the profile was rewritten:\n%s", body)
	}
}

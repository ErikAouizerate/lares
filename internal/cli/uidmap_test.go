package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDelegatedToRoot checks what counts as a usable delegation: a size-1 range
// for the uid on root's line, since Incus refuses a uid inside a wider block.
func TestDelegatedToRoot(t *testing.T) {
	subid := func(body string) string {
		t.Helper()

		path := filepath.Join(t.TempDir(), "subid")

		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}

		return path
	}

	cases := []struct {
		name string
		body string
		want bool
	}{
		{"dedicated", "agent:524288:65536\nroot:1000000:1000000000\nroot:1001:1\n", true},
		{"only a wide range", "root:1000000:1000000000\n", false},
		{"another user's delegation", "agent:1001:1\nroot:1000000:1000000000\n", false},
		{"another uid", "root:1002:1\nroot:1000000:1000000000\n", false},
		{"empty", "", false},
		{"malformed", "root:1001\n", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := delegatedToRoot(subid(tc.body), 1001); got != tc.want {
				t.Errorf("delegatedToRoot = %t, want %t", got, tc.want)
			}
		})
	}
}

// TestIsIncusDaemon checks that only the daemon itself is recognised: the
// helpers it spawns share its name and are not restarted with it.
func TestIsIncusDaemon(t *testing.T) {
	cases := []struct {
		cmdline string
		want    bool
	}{
		{"/usr/libexec/incus/incusd\x00--group=incus\x00", true},
		{"incusd\x00", true},
		{"incusd\x00forkstart\x00coi-f39a18e7-1\x00", false},
		{"incusd\x00forkproxy\x00", false},
		{"/usr/bin/incus\x00list\x00", false},
		{"", false},
	}

	for _, tc := range cases {
		if got := isIncusDaemon([]byte(tc.cmdline)); got != tc.want {
			t.Errorf("isIncusDaemon(%q) = %t, want %t", tc.cmdline, got, tc.want)
		}
	}
}

// TestUIDMappingFixDelegatesTheHostUID checks that the printed commands delegate
// the machine's uid (not the container's) and name both in the explanation.
func TestUIDMappingFixDelegatesTheHostUID(t *testing.T) {
	fix := uidMappingFix(1001)

	for _, want := range []string{`echo "root:1001:1" >> /etc/subuid`, `echo "root:1001:1" >> /etc/subgid`, "systemctl restart incus", "uid\n1000 while this machine's user is uid 1001"} {
		if !strings.Contains(fix, want) {
			t.Errorf("the fix %q does not contain %q", fix, want)
		}
	}
}

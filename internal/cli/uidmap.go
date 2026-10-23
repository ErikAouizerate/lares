package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A sandbox command runs as the "lares" user of the image, uid 1000. When this
// machine's user has another uid, coi maps it into the container with raw.idmap,
// and Incus only maps a host uid that root has been delegated in /etc/subuid and
// /etc/subgid. The delegation has to be a size-1 one (root:<uid>:1): Incus
// refuses a zip inside a larger block, which is what a uid inside root's
// default 1000000 range is.
//
// Without it the container does not start at all, and Incus answers with a
// forkmount error that says nothing about the cause. Checking first turns that
// into the commands to run.
const (
	subuidPath = "/etc/subuid"
	subgidPath = "/etc/subgid"
)

// uidMappingProblem says why Incus cannot map this machine's user into the
// sandbox, or reports false when nothing is in the way.
func uidMappingProblem() (string, bool) {
	uid := os.Getuid()

	// The container uid needs no mapping: coi asks for no raw.idmap then.
	if uid == containerUser {
		return "", false
	}

	for _, path := range []string{subuidPath, subgidPath} {
		if !delegatedToRoot(path, uid) {
			return fmt.Sprintf("%s gives root no delegation for uid %d", path, uid), true
		}
	}

	// Incus reads both files at start, so a delegation added afterwards only
	// takes effect once it has been restarted.
	started, ok := incusdStartTime()
	if !ok {
		return "", false
	}

	for _, path := range []string{subuidPath, subgidPath} {
		if info, err := os.Stat(path); err == nil && info.ModTime().After(started.Add(time.Second)) {
			return fmt.Sprintf("%s was changed after Incus started, so Incus does not know about uid %d yet", path, uid), true
		}
	}

	return "", false
}

// uidMappingFix is what makes the mapping possible, ready to paste: the two
// delegations of the host uid, then the restart that makes Incus read them.
func uidMappingFix(uid int) string {
	return fmt.Sprintf(`  sudo sh -c 'echo "root:%[1]d:1" >> /etc/subuid; echo "root:%[1]d:1" >> /etc/subgid'
  sudo systemctl restart incus

Incus maps a host uid into a container only when root has been delegated that
uid, and lares needs the mapping because the sandbox runs its commands as uid
%[2]d while this machine's user is uid %[1]d.`, uid, containerUser)
}

// requireUIDMapping refuses to start the sandbox when Incus could not map this
// machine's user into it. It is called where a container is about to start,
// which is the only moment the mapping is needed.
func requireUIDMapping() error {
	problem, blocked := uidMappingProblem()
	if !blocked {
		return nil
	}

	return fmt.Errorf("the sandbox cannot start: %s\n\nto fix it:\n%s", problem, uidMappingFix(os.Getuid()))
}

// warnUIDMapping reports the same problem without stopping anything, for the
// install: the profile is in place, the sandbox just cannot start yet.
func warnUIDMapping() {
	problem, blocked := uidMappingProblem()
	if !blocked {
		return
	}

	fmt.Fprintf(os.Stderr, "lares: the sandbox cannot start yet: %s\n\nto fix it:\n%s\n", problem, uidMappingFix(os.Getuid()))
}

// delegatedToRoot reports whether a subordinate id file delegates exactly this
// uid to root. A wider root range does not count: Incus rejects raw.idmap for a
// uid inside one.
func delegatedToRoot(path string, uid int) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Split(strings.TrimSpace(line), ":")
		if len(fields) != 3 || (fields[0] != "root" && fields[0] != "0") {
			continue
		}

		start, startErr := strconv.Atoi(strings.TrimSpace(fields[1]))
		count, countErr := strconv.Atoi(strings.TrimSpace(fields[2]))

		if startErr == nil && countErr == nil && start == uid && count == 1 {
			return true
		}
	}

	return false
}

// incusdStartTime returns when the running Incus daemon started, which is what
// tells a delegation that Incus already read from one it has not. It reports
// false when no daemon is visible, because Incus may as well run elsewhere.
func incusdStartTime() (time.Time, bool) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return time.Time{}, false
	}

	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}

		dir := filepath.Join("/proc", entry.Name())

		cmdline, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil || !isIncusDaemon(cmdline) {
			continue
		}

		info, err := os.Stat(dir)
		if err != nil {
			continue
		}

		// A process directory carries the start time of its process.
		return info.ModTime(), true
	}

	return time.Time{}, false
}

// isIncusDaemon reports whether a /proc cmdline is the Incus daemon itself and
// not one of its helpers: forkstart and forkproxy carry the same name in their
// title, and they are not restarted with the daemon.
func isIncusDaemon(cmdline []byte) bool {
	args := strings.Split(strings.TrimRight(string(cmdline), "\x00"), "\x00")
	if len(args) == 0 || filepath.Base(args[0]) != "incusd" {
		return false
	}

	for _, arg := range args[1:] {
		if !strings.HasPrefix(arg, "-") {
			return false
		}
	}

	return true
}

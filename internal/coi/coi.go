// Package coi drives the coi command line and the Incus API behind it.
//
// lares does not talk to Incus directly: coi owns the containers, their
// profiles and their sessions, and everything lares needs to know is either
// reported by `coi list --json` or read from Incus with `incus query`.
package coi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrNoSandbox is returned when no container carries the alias lares looked for.
var ErrNoSandbox = errors.New("no sandbox")

// Container is what `coi list --json` reports about one sandbox.
type Container struct {
	Name      string `json:"name"`
	Alias     string `json:"alias"`
	Status    string `json:"status"`
	Workspace string `json:"workspace"`
	IPv4      string `json:"ipv4"`
}

// Running reports whether the container is up.
func (c Container) Running() bool {
	return c.Status == "Running"
}

// listing mirrors the top level of `coi list --json`.
type listing struct {
	Active []Container `json:"active_containers"`
}

// List returns the containers coi is currently managing.
func List() ([]Container, error) {
	out, err := output("list", "--json")
	if err != nil {
		return nil, err
	}

	var listed listing

	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		return nil, fmt.Errorf("reading the output of coi list: %w", err)
	}

	return listed.Active, nil
}

// Find returns the container carrying the given alias, which is how lares names
// the sandbox: the profile, the alias and the command all carry the same name.
func Find(alias string) (Container, error) {
	containers, err := List()
	if err != nil {
		return Container{}, err
	}

	for _, container := range containers {
		if container.Alias == alias {
			return container, nil
		}
	}

	return Container{}, fmt.Errorf("%w with alias %q", ErrNoSandbox, alias)
}

// Start boots a container that is not running.
func Start(name string) error {
	return Run("container", "start", name)
}

// HasImage reports whether coi has an image carrying this alias.
func HasImage(name string) bool {
	_, err := output("image", "exists", name)

	return err == nil
}

// DeleteImage removes a coi image.
func DeleteImage(name string) error {
	return Run("image", "delete", name)
}

// Mount adds a host directory to a running container, as `coi container mount`
// does.
func Mount(name, device, source, path string) error {
	return Run("container", "mount", name, device, source, path)
}

// Unmount removes a disk device from a container, which detaches the host
// directory it mounts without stopping the container.
func Unmount(name, device string) error {
	_, err := incus("config", "device", "remove", name, device)

	return err
}

// PushFile copies a host file into a container, with the destination written as
// "<container>:<path>".
func PushFile(local, destination string) error {
	_, err := output("file", "push", local, destination)

	return err
}

// Exec runs a command in the container without attaching it to the terminal.
// The output of the command is dropped: it is used for the setup steps whose
// result only matters when they fail.
func Exec(name string, args ...string) error {
	_, err := output(append([]string{"container", "exec", name, "--"}, args...)...)

	return err
}

// Output runs a command in the container and returns what it wrote to standard
// output.
func Output(name string, args ...string) (string, error) {
	return output(append([]string{"container", "exec", name, "--"}, args...)...)
}

// Shell runs `coi shell` with the terminal attached, which is how the sandbox is
// created the first time it is entered.
func Shell(dir string, args ...string) error {
	cmd := exec.Command("coi", append([]string{"shell"}, args...)...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Run runs a coi command with its output on ours.
func Run(args ...string) error {
	cmd := exec.Command("coi", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// Show runs a coi command with its standard output kept as it is, for the
// commands whose output is the point, like `coi list`.
func Show(args ...string) error {
	cmd := exec.Command("coi", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// output runs a coi command and returns its standard output.
func output(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	cmd := exec.Command("coi", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return "", fmt.Errorf("coi %s: %w: %s", strings.Join(args, " "), err, message)
		}

		return "", fmt.Errorf("coi %s: %w", strings.Join(args, " "), err)
	}

	return stdout.String(), nil
}

// MountSources returns the host paths mounted in a container. Every one of them
// is a directory the container can be entered from, which is what lares checks
// before starting a tool in the current directory.
func MountSources(name string) ([]string, error) {
	devices, err := diskDevices(name)
	if err != nil {
		return nil, err
	}

	var sources []string

	for _, source := range devices {
		sources = append(sources, source)
	}

	return sources, nil
}

// MountDevice returns the name of the device mounting a host path in a
// container, and whether there is one. The name is what unmounting takes, and it
// is not derivable from the path: a container created from a profile names its
// devices after the order of the mounts, not after the directory.
func MountDevice(name, source string) (string, bool, error) {
	devices, err := diskDevices(name)
	if err != nil {
		return "", false, err
	}

	for device, mounted := range devices {
		if mounted == source {
			return device, true, nil
		}
	}

	return "", false, nil
}

// diskDevices returns the disk devices of a container, mapping the device name
// to the host path it mounts.
func diskDevices(name string) (map[string]string, error) {
	// `incus config device show` writes YAML, which is not worth parsing when
	// the API answers the same question in JSON.
	out, err := incus("query", "/1.0/instances/"+name)
	if err != nil {
		return nil, err
	}

	var instance struct {
		Devices map[string]struct {
			Type   string `json:"type"`
			Source string `json:"source"`
		} `json:"devices"`
	}

	if err := json.Unmarshal([]byte(out), &instance); err != nil {
		return nil, fmt.Errorf("reading the devices of %q: %w", name, err)
	}

	devices := make(map[string]string)

	for device, config := range instance.Devices {
		if config.Type == "disk" && config.Source != "" {
			devices[device] = config.Source
		}
	}

	return devices, nil
}

// incus runs an Incus command and returns its standard output.
func incus(args ...string) (string, error) {
	var stdout, stderr bytes.Buffer

	cmd := exec.Command("incus", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return "", fmt.Errorf("incus %s: %w: %s", strings.Join(args, " "), err, message)
		}

		return "", fmt.Errorf("incus %s: %w", strings.Join(args, " "), err)
	}

	return stdout.String(), nil
}

// Package clipboard writes the clipboard of the host session, with the tool
// that matches the display server in use.
package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// HandlesOSC52 reports whether the terminal lares is attached to sets the
// clipboard itself when it receives an OSC 52 sequence.
//
// VTE, the terminal widget behind Ptyxis and GNOME Terminal, does not implement
// OSC 52 and exports VTE_VERSION, which is the only way to tell those terminals
// apart from a program. The others are assumed to do the right thing: foot,
// Ghostty, kitty and xterm all set the clipboard.
func HandlesOSC52() bool {
	return os.Getenv("VTE_VERSION") == ""
}

// Command returns the program that writes the clipboard of the session lares
// runs in, and its arguments.
//
// Wayland comes first: a Wayland session usually has X11 running through
// XWayland as well, and the X11 clipboard is not the one desktop applications
// read.
func Command() (string, []string, error) {
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return "wl-copy", nil, nil
	case os.Getenv("DISPLAY") != "":
		if _, err := exec.LookPath("xclip"); err == nil {
			return "xclip", []string{"-selection", "clipboard"}, nil
		}

		return "xsel", []string{"--clipboard", "--input"}, nil
	}

	return "", nil, fmt.Errorf("no display server")
}

// Write puts text in the clipboard of the host session. wl-copy and xclip both
// fork, so the clipboard keeps its content once lares has exited.
func Write(text string) error {
	command, args, err := Command()
	if err != nil {
		return err
	}

	cmd := exec.Command(command, args...)
	cmd.Stdin = strings.NewReader(text)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running %s: %w", command, err)
	}

	return nil
}

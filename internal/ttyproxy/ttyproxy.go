// Package ttyproxy runs a command between the host terminal and the sandbox,
// copying the clipboard writes it makes to the clipboard of the host session.
//
// The command usually talks to the terminal directly, which is what the shell
// would do. Only the terminals that drop OSC 52 need lares in the middle, and
// then the command still gets a terminal: it is given a PTY of its own rather
// than a pipe, because a program that cannot read its terminal size falls back
// to 80x25 and stops following resizes. Incus, which coi runs underneath, reads
// that size from its standard output and drops the session on SIGWINCH when
// there is no terminal there to ask.
package ttyproxy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/riko/lares/internal/clipboard"
	"github.com/riko/lares/internal/osc52"
	"golang.org/x/term"
)

// drainGrace is how long Run keeps reading once the command has exited, so that
// the last bytes it wrote are not lost when the process is already gone.
const drainGrace = 300 * time.Millisecond

// Run runs argv and returns the exit code of the command. It only returns an
// error when the command could not be started at all.
//
// On a terminal that sets the clipboard itself, Run hands the process over with
// exec: the command then behaves exactly as if the shell had started it.
func Run(argv []string) (int, error) {
	if len(argv) == 0 {
		return 0, fmt.Errorf("no command given")
	}

	path, err := exec.LookPath(argv[0])
	if err != nil {
		return 0, err
	}

	if !needsRelay() {
		return 0, syscall.Exec(path, argv, os.Environ())
	}

	return relay(path, argv)
}

// needsRelay reports whether lares has to do the copying itself. Anywhere else
// the terminal sets the clipboard on its own and relaying as well would only
// write the same selection twice.
func needsRelay() bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return false
	}

	if clipboard.HandlesOSC52() {
		return false
	}

	_, _, err := clipboard.Command()

	return err == nil
}

// relay runs the command on a PTY of its own, copies its clipboard writes and
// forwards the terminal in both directions.
func relay(path string, argv []string) (int, error) {
	cmd := exec.Command(path, argv[1:]...)

	size, err := pty.GetsizeFull(os.Stdout)
	if err != nil {
		size = &pty.Winsize{Rows: 24, Cols: 80}
	}

	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return 0, fmt.Errorf("starting %s on a terminal: %w", argv[0], err)
	}

	defer func() { _ = ptmx.Close() }()

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return 0, fmt.Errorf("switching the terminal to raw mode: %w", err)
	}

	defer func() { _ = term.Restore(int(os.Stdin.Fd()), state) }()

	resizes := make(chan os.Signal, 1)
	signal.Notify(resizes, syscall.SIGWINCH)
	defer signal.Stop(resizes)

	// The keys go to the command as they are typed.
	go func() { _, _ = io.Copy(ptmx, os.Stdin) }()

	// Wait has to run in its own goroutine to be reaped while the output is
	// still being read. The exit code is sent before the channel is closed, so
	// that reading it never blocks once the command is known to be gone.
	codes := make(chan int, 1)
	exited := make(chan struct{})

	go func() {
		_ = cmd.Wait()
		codes <- exitCode(cmd)

		close(exited)
	}()

	output := make(chan []byte)

	go func() {
		defer close(output)

		buf := make([]byte, 32<<10)

		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])

				output <- chunk
			}

			if err != nil {
				return
			}
		}
	}()

	copying := osc52.NewRelay(os.Stdout, clipboard.Write)
	command := exited

	// grace stops the relay when the command is gone but something it left
	// behind still holds the terminal open.
	var grace <-chan time.Time

	for {
		select {
		case chunk, ok := <-output:
			if !ok {
				return <-codes, nil
			}

			_, _ = copying.Write(chunk)
		case <-command:
			command = nil
			grace = time.After(drainGrace)
		case <-grace:
			return <-codes, nil
		case <-resizes:
			_ = pty.InheritSize(os.Stdout, ptmx)
		}
	}
}

// exitCode reads the status the command exited with, reporting a command killed
// by a signal the way a shell does.
func exitCode(cmd *exec.Cmd) int {
	state := cmd.ProcessState
	if state == nil {
		return 0
	}

	if code := state.ExitCode(); code >= 0 {
		return code
	}

	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}

	return 1
}

// Package osc52 copies the clipboard writes of a program running inside a
// sandbox to the clipboard of the host session.
//
// A sandbox has no display server, so an agent inside it cannot run wl-copy or
// xclip: it copies by asking the terminal to set the clipboard with an OSC 52
// sequence, which travels back to the host with the rest of its output.
// Terminals built on VTE (Ptyxis, GNOME Terminal) parse those sequences and
// drop them, which leaves the copy nowhere to go. lares sits between the two,
// so it sets the host clipboard itself when it recognises such a terminal.
// Anywhere else the terminal does it, as the agent expects, and relaying as
// well would only write the same selection twice.
package osc52

import (
	"bytes"
	"encoding/base64"
	"io"
)

// start is the beginning of the sequence that carries a clipboard write. The
// sequence ends with BEL or with ST (ESC \).
const start = "\x1b]52;"

// MaxSequence bounds what the relay buffers while it waits for the end of a
// sequence a read cut in half. The agent refuses to emit more than 100000
// base64 characters, so the limit only has to sit comfortably above that.
const MaxSequence = 128 << 10

// Relay is the writer lares hands to the exec stream of the sandbox. It passes
// the output through untouched, and copies the text carried by the OSC 52
// sequences to the host clipboard on the way.
type Relay struct {
	out          io.Writer
	setClipboard func(string) error

	// pending holds the beginning of a sequence a read cut in half, which the
	// next write completes.
	pending []byte
}

// NewRelay returns a relay writing to out and setting the clipboard with set.
func NewRelay(out io.Writer, set func(string) error) *Relay {
	return &Relay{out: out, setClipboard: set}
}

// Write passes p through and copies the clipboard writes it carries. It returns
// what the underlying writer accepted, so that a short write is reported as the
// io.Writer contract requires.
func (r *Relay) Write(p []byte) (int, error) {
	n, err := r.out.Write(p)
	r.relay(p[:n])

	return n, err
}

// relay copies the clipboard writes found in p, keeping for the next call the
// sequence a read cut in half.
func (r *Relay) relay(p []byte) {
	data := append(r.pending, p...)
	r.pending = nil

	for {
		at := bytes.Index(data, []byte(start))

		if at < 0 {
			r.keepTrailingStart(data)

			return
		}

		body := data[at+len(start):]

		end, terminator := sequenceEnd(body)
		if end < 0 {
			// The sequence is not complete yet. Keeping it would be pointless
			// once it can no longer be a clipboard write an agent produced.
			if len(body) <= MaxSequence {
				r.pending = append([]byte(nil), data[at:]...)
			}

			return
		}

		r.setFromSequence(body[:end])
		data = body[end+terminator:]
	}
}

// keepTrailingStart remembers the end of data when it could be the beginning of
// a sequence the next write completes.
func (r *Relay) keepTrailingStart(data []byte) {
	for length := len(start) - 1; length > 0; length-- {
		if len(data) < length || !bytes.Equal(data[len(data)-length:], []byte(start[:length])) {
			continue
		}

		r.pending = append([]byte(nil), data[len(data)-length:]...)

		return
	}
}

// sequenceEnd returns where the body of a sequence stops and the length of its
// terminator, or -1 when the sequence is not complete yet.
func sequenceEnd(body []byte) (int, int) {
	bel := bytes.IndexByte(body, '\x07')
	st := bytes.Index(body, []byte("\x1b\\"))

	switch {
	case bel < 0 && st < 0:
		return -1, 0
	case st < 0:
		return bel, 1
	case bel >= 0 && bel < st:
		return bel, 1
	default:
		return st, 2
	}
}

// setFromSequence reads the body of a sequence, "<selection>;<base64 text>". Only
// the clipboard selection carries something to copy. A payload of "?" is a read
// request, which fails to decode and is dropped like everything else.
func (r *Relay) setFromSequence(body []byte) {
	selection, payload, found := bytes.Cut(body, []byte(";"))
	if !found || string(selection) != "c" {
		return
	}

	text, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		return
	}

	// The clipboard is a convenience: a session without a clipboard tool, or
	// with one that refuses, is no reason to lose the output of the command.
	_ = r.setClipboard(string(text))
}

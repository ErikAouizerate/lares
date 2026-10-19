package osc52

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// sequence builds the OSC 52 sequence a program inside the sandbox writes to
// set the clipboard.
func sequence(selection, text, terminator string) string {
	return start + selection + ";" + base64.StdEncoding.EncodeToString([]byte(text)) + terminator
}

func TestRelayCopiesTheClipboardWrites(t *testing.T) {
	hello := sequence("c", "hello", "\x07")
	unterminated := start + "c;" + strings.Repeat("A", MaxSequence+10)

	cases := map[string]struct {
		writes []string
		want   []string
	}{
		"sequence on its own":        {writes: []string{hello}, want: []string{"hello"}},
		"between two lines of text":  {writes: []string{"before\n" + hello + "after\n"}, want: []string{"hello"}},
		"terminated by ST":           {writes: []string{sequence("c", "hello", "\x1b\\")}, want: []string{"hello"}},
		"two sequences in one write": {writes: []string{sequence("c", "one", "\x07") + sequence("c", "two", "\x07")}, want: []string{"one", "two"}},
		"cut in the middle":          {writes: []string{hello[:11], hello[11:]}, want: []string{"hello"}},
		"cut inside the start":       {writes: []string{"text\x1b]5", "2;c;aGVsbG8=\x07"}, want: []string{"hello"}},
		"cut after the escape":       {writes: []string{"text\x1b", hello[1:]}, want: []string{"hello"}},
		"primary selection":          {writes: []string{sequence("p", "hello", "\x07")}, want: nil},
		"read request":               {writes: []string{start + "c;?\x07"}, want: nil},
		"another sequence":           {writes: []string{"\x1b]8;;https://example.com\x07link"}, want: nil},
		"payload is not base64":      {writes: []string{start + "c;not base64!\x07"}, want: nil},
		"nothing terminates it":      {writes: []string{start + "c;aGVs"}, want: nil},
		"dropped after the limit":    {writes: []string{unterminated}, want: nil},
		"kept on writing after":      {writes: []string{unterminated, hello}, want: []string{"hello"}},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			var copied []string

			var out bytes.Buffer

			relay := NewRelay(&out, func(text string) error {
				copied = append(copied, text)

				return nil
			})

			var written int

			for _, write := range testCase.writes {
				written += len(write)

				if _, err := relay.Write([]byte(write)); err != nil {
					t.Fatalf("writing %q: %v", write, err)
				}
			}

			if out.Len() != written {
				t.Errorf("wrote %d bytes of %d through an incomplete sequence", out.Len(), written)
			}

			if len(copied) != len(testCase.want) {
				t.Fatalf("copied %q, want %q", copied, testCase.want)
			}

			for i, want := range testCase.want {
				if copied[i] != want {
					t.Errorf("copied[%d] = %q, want %q", i, copied[i], want)
				}
			}
		})
	}
}

// TestRelayPassesEverythingThrough checks that the relay never drops the output
// of the command, which is what the terminal has to display.
func TestRelayPassesEverythingThrough(t *testing.T) {
	hello := sequence("c", "hello", "\x07")

	cases := []string{
		"plain output\n",
		"before\n" + hello + "after\n",
		start + "c;aGVs",
		"text\x1b",
	}

	for _, in := range cases {
		var out bytes.Buffer

		relay := NewRelay(&out, func(string) error { return nil })

		// Write it byte by byte: a relay that mishandles partial sequences would
		// lose bytes along the way.
		for i := 0; i < len(in); i++ {
			if _, err := relay.Write([]byte{in[i]}); err != nil {
				t.Fatalf("writing byte %d: %v", i, err)
			}
		}

		if out.String() != in {
			t.Errorf("output = %q, want %q", out.String(), in)
		}
	}
}

// TestRelaySurvivesAFailingClipboard checks that a clipboard tool that refuses
// does not break the output of the command: the sequence is passed through like
// any other byte, and the failure is swallowed.
func TestRelaySurvivesAFailingClipboard(t *testing.T) {
	clipboard := sequence("c", "hello", "\x07")

	var out bytes.Buffer

	relay := NewRelay(&out, func(string) error { return errors.New("no clipboard") })

	if _, err := relay.Write([]byte(clipboard)); err != nil {
		t.Fatalf("relay failed on a clipboard error: %v", err)
	}

	if out.String() != clipboard {
		t.Errorf("output = %q, want %q", out.String(), clipboard)
	}
}

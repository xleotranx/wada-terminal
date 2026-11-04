// Package status reads the Program Status Protocol (OSC 7501) out of what a
// program writes to its terminal, and the one mode a window needs that the
// terminal emulator does not report: whether the program asked for bracketed
// paste. It parses no screen; the terminal emulator does that.
package status

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

// The Program Status Protocol (OSC 7501) is how a program tells the terminal
// what it is doing: idle, working, done, blocked on the user, or failed. A
// program that speaks it is an agent here; one that does not is a program,
// and the window falls back to reading its screen.
//
// The specification is at https://www.superlogical.com/rex/docs/build/program-status.
// Every report is a list of key=value pairs separated by colons:
//
//	ESC ] 7501 ; state=working:app=cargo:progress=40 ESC \
//
// A report replaces the record it names completely. The report without an id
// is the program's own record; one with an id reports on part of its work,
// and a slash makes one record the child of another. "state=clear" removes a
// record and its children, or every record when it names none.
const (
	// statusMaxBody is the longest report body, in bytes.
	statusMaxBody = 4096 - len("\x1b]7501;") - len("\x1b\\")
	// statusMaxTitle and statusMaxMsg bound the text after base64 decoding.
	statusMaxTitle = 192
	statusMaxMsg   = 2048
	// statusMaxTitleEncoded and statusMaxMsgEncoded bound it before decoding.
	statusMaxTitleEncoded = 256
	statusMaxMsgEncoded   = 2732
	// statusMaxApp, statusMaxID, statusMaxIDSegment and statusMaxIDDepth
	// bound the names a program reports.
	statusMaxApp       = 32
	statusMaxID        = 128
	statusMaxIDSegment = 32
	statusMaxIDDepth   = 8
	statusMaxKey       = 16
	// QueryReply answers a program that asks which states the terminal
	// knows (OSC 7501 ; ?).
	QueryReply          = "\x1b]7501;?:states=idle,working,done,blocked,error:kinds=permission,question,auth\x1b\\"
	statusNameBytes     = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.+-"
	statusValueAlphabet = statusNameBytes + ",/="
)

// Status is one record a program reported over OSC 7501. Its words are the
// program's own; the window does not translate them. Progress is -1 when the
// program sent none, which is different from 0.
type Status struct {
	State    string // idle, working, done, blocked, error, or clear
	Kind     string // permission, question or auth, for a blocked record
	Progress int    // 0 to 100, for a working or blocked record; -1 if absent
	ID       string // "" for the program's own record
	App      string // a stable name for the program, such as "claude-code"
	Title    string // a short label for this record
	Message  string // one line saying what the record is doing or waiting for
}

// Clear reports whether the report removes records rather than adding one.
func (s Status) Clear() bool { return s.State == "clear" }

// parseStatus parses the body of an OSC 7501 report, everything after
// "7501;". It returns false when the report breaks the specification, in
// which case nothing from it is applied. Malformed pairs and unknown keys are
// skipped; an unknown state, a bad id, or bad base64 text discards the whole
// report, so a report meant for one record never falls back to the root.
func parseStatus(body string) (Status, bool) {
	if len(body) > statusMaxBody {
		return Status{}, false
	}
	if body == "?" {
		// A query, handled by the caller; not a report.
		return Status{}, false
	}

	fields := make(map[string]string, 7)
	for _, pair := range strings.Split(body, ":") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || len(key) > statusMaxKey {
			if len(key) > statusMaxKey {
				return Status{}, false // a key over the limit discards the report
			}
			continue
		}
		value = strings.TrimSpace(value)
		if !statusValue(value) {
			continue // a value with bytes outside the alphabet is skipped
		}
		switch key {
		case "state", "id", "kind", "progress", "app", "title", "msg":
			fields[key] = value // the last value wins
		}
	}

	state, ok := fields["state"]
	if !ok || !validStatusState(state) {
		return Status{}, false
	}

	// Every id is checked, even one a later pair replaces, because an
	// invalid id would make the report fall back to the root record.
	for _, pair := range strings.Split(body, ":") {
		key, value, ok := strings.Cut(pair, "=")
		if !ok || strings.TrimSpace(key) != "id" {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" && !validStatusID(value) {
			return Status{}, false
		}
	}
	if id := fields["id"]; id != "" && !validStatusID(id) {
		return Status{}, false
	}

	st := Status{State: state, Progress: -1, ID: fields["id"]}
	if app := fields["app"]; app != "" && validStatusName(app) {
		st.App = app
	}
	if app := fields["app"]; len(app) > statusMaxApp || len(fields["title"]) > statusMaxTitleEncoded || len(fields["msg"]) > statusMaxMsgEncoded {
		return Status{}, false
	}
	if state == "blocked" {
		switch kind := fields["kind"]; kind {
		case "permission", "question", "auth":
			st.Kind = kind
		}
	}
	if state == "working" || state == "blocked" {
		if progress, ok := parseStatusProgress(fields["progress"]); ok {
			st.Progress = progress
		}
	}
	if encoded := fields["title"]; encoded != "" {
		text, ok := decodeStatusText(encoded, statusMaxTitle)
		if !ok {
			return Status{}, false
		}
		st.Title = text
	}
	if encoded := fields["msg"]; encoded != "" {
		text, ok := decodeStatusText(encoded, statusMaxMsg)
		if !ok {
			return Status{}, false
		}
		st.Message = text
	}
	return st, true
}

// statusValue reports whether every byte of a value is one the protocol
// allows. Pairs with anything else are skipped rather than guessed at.
func statusValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if !strings.ContainsRune(statusValueAlphabet, rune(value[i])) {
			return false
		}
	}
	return true
}

// validStatusState is the set of states the protocol defines.
func validStatusState(state string) bool {
	switch state {
	case "idle", "working", "done", "blocked", "error", "clear":
		return true
	}
	return false
}

// validStatusName is a name an app or an id segment can hold.
func validStatusName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !strings.ContainsRune(statusNameBytes, rune(name[i])) {
			return false
		}
	}
	return true
}

// validStatusID checks names joined by single slashes, within the limits.
func validStatusID(id string) bool {
	if len(id) > statusMaxID {
		return false
	}
	depth := 0
	for _, segment := range strings.Split(id, "/") {
		depth++
		if depth > statusMaxIDDepth || len(segment) > statusMaxIDSegment || !validStatusName(segment) {
			return false
		}
	}
	return true
}

// parseStatusProgress reads a whole number from 0 to 100; anything else is
// absent, as the protocol says.
func parseStatusProgress(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	n := 0
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
		if n > 100 {
			return 0, false
		}
	}
	return n, true
}

// decodeStatusText decodes base64 text, with or without padding, and checks
// that it is UTF-8 and holds no control characters. A report with text that
// is not safe to show is discarded whole.
func decodeStatusText(encoded string, limit int) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(encoded)
	}
	if err != nil || len(raw) > limit || !utf8.Valid(raw) {
		return "", false
	}
	for _, r := range string(raw) {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return string(raw), true
}

package terminal

import (
	"strings"

	"wada-terminal/internal/status"
)

// applyStatusReports moves the session's state to what the program's OSC
// 7501 reports say, in order. A program that reports is an agent whatever
// kind it started as: the protocol, not the program's name, is what makes an
// agent here.
func (s *session) applyStatusReports(reports []status.Status) {
	for _, st := range reports {
		s.applyStatusReport(st)
	}
}

// applyStatusReport is the window's copy of herdr's seen rule. A working or
// blocked agent has not finished, so it counts as seen; when it leaves those
// states, it is seen only if the user was looking at its pane. An unseen
// finish is what the window calls Done. A report for a child record changes
// no root state, but it still marks the program as one that reports.
func (s *session) applyStatusReport(st status.Status) {
	// Any report takes the program out of the screen-reading fallback for
	// good: a protocol this explicit should never be second-guessed.
	if !s.reported {
		s.reported = true
		s.busy, s.blocked, s.done, s.asked = false, false, false, false
	}
	if st.App != "" {
		s.app = st.App
	}
	if st.ID != "" {
		// The program reported on one part of its work, not itself.
		return
	}
	if st.Clear() {
		s.word, s.wordKind, s.wordProgress = "", "", -1
		s.statusTitle, s.statusMsg = "", ""
		return
	}

	wasBusy := s.word == "working" || s.word == "blocked"
	looking := s.looking()
	switch st.State {
	case "working", "blocked":
		s.seen = true
	default:
		if wasBusy {
			s.seen = looking
		}
	}
	s.word = st.State
	s.wordKind = st.Kind
	s.wordProgress = st.Progress
	s.statusTitle = st.Title
	s.statusMsg = st.Message
}

// looking reports whether the user is watching this session's pane: it has
// the keyboard and the window is in front.
func (s *session) looking() bool {
	if s.t == nil {
		return false
	}
	if !s.t.focused(s) {
		return false
	}
	return s.t.host == nil || s.t.host.Foreground()
}

// isAgent reports whether a session is an agent: it runs a known agent's
// program, or its program speaks OSC 7501. A shell that starts an agent which
// reports becomes one; a program that never reports stays a terminal.
func (s *session) isAgent() bool {
	return s != nil && (s.agent || s.kind != kindShell || s.reported)
}

// statusWord is the program's own 7501 word, empty when it has none.
func (s *session) statusWord() string {
	if !s.reported {
		return ""
	}
	return s.word
}

// agentName is what to call a reporting program whose session did not start
// as a known agent: its own app name, made readable, or the pane's title.
func (s *session) agentName() string {
	if s.app == "" {
		return s.name
	}
	switch s.app {
	case "claude-code", "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	case "opencode":
		return "OpenCode"
	}
	words := strings.Fields(strings.ReplaceAll(s.app, "-", " "))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

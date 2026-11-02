package terminal

import (
	"context"
	"strings"
	"time"

	"wada-terminal/internal/config"
	"wada-terminal/internal/vt"
)

// agentState is what a pane holds, as the Sessions reports it. Herdr's
// vocabulary, narrowed to what a terminal can see: an agent gets work, draws
// its progress, asks for an approval, and finishes in a pane the user may not
// be looking at.
type agentState uint8

const (
	// agentIdle is a program waiting at its prompt.
	agentIdle agentState = iota
	// agentWorking is a program that just drew or was just given a key.
	agentWorking
	// agentBlocked is an agent whose screen asks for an approval or a choice.
	agentBlocked
	// agentDone is an agent that finished while the user looked elsewhere.
	agentDone
	// agentStopped is a program that exited.
	agentStopped
)

// label names a state in the index and a pane's header.
func (s agentState) label() string {
	switch s {
	case agentWorking:
		return "Working"
	case agentBlocked:
		return "Needs input"
	case agentDone:
		return "Done"
	case agentStopped:
		return "Stopped"
	default:
		return "Idle"
	}
}

// settleDelay is how long an agent's pane may be quiet before the
// Sessions decides it stopped working. It is long enough for a program
// that pauses between two lines, and short enough to notice a prompt.
const settleDelay = 3 * time.Second

// tickInterval is how often a pane that is working is checked for quiet.
const tickInterval = 500 * time.Millisecond

// agentOutput notes that an agent drew something: it is working until its
// pane stays quiet for settleDelay.
func (t *Plugin) agentOutput(s *session) {
	if s == nil || !s.isAgent() || s.reported || !s.alive {
		return
	}
	s.blocked = false
	s.done = false
	s.busy = true
	s.lastWork = time.Now()
	t.startAgentWatch()
}

// prompted notes that the user typed into a pane, which is what gives an
// agent something to finish.
func (t *Plugin) prompted(s *session) {
	if s == nil || !s.isAgent() || s.reported || !s.alive {
		return
	}
	s.done = false // typing in a pane is looking at it
	s.asked = true
	t.agentOutput(s)
}

// startAgentWatch starts the quiet watcher the first time an agent works,
// and wakes it when it is already running.
func (t *Plugin) startAgentWatch() {
	if t.agentWake == nil {
		return // a session made outside a window, as in a test
	}
	if t.agentWatching.CompareAndSwap(false, true) {
		select {
		case t.agentWake <- struct{}{}:
		default:
		}
	}
}

// watchAgents checks the working panes for quiet, a tick at a time, and stops
// while every agent is idle, blocked or done. It runs in a scope worker; the
// state it reads and changes belongs to the UI thread, which is where
// settleAgents runs.
func (t *Plugin) watchAgents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.agentWake:
		}
		for t.agentWatching.Load() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(tickInterval):
			}
			t.ctx.PostQuiet(t.settleAgents)
		}
	}
}

// settleAgents is a tick on the UI thread: every agent whose pane has been
// quiet for settleDelay stops working, and its screen decides whether it is
// blocked, done or idle. A frame is drawn only when what the window shows
// changed.
func (t *Plugin) settleAgents() {
	now := time.Now()
	foreground := t.host != nil && t.host.Foreground()
	live, changed := false, false
	for _, s := range t.allSessions() {
		if !s.busy {
			continue
		}
		if now.Sub(s.lastWork) < settleDelay {
			live = true
			continue
		}
		if s.settle(t.focused(s) && foreground) {
			changed = true
		}
	}
	// New output between the check above and this line wakes the watcher
	// again, so stopping here cannot leave a working pane unread.
	if !live {
		t.agentWatching.Store(false)
	}
	if changed {
		t.ctx.Invalidate()
	} else {
		t.ctx.FrameDue()
	}
}

// agentState is what the index and the state dots report for a session.
//
// A program that speaks OSC 7501 is believed over any reading of its screen:
// working and blocked are its own words, error counts as blocked, and any
// other finish is Done until the user looks at it. A program that does not
// report is read from its screen by the quiet watcher.
func (s *session) agentState() agentState {
	if !s.alive {
		return agentStopped
	}
	if s.reported {
		switch s.word {
		case "working":
			return agentWorking
		case "blocked", "error":
			return agentBlocked
		case "":
			return agentIdle
		}
		if !s.seen {
			return agentDone
		}
		return agentIdle
	}
	switch {
	case s.blocked:
		return agentBlocked
	case s.busy:
		return agentWorking
	case s.done:
		return agentDone
	default:
		return agentIdle
	}
}

// stateLabel names what a session holds; a shell is running rather than idle
// between prompts. A blocked program's own word says what it waits for.
func (s *session) stateLabel() string {
	if s.isAgent() {
		if s.agentState() == agentBlocked {
			switch s.wordKind {
			case "permission":
				return "Needs approval"
			case "question":
				return "Needs answer"
			case "auth":
				return "Needs login"
			}
		}
		return s.agentState().label()
	}
	if s.alive {
		return "Running"
	}
	return "Stopped"
}

// settle ends a quiet working spell and returns whether the state the window
// shows changed.
func (s *session) settle(watched bool) bool {
	if s.reported {
		// The program's own reports are the state; a quiet screen says
		// nothing about an agent that speaks the protocol.
		s.busy = false
		return false
	}
	before := s.agentState()
	s.busy = false
	if s.isAgent() {
		if blockedOnScreen(s.screen) {
			s.blocked = true
			s.done = false
		} else {
			s.blocked = false
			switch {
			case watched:
				// The user watched it finish: there is nothing to announce.
				s.asked, s.done = false, false
			case s.asked:
				s.done = true
			}
		}
	}
	return s.agentState() != before
}

// seen records that the user looked at a finished agent, which clears the
// done badge until it is given work again.
func (t *Plugin) seen(s *session) {
	if s == nil {
		return
	}
	if s.reported {
		if !s.seen {
			s.seen = true
			if t.ctx != nil {
				t.ctx.Invalidate()
			}
		}
		return
	}
	if !s.done {
		return
	}
	s.done, s.asked = false, false
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// attention is how many agents need the user, and whether the first of them
// is blocked on an approval or only finished.
func (t *Plugin) attention() (agentState, int) {
	blocked, done := 0, 0
	for _, s := range t.allSessions() {
		if !s.isAgent() {
			continue
		}
		switch s.agentState() {
		case agentBlocked:
			blocked++
		case agentDone:
			done++
		}
	}
	switch {
	case blocked > 0:
		return agentBlocked, blocked + done
	case done > 0:
		return agentDone, done
	}
	return agentIdle, 0
}

// attentionCount is how many agents need the user: those blocked on an
// approval and those that finished unseen.
func (t *Plugin) attentionCount() int {
	_, n := t.attention()
	return n
}

// rollup is what a workspace's agents ask of the user, the most pressing
// state first: a blocked agent, then one that finished, then one at work.
func (w *workspace) rollup() (agentState, int) {
	blocked, done, working := 0, 0, 0
	for _, s := range w.sessions(nil) {
		if !s.isAgent() {
			continue
		}
		switch s.agentState() {
		case agentBlocked:
			blocked++
		case agentDone:
			done++
		case agentWorking:
			working++
		}
	}
	switch {
	case blocked > 0:
		return agentBlocked, blocked
	case done > 0:
		return agentDone, done
	case working > 0:
		return agentWorking, working
	}
	return agentIdle, 0
}

// approvalPhrases are lines that only an approval draws. The list stays
// deliberately short: a wrong "blocked" invites the user to type into a pane
// that was only printing prose.
var approvalPhrases = []string{
	"allow this command",
	"allow this tool",
	"allow access",
	"approve this",
	"needs your approval",
	"requires approval",
	"waiting for approval",
	"permission to run",
	"run this command?",
	"press enter to confirm",
}

// questionPhrases open an approval without being one on their own, as in
// prose a program writes. They count only with an action and a question mark.
var questionPhrases = []string{
	"do you want to",
	"would you like to",
}

// actionWords are the verbs an approval question asks about.
var actionWords = map[string]bool{
	"proceed": true, "allow": true, "approve": true, "run": true,
	"execute": true, "continue": true, "create": true, "delete": true,
	"edit": true, "write": true, "modify": true, "apply": true, "use": true,
	"install": true, "commit": true, "push": true, "overwrite": true,
	"remove": true, "replace": true, "save": true, "send": true,
}

// blockedOnScreen reads the bottom of an agent's live screen, not the part
// scrolled back, and reports whether it shows an approval or a choice with a
// cursor on it.
func blockedOnScreen(screen *vt.Screen) bool {
	if screen == nil {
		return false
	}
	lines := screenTail(screen, 12)
	if len(lines) == 0 {
		return false
	}
	yes, no := false, false
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, phrase := range approvalPhrases {
			if strings.Contains(lower, phrase) {
				return true
			}
		}
		for _, phrase := range questionPhrases {
			if strings.Contains(lower, phrase) && strings.Contains(lower, "?") && hasAction(lower) {
				return true
			}
		}
		choice := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lower), "❯"))
		choice = strings.TrimSpace(strings.TrimPrefix(choice, ">"))
		switch {
		case numberedChoice(choice, "1") && strings.Contains(choice, "yes"):
			yes = true
		case (numberedChoice(choice, "2") || numberedChoice(choice, "3")) && strings.Contains(choice, "no"):
			no = true
		}
	}
	return yes && no
}

// hasAction reports whether a line asks about one of an approval's verbs.
func hasAction(line string) bool {
	for _, field := range strings.FieldsFunc(line, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if actionWords[field] {
			return true
		}
	}
	return false
}

// numberedChoice reports whether a line is the numbered choice n, as a menu
// of an agent's approval prints them.
func numberedChoice(line, n string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, n) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, n))
	return strings.HasPrefix(rest, ".") || strings.HasPrefix(rest, ")")
}

// screenTail is the last max non-empty lines of a screen, top line first.
func screenTail(screen *vt.Screen, max int) []string {
	_, rows := screen.Size()
	lines := make([]string, 0, max)
	for y := rows - 1; y >= 0 && len(lines) < max; y-- {
		var b strings.Builder
		for _, cell := range screen.BottomRow(y) {
			if cell.Width == 0 {
				continue // the second half of a wide character
			}
			if cell.Text == "" {
				b.WriteByte(' ')
			} else {
				b.WriteString(cell.Text)
			}
		}
		if line := strings.TrimSpace(b.String()); line != "" {
			lines = append(lines, line)
		}
	}
	// The rows were read bottom up; the index reads them as they show.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines
}

// detectAgent looks at the program's window title for a name programs.json
// says is an agent, so an agent started inside a shell is still one.
func (t *Plugin) detectAgent(s *session) {
	t.adoptAgent(s)
}

// adoptAgent chooses an agent entry from the foreground program first, then
// the window title. A chosen agent or a 7501 reporter is never overridden. It
// reports whether it changed what the pane shows of its program.
func (t *Plugin) adoptAgent(s *session) bool {
	if s == nil || (s.agent && !s.detected) {
		return false
	}
	before, beforeGlyph := s.agent, s.agentGlyph
	id, def, ok := t.matchAgent(s.foreground)
	if !ok {
		id, def, ok = t.matchAgent(s.screen.Title())
	}
	if ok {
		s.agent, s.detected, s.agentID = true, true, id
		s.agentGlyph, s.agentColor = def.Glyph, def.Color
	} else if s.detected {
		s.agent, s.detected, s.agentID = false, false, ""
		s.agentGlyph, s.agentColor = "", ""
	}
	return s.agent != before || s.agentGlyph != beforeGlyph
}

// matchAgent finds the programs.json entry whose match names a program.
func (t *Plugin) matchAgent(name string) (string, config.Agent, bool) {
	lower := strings.ToLower(name)
	if lower == "" {
		return "", config.Agent{}, false
	}
	for id, def := range t.agentDefs {
		for _, pattern := range def.Match {
			if pattern != "" && strings.Contains(lower, strings.ToLower(pattern)) {
				return id, def, true
			}
		}
	}
	return "", config.Agent{}, false
}

// scanForegrounds checks what each local pane's terminal is running now and
// adopts an agent whose programs.json match names it. It runs on the UI
// thread, from a posted scan, never inside a frame. A scan that finds nothing
// new asks for no frame: polling every second must not cost a redraw.
func (t *Plugin) scanForegrounds() {
	changed := false
	for _, s := range t.allSessions() {
		if s == nil || s.proc == nil || !s.alive {
			continue
		}
		name, ok := s.proc.ForegroundName()
		if !ok || name == s.foreground {
			continue
		}
		s.foreground = name
		if t.adoptAgent(s) {
			changed = true
		}
	}
	if changed {
		t.ctx.Invalidate()
	} else {
		t.ctx.FrameDue()
	}
}

// watchForegrounds polls local panes without blocking the UI.
func (t *Plugin) watchForegrounds(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.ctx.PostQuiet(t.scanForegrounds)
		}
	}
}

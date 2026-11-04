package status

import "strings"

// Stream reads a program's output as it goes to its terminal emulator, for
// what the emulator does not report: the program's OSC 7501 records, its
// queries of them, whether it asked for bracketed paste, and the targets of
// the hyperlinks it printed (OSC 8). A sequence may end in one Write and
// continue in the next.
//
// The scanner follows the VT500 state machine only as far as finding these:
// an ESC inside a string ends it, as in xterm and Ghostty, and CAN or SUB
// cancels a sequence, so a program that never terminates a string cannot hide
// the reports that follow it. A Stream is not safe for concurrent use.
type Stream struct {
	state scanState
	// csi is the private marker and the parameters of the CSI being read.
	csiPrivate byte
	csiParams  []byte
	csiOther   bool // an intermediate byte: not a mode this reads
	// osc is the payload of the OSC being read, up to oscLimit bytes.
	osc     []byte
	oscLong bool

	bracketed bool
	reported  bool
	root      *Status
	records   map[string]Status
	pending   []Status
	replies   []string
	links     []string
}

// maxLinks is how many hyperlink targets a Stream holds between takes.
const maxLinks = 256

type scanState uint8

const (
	scanGround scanState = iota
	scanEscape
	scanCSI
	scanOSC
	scanOSCEscape
	scanString // DCS, SOS, PM, APC: skipped until ST, BEL or the next ESC
)

// oscLimit is the longest OSC payload kept: a report and its "7501;".
const oscLimit = statusMaxBody + len("7501;")

// Write scans what the program wrote.
func (s *Stream) Write(p []byte) {
	for _, b := range p {
		s.scan(b)
	}
}

func (s *Stream) scan(b byte) {
	if b == 0x18 || b == 0x1a { // CAN, SUB: whatever was open is dropped
		s.state = scanGround
		return
	}
	switch s.state {
	case scanGround:
		// Output is UTF-8, as the emulator reads it: a byte from 0x80 to
		// 0x9f is part of a character, never an 8-bit control.
		if b == 0x1b {
			s.state = scanEscape
		}
	case scanEscape:
		switch b {
		case '[':
			s.startCSI()
		case ']':
			s.startOSC()
		case 'P', 'X', '^', '_':
			s.state = scanString
		case 'c': // RIS: a full reset takes the modes back to their defaults
			s.bracketed = false
			s.state = scanGround
		case 0x1b:
		default:
			s.state = scanGround
		}
	case scanCSI:
		switch {
		case b == 0x1b:
			s.state = scanEscape
		case b >= 0x40 && b <= 0x7e:
			s.dispatchCSI(b)
			s.state = scanGround
		case b >= 0x20 && b <= 0x2f:
			s.csiOther = true
		case b >= 0x30 && b <= 0x3f:
			if len(s.csiParams) == 0 && s.csiPrivate == 0 && (b == '?' || b == '<' || b == '=' || b == '>') {
				s.csiPrivate = b
			} else if len(s.csiParams) < 64 {
				s.csiParams = append(s.csiParams, b)
			}
		}
	case scanOSC:
		switch b {
		case 0x07:
			s.finishOSC()
			s.state = scanGround
		case 0x1b:
			s.state = scanOSCEscape
		default:
			if len(s.osc) < oscLimit {
				s.osc = append(s.osc, b)
			} else {
				s.oscLong = true
			}
		}
	case scanOSCEscape:
		// ST ends the OSC; any other escape ends it too and starts the next
		// sequence.
		s.finishOSC()
		s.state = scanEscape
		if b == '\\' {
			s.state = scanGround
		} else {
			s.scan(b)
		}
	case scanString:
		switch b {
		case 0x07:
			s.state = scanGround
		case 0x1b:
			s.state = scanEscape
		}
	}
}

func (s *Stream) startCSI() {
	s.csiPrivate, s.csiParams, s.csiOther = 0, s.csiParams[:0], false
	s.state = scanCSI
}

func (s *Stream) startOSC() {
	s.osc, s.oscLong = s.osc[:0], false
	s.state = scanOSC
}

// dispatchCSI notes bracketed paste (DECSET 2004) and the soft reset that
// clears it.
func (s *Stream) dispatchCSI(final byte) {
	if s.csiOther {
		if final == 'p' && s.csiPrivate == 0 && string(s.csiParams) == "" {
			s.bracketed = false // DECSTR
		}
		return
	}
	if s.csiPrivate != '?' || (final != 'h' && final != 'l') {
		return
	}
	for _, param := range strings.Split(string(s.csiParams), ";") {
		if param == "2004" {
			s.bracketed = final == 'h'
		}
	}
}

// finishOSC applies an OSC 7501 report, answers a query of them, or keeps
// the target of a hyperlink.
func (s *Stream) finishOSC() {
	if s.oscLong {
		return
	}
	if link, ok := strings.CutPrefix(string(s.osc), "8;"); ok {
		// OSC 8 ; params ; URI starts a link, and an empty URI ends it.
		if _, uri, ok := strings.Cut(link, ";"); ok && uri != "" && len(s.links) < maxLinks {
			s.links = append(s.links, uri)
		}
		return
	}
	body, ok := strings.CutPrefix(string(s.osc), "7501;")
	if !ok {
		return
	}
	if strings.TrimSpace(body) == "?" {
		s.replies = append(s.replies, QueryReply)
		return
	}
	if st, ok := parseStatus(body); ok {
		s.apply(st)
	}
}

// apply stores one report, replacing its record, and queues it for whoever
// moves state on reports (TakeReports).
func (s *Stream) apply(st Status) {
	s.reported = true
	s.pending = append(s.pending, st)
	switch {
	case st.Clear() && st.ID == "":
		s.root = nil
		s.records = nil
	case st.Clear():
		if s.root != nil && s.root.ID == st.ID {
			s.root = nil
		}
		delete(s.records, st.ID)
		child := st.ID + "/"
		for id := range s.records {
			if strings.HasPrefix(id, child) {
				delete(s.records, id)
			}
		}
	case st.ID == "":
		root := st
		s.root = &root
	default:
		if s.records == nil {
			s.records = map[string]Status{}
		}
		s.records[st.ID] = st
	}
}

// Status is the program's own record, if it reported one.
func (s *Stream) Status() (Status, bool) {
	if s.root == nil {
		return Status{}, false
	}
	return *s.root, true
}

// Statuses is a copy of the records the program reported for parts of its
// work, by id. The program's own record is Status, not an entry here.
func (s *Stream) Statuses() map[string]Status {
	if len(s.records) == 0 {
		return nil
	}
	out := make(map[string]Status, len(s.records))
	for id, st := range s.records {
		out[id] = st
	}
	return out
}

// Reported reports whether the program ever sent a status report. A program
// that has not is not an agent to the window, however busy its screen looks.
func (s *Stream) Reported() bool { return s.reported }

// Bracketed reports whether the program asked for bracketed paste, so text
// with line breaks reaches it as one paste rather than as lines it runs.
func (s *Stream) Bracketed() bool { return s.bracketed }

// TakeReports returns the reports received since the last call, in order,
// and forgets them; the records stay.
func (s *Stream) TakeReports() []Status {
	reports := s.pending
	s.pending = nil
	return reports
}

// TakeLinks returns the targets of the hyperlinks printed since the last
// call, in order, and forgets them.
func (s *Stream) TakeLinks() []string {
	links := s.links
	s.links = nil
	return links
}

// TakeReplies returns what the program's queries should be answered with,
// and forgets them.
func (s *Stream) TakeReplies() []string {
	replies := s.replies
	s.replies = nil
	return replies
}

// AppendLinks adds hyperlink targets to a list, each once and the newest
// last, keeping at most max of them.
func AppendLinks(links, more []string, max int) []string {
	for _, link := range more {
		for i, old := range links {
			if old == link {
				links = append(links[:i], links[i+1:]...)
				break
			}
		}
		links = append(links, link)
	}
	if len(links) > max {
		links = append([]string(nil), links[len(links)-max:]...)
	}
	return links
}

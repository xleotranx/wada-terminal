package status

import (
	"encoding/base64"
	"strings"
	"testing"
)

// report writes one OSC 7501 sequence to a stream.
func report(t *testing.T, s *Stream, body string) {
	t.Helper()
	s.Write([]byte("\x1b]7501;" + body + "\x1b\\"))
}

// TestStatusReportMinimal checks the one required key and the defaults of the
// others, as the protocol defines them.
func TestStatusReportMinimal(t *testing.T) {
	s := &Stream{}
	report(t, s, "state=idle")
	if !s.Reported() {
		t.Fatal("a program that reported is not marked as reported")
	}
	st, ok := s.Status()
	if !ok {
		t.Fatal("no status record")
	}
	if st.State != "idle" || st.Progress != -1 || st.Kind != "" || st.ID != "" ||
		st.App != "" || st.Title != "" || st.Message != "" {
		t.Fatalf("status = %+v, want a minimal idle record", st)
	}
	reports := s.TakeReports()
	if len(reports) != 1 || reports[0].State != "idle" {
		t.Fatalf("reports = %+v, want the idle report", reports)
	}
	if again := s.TakeReports(); len(again) != 0 {
		t.Fatalf("the reports were not taken: %+v", again)
	}
}

// TestStatusReportAllKeys checks every field, including the base64 text the
// specification gives for people.
func TestStatusReportAllKeys(t *testing.T) {
	s := &Stream{}
	report(t, s, "state=blocked:kind=permission:progress=42:id=tf/plan:app=terraform:"+
		"title=UGxhbg==:msg=QXBwbHkgMyB0byBhZGQsIDEgdG8gY2hhbmdlLCAwIHRvIGRlc3Ryb3k/")
	st, ok := s.Statuses()["tf/plan"]
	if !ok {
		t.Fatalf("the child record is not kept: %+v", s.Statuses())
	}
	if st.State != "blocked" || st.Kind != "permission" || st.Progress != 42 {
		t.Fatalf("state/kind/progress = %+v", st)
	}
	if st.ID != "tf/plan" || st.App != "terraform" {
		t.Fatalf("id/app = %q/%q", st.ID, st.App)
	}
	if st.Title != "Plan" {
		t.Errorf("title = %q, want Plan", st.Title)
	}
	if st.Message != "Apply 3 to add, 1 to change, 0 to destroy?" {
		t.Errorf("message = %q", st.Message)
	}
	if _, ok := s.Status(); ok {
		t.Errorf("a child record became the program's own record")
	}
}

// TestStatusReportQuery checks that a program can ask whether the terminal
// speaks the protocol, and that asking is not reporting.
func TestStatusReportQuery(t *testing.T) {
	s := &Stream{}
	s.Write([]byte("\x1b]7501;?\x1b\\"))
	replies := s.TakeReplies()
	if len(replies) != 1 {
		t.Fatalf("replies = %q, want one", replies)
	}
	want := "?:states=idle,working,done,blocked,error:kinds=permission,question,auth"
	if !strings.Contains(replies[0], want) {
		t.Fatalf("reply = %q, want %q", replies[0], want)
	}
	if s.Reported() {
		t.Fatal("a query marked the program as reporting")
	}
}

// TestStatusReportForgiving checks the rules: malformed pairs and unknown keys
// are skipped, the last value wins, and whitespace around keys and values is
// allowed.
func TestStatusReportForgiving(t *testing.T) {
	s := &Stream{}
	report(t, s, "garbage:=x:Bad=1:app=b@d:future=yes: state = working :\tapp=cargo\t:::")
	st, ok := s.Status()
	if !ok {
		t.Fatal("a report with a few bad pairs was discarded")
	}
	if st.State != "working" || st.App != "cargo" {
		t.Fatalf("status = %+v, want working/cargo", st)
	}

	report(t, s, "state=idle:app=a:state=done:app=b")
	st, _ = s.Status()
	if st.State != "done" || st.App != "b" {
		t.Fatalf("last value did not win: %+v", st)
	}
}

// TestStatusReportRejects checks what discards a report whole: an unknown
// state, a bad id, too much text, bad base64, or a state missing.
func TestStatusReportRejects(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no state", "app=cargo"},
		{"unknown state", "state=bogus"},
		{"bad id", "state=working:id=deploy//east"},
		{"bad id byte", "state=working:id=deploy/e@st"},
		{"id too long", "state=working:id=" + strings.Repeat("a", 129)},
		{"id segment too long", "state=working:id=" + strings.Repeat("a", 33)},
		{"id too deep", "state=working:id=a/b/c/d/e/f/g/h/i"},
		{"title too long", "state=idle:title=" + strings.Repeat("A", 257)},
		{"msg too long after decoding", "state=idle:msg=" + base64Of(strings.Repeat("x", 2049))},
		{"bad base64", "state=idle:msg=A"},
		{"control in text", "state=idle:title=" + base64Of("a\x01b")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Stream{}
			report(t, s, tc.body)
			if _, ok := s.Status(); ok {
				t.Fatalf("the report %q was accepted", tc.body)
			}
			if s.Reported() {
				t.Fatal("a discarded report marked the program as reporting")
			}
		})
	}
}

// TestStatusReportKindAndProgress checks that kind only applies to blocked and
// progress only to working or blocked.
func TestStatusReportKindAndProgress(t *testing.T) {
	cases := []struct {
		body     string
		wantKind string
		wantProg int
	}{
		{"state=working:kind=auth:progress=10", "", 10},
		{"state=done:kind=auth:progress=10", "", -1},
		{"state=blocked:kind=dance", "", -1},
		{"state=blocked:kind=question", "question", -1},
		{"state=working:progress=101", "", -1},
		{"state=working:progress=-1", "", -1},
		{"state=working:progress=100", "", 100},
	}
	for _, tc := range cases {
		s := &Stream{}
		report(t, s, tc.body)
		st, ok := s.Status()
		if !ok {
			t.Fatalf("%q was discarded", tc.body)
		}
		if st.Kind != tc.wantKind || st.Progress != tc.wantProg {
			t.Errorf("%q: kind/progress = %q/%d, want %q/%d", tc.body, st.Kind, st.Progress, tc.wantKind, tc.wantProg)
		}
	}
}

// TestStatusReportClear checks that clear removes the addressed record and its
// children, and that a clear without an id removes every record.
func TestStatusReportClear(t *testing.T) {
	s := &Stream{}
	report(t, s, "state=working")
	report(t, s, "state=working:id=deploy/us-east:app=aws")
	report(t, s, "state=working:id=deploy/eu-west:app=aws")
	report(t, s, "state=idle:id=other")

	report(t, s, "state=clear:id=deploy")
	if _, ok := s.Status(); !ok {
		t.Error("clearing a child record removed the root")
	}
	statuses := s.Statuses()
	if len(statuses) != 1 || statuses["other"].State != "idle" {
		t.Fatalf("after clearing deploy, records = %+v", statuses)
	}

	report(t, s, "state=clear")
	if _, ok := s.Status(); ok {
		t.Error("clear without an id kept the root")
	}
	if len(s.Statuses()) != 0 {
		t.Errorf("clear without an id kept children: %+v", s.Statuses())
	}
	if !s.Reported() {
		t.Error("clearing forgot that the program ever reported")
	}
}

// TestStatusReportSplitAcrossWrites checks that the parser keeps an OSC 7501
// sequence split between two reads.
func TestStatusReportSplitAcrossWrites(t *testing.T) {
	s := &Stream{}
	s.Write([]byte("\x1b]7501;state=wor"))
	s.Write([]byte("king:app=cargo\x1b\\"))
	st, ok := s.Status()
	if !ok || st.State != "working" || st.App != "cargo" {
		t.Fatalf("split report = %+v, ok=%v", st, ok)
	}
}

// TestStatusReportAfterUnterminatedStrings checks that a report still
// arrives after a program's strings that a terminal ends early: pi marks its
// cursor with an APC ended by BEL, and an ESC ends any string or OSC.
func TestStatusReportAfterUnterminatedStrings(t *testing.T) {
	s := &Stream{}
	s.Write([]byte("> typed\x1b_pi:c\x07 more"))
	s.Write([]byte("\x1bPnever ended\x1b[0m"))
	s.Write([]byte("\x1b]0;title with no end\x1b]7501;state=working\x1b\\"))
	if st, ok := s.Status(); !ok || st.State != "working" {
		t.Fatalf("status = %+v, ok=%v; want working", st, ok)
	}
}

// TestUTF8IsNotAControl checks that the continuation bytes of a character
// (0x9b and 0x9d are two) start no sequence.
func TestUTF8IsNotAControl(t *testing.T) {
	s := &Stream{}
	s.Write([]byte("\xe2\x80\x9b\xe2\x80\x9d 7501;state=working\x07"))
	if s.Reported() {
		t.Fatal("text with UTF-8 continuation bytes was read as a report")
	}
}

// TestHyperlinkTargets checks that the targets of OSC 8 links are kept, and
// the empty one that ends a link is not.
func TestHyperlinkTargets(t *testing.T) {
	s := &Stream{}
	s.Write([]byte("\x1b]8;;https://one.example/a\x07One\x1b]8;;\x07 \x1b]8;id=2;https://two"))
	s.Write([]byte(".example/b\x1b\\Two\x1b]8;;\x1b\\"))
	links := s.TakeLinks()
	if len(links) != 2 || links[0] != "https://one.example/a" || links[1] != "https://two.example/b" {
		t.Fatalf("links = %q", links)
	}
	if again := s.TakeLinks(); len(again) != 0 {
		t.Fatalf("the links were not taken: %q", again)
	}
}

// TestBracketedPaste checks DECSET 2004 among other modes, and the resets
// that clear it.
func TestBracketedPaste(t *testing.T) {
	s := &Stream{}
	if s.Bracketed() {
		t.Fatal("bracketed paste is on before a program asked")
	}
	s.Write([]byte("\x1b[?1049;20"))
	s.Write([]byte("04h"))
	if !s.Bracketed() {
		t.Fatal("DECSET 1049;2004 did not turn bracketed paste on")
	}
	s.Write([]byte("\x1b[?2004l"))
	if s.Bracketed() {
		t.Fatal("DECRST 2004 did not turn it off")
	}
	s.Write([]byte("\x1b[?2004h\x1b[!p"))
	if s.Bracketed() {
		t.Fatal("a soft reset did not turn it off")
	}
	s.Write([]byte("\x1b[?2004h\x1bc"))
	if s.Bracketed() {
		t.Fatal("a full reset did not turn it off")
	}
	s.Write([]byte("\x1b[2004h"))
	if s.Bracketed() {
		t.Fatal("ANSI mode 2004 is not bracketed paste")
	}
}

// base64Of encodes text the way the protocol carries title and msg.
func base64Of(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }

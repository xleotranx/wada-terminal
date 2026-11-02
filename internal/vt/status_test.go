package vt

import (
	"encoding/base64"
	"strings"
	"testing"
)

// report writes one OSC 7501 sequence to a screen.
func report(t *testing.T, s *Screen, body string) {
	t.Helper()
	s.Write([]byte("\x1b]7501;" + body + "\x1b\\"))
}

// TestStatusReportMinimal checks the one required key and the defaults of the
// others, as the protocol defines them.
func TestStatusReportMinimal(t *testing.T) {
	s := New(40, 8)
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
	reports := s.TakeStatusReports()
	if len(reports) != 1 || reports[0].State != "idle" {
		t.Fatalf("reports = %+v, want the idle report", reports)
	}
	if again := s.TakeStatusReports(); len(again) != 0 {
		t.Fatalf("the reports were not taken: %+v", again)
	}
}

// TestStatusReportAllKeys checks every field, including the base64 text the
// specification gives for people.
func TestStatusReportAllKeys(t *testing.T) {
	s := New(40, 8)
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
	s := New(40, 8)
	s.Write([]byte("\x1b]7501;?\x1b\\"))
	replies := s.TakeResponses()
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
	s := New(40, 8)
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
			s := New(40, 8)
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
		s := New(40, 8)
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
	s := New(40, 8)
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
	s := New(40, 8)
	s.Write([]byte("\x1b]7501;state=wor"))
	s.Write([]byte("king:app=cargo\x1b\\"))
	st, ok := s.Status()
	if !ok || st.State != "working" || st.App != "cargo" {
		t.Fatalf("split report = %+v, ok=%v", st, ok)
	}
}

// TestStatusReportDoesNotPaint checks that a report is not text on the screen.
func TestStatusReportDoesNotPaint(t *testing.T) {
	s := New(20, 4)
	report(t, s, "state=working:app=cargo")
	for y := 0; y < 4; y++ {
		for _, cell := range s.Row(y) {
			if cell.Text != "" {
				t.Fatalf("the report painted %q at row %d", cell.Text, y)
			}
		}
	}
}

// base64Of encodes text the way the protocol carries title and msg.
func base64Of(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }

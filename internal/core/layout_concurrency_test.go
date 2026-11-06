package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentLayoutSaves(t *testing.T) {
	s := &Server{dir: t.TempDir()}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				if r := s.dispatch(Request{Method: "layout.set", Layout: json.RawMessage(`{"spaces":[]}`)}); r.Error != "" {
					t.Error(r.Error)
				}
				s.dispatch(Request{Method: "layout.get"})
			}
		}()
	}
	wg.Wait()
	body, err := os.ReadFile(filepath.Join(s.dir, "layout.json"))
	if err != nil || !json.Valid(body) {
		t.Fatalf("invalid saved layout: %s %v", body, err)
	}
}

func TestFailedLayoutSaveKeepsLastGoodDocument(t *testing.T) {
	dir := t.TempDir()
	s := &Server{dir: dir, layout: json.RawMessage(`{"spaces":["old"]}`), layoutRev: 7}
	if err := os.Mkdir(filepath.Join(dir, "layout.json.tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	reply := s.dispatch(Request{Method: "layout.set", Layout: json.RawMessage(`{"spaces":["new"]}`)})
	if reply.Error == "" {
		t.Fatal("expected failed disk write")
	}
	if string(s.layout) != `{"spaces":["old"]}` || s.layoutRev != 7 {
		t.Fatal("failed persistence published a new document")
	}
}

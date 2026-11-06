package terminal

import (
	"testing"
	"wada-terminal/internal/config"
)

func TestLayoutQueriesFollowVisibleStack(t *testing.T) {
	p := &Plugin{}
	root := &config.Block{Type: "stack", Active: "shown", Children: []config.Block{{ID: "shown", Type: "mod"}, {ID: "other", Type: "column", Children: []config.Block{{Type: "tabs"}, {Type: "title"}, {Type: "float", ID: "hidden"}, {Type: "machines"}}}}}
	if p.hasTabsBlock(root) || p.hasTitleBlock(root) || p.hasMachinesBlock(root) || len(p.floatingBlocks(root)) != 0 {
		t.Fatal("inactive children affected visible layout")
	}
	if p.hasTitleBlock(&config.Block{Type: "title", Hidden: true}) {
		t.Fatal("hidden title affected chrome")
	}
}

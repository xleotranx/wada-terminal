package terminal

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestDragBy checks that a drag moves the divider by the pointer's offset and
// stops before either pane closes.
func TestDragBy(t *testing.T) {
	b := &branch{vertical: true, ratio: 0.5, usable: 100}
	b.dragBy(10, 0, true)
	if want := float32(0.6); b.ratio != want {
		t.Fatalf("ratio = %v, want %v", b.ratio, want)
	}
	b.dragBy(-30, 0, true)
	if want := float32(0.3); b.ratio != want {
		t.Fatalf("ratio = %v, want %v", b.ratio, want)
	}
	b.dragBy(-1000, 0, true)
	if b.ratio != 0.12 {
		t.Fatalf("ratio = %v, want the 0.12 limit", b.ratio)
	}
	b.dragBy(1000, 0, true)
	if b.ratio != 0.88 {
		t.Fatalf("ratio = %v, want the 0.88 limit", b.ratio)
	}
	// A horizontal split reads the pointer's vertical offset.
	h := &branch{ratio: 0.5, usable: 100}
	h.dragBy(100, 20, false)
	if want := float32(0.7); h.ratio != want {
		t.Fatalf("horizontal ratio = %v, want %v", h.ratio, want)
	}
	// A split that was never laid out takes no drag.
	z := &branch{ratio: 0.5}
	z.dragBy(50, 0, true)
	if z.ratio != 0.5 {
		t.Fatalf("unmeasured split moved to %v", z.ratio)
	}
}

// TestDragDivider checks the pointer sequence a drag of a divider sends: the
// press takes the pointer, the moves move the split, and the release lets go.
func TestDragDivider(t *testing.T) {
	p := &Plugin{}
	b := &branch{vertical: true, ratio: 0.5, usable: 100}
	if !p.dragDivider(b, ui.InputEvent{Kind: ui.InputPointerDown}, true) {
		t.Fatal("the press was not taken")
	}
	if p.drag != b {
		t.Fatal("the split is not the one dragged")
	}
	p.dragDivider(b, ui.InputEvent{Kind: ui.InputPointerMove, X: 10}, true)
	if want := float32(0.6); b.ratio != want {
		t.Fatalf("ratio = %v, want %v", b.ratio, want)
	}
	if !p.dragDivider(b, ui.InputEvent{Kind: ui.InputPointerUp}, true) {
		t.Fatal("the release was not taken")
	}
	if p.drag != nil {
		t.Fatal("the drag outlived its release")
	}
	// Moves after the release do nothing.
	p.dragDivider(b, ui.InputEvent{Kind: ui.InputPointerMove, X: 40}, true)
	if b.ratio != 0.6 {
		t.Fatalf("a move after the release changed the ratio to %v", b.ratio)
	}
}

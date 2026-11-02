package terminal

// node is a leaf of a tab's layout or a split of it.
type node struct {
	leaf  *session
	split *branch
}

// branch splits the room of a node in two, side by side when vertical is set
// and one above the other otherwise, with ratio of the room for a.
type branch struct {
	vertical bool
	ratio    float32
	a, b     *node
	// usable is the room the two panes share along the split, in DIPs, as
	// the last frame laid it out; a drag divides by it.
	usable float32
}

// leafOf makes a layout of one session.
func leafOf(s *session) *node { return &node{leaf: s} }

// sessions appends the sessions of the layout in the order they are drawn.
func (n *node) sessions(out []*session) []*session {
	if n == nil {
		return out
	}
	if n.leaf != nil {
		return append(out, n.leaf)
	}
	out = n.split.a.sessions(out)
	return n.split.b.sessions(out)
}

// contains reports whether the layout shows s.
func (n *node) contains(s *session) bool {
	if n == nil {
		return false
	}
	if n.leaf != nil {
		return n.leaf == s
	}
	return n.split.a.contains(s) || n.split.b.contains(s)
}

// splitAt replaces the leaf of s with a split of s and next. It returns the
// layout to put in the leaf's place and whether it found s.
func splitAt(n *node, s, next *session, vertical bool) (*node, bool) {
	if n == nil {
		return n, false
	}
	if n.leaf != nil {
		if n.leaf != s {
			return n, false
		}
		return &node{split: &branch{vertical: vertical, ratio: 0.5, a: leafOf(s), b: leafOf(next)}}, true
	}
	if a, ok := splitAt(n.split.a, s, next, vertical); ok {
		n.split.a = a
		return n, true
	}
	if b, ok := splitAt(n.split.b, s, next, vertical); ok {
		n.split.b = b
		return n, true
	}
	return n, false
}

// removeAt takes the leaf of s out of the layout, joining what is left of the
// split it was in. It returns nil when the layout held nothing else.
func removeAt(n *node, s *session) *node {
	if n == nil {
		return nil
	}
	if n.leaf != nil {
		if n.leaf == s {
			return nil
		}
		return n
	}
	a := removeAt(n.split.a, s)
	if a == nil {
		return n.split.b
	}
	n.split.a = a
	b := removeAt(n.split.b, s)
	if b == nil {
		return n.split.a
	}
	n.split.b = b
	return n
}

// replaceAt puts next where s is in the layout.
func replaceAt(n *node, s, next *session) *node {
	if n == nil {
		return n
	}
	if n.leaf != nil {
		if n.leaf == s {
			return leafOf(next)
		}
		return n
	}
	n.split.a = replaceAt(n.split.a, s, next)
	n.split.b = replaceAt(n.split.b, s, next)
	return n
}

// dragBy moves the divider by what the pointer is into it, as a drag does,
// and keeps the split from closing on either side.
func (b *branch) dragBy(evX, evY float32, vertical bool) {
	if b.usable <= 0 {
		return
	}
	d := evY
	if vertical {
		d = evX
	}
	b.ratio = min(max(b.ratio+d/b.usable, 0.12), 0.88)
}

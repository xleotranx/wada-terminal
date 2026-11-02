package terminal

import (
	"fmt"
	"strconv"

	"wada-terminal/internal/config"

	"github.com/egoist/mygo/ui"
)

// layoutAxis is the direction a block is placed in: the root, a row, or a
// column. Size.Px is measured along it.
type layoutAxis uint8

const (
	axisNone layoutAxis = iota
	axisRow
	axisColumn
)

// blockPosition is where a block sits among its visible siblings, which picks
// the divider it draws.
type blockPosition uint8

const (
	posOnly blockPosition = iota
	posFirst
	posMiddle
	posLast
)

// layoutTree is the tree the page draws. Without a config file it is the old
// sidebar shape: the workspace tree and the panes.
func (t *Plugin) layoutTree() *config.Block {
	if t.windowLayout != nil && t.windowLayout.Root != nil {
		return t.windowLayout.Root
	}
	width := t.sidebarPx
	if width < 180 || width > 360 {
		width = defaultSidebarWidth
	}
	workspaces := config.Block{Type: "workspaces", Size: &config.Size{Px: width}}
	panes := config.Block{Type: "panes", Size: &config.Size{Weight: 1}}
	children := []config.Block{workspaces, panes}
	if t.sidebarAt == "right" {
		children = []config.Block{panes, workspaces}
	}
	return &config.Block{Type: "row", Children: children}
}

// stackChild is the child a stack shows: the runtime choice, then the file's
// active child, then the first visible one.
func (t *Plugin) stackChild(block *config.Block) (*config.Block, bool) {
	if block == nil || block.Type != "stack" {
		return nil, false
	}
	active := block.Active
	if t.layoutStacks != nil {
		if chosen := t.layoutStacks.Get()[block.ID]; chosen != "" {
			active = chosen
		}
	}
	for i := range block.Children {
		child := &block.Children[i]
		if child.ID == active && !child.Hidden {
			return child, true
		}
	}
	for i := range block.Children {
		child := &block.Children[i]
		if !child.Hidden {
			return child, true
		}
	}
	return nil, false
}

// floatingBlocks collects every visible float in the layout. A float may sit
// anywhere in the tree; it is drawn over the page, not in the row or column.
func (t *Plugin) floatingBlocks(root *config.Block) []*config.Block {
	var out []*config.Block
	var walk func(*config.Block)
	walk = func(block *config.Block) {
		if block == nil || block.Hidden {
			return
		}
		if block.Type == "float" {
			out = append(out, block)
			return
		}
		for i := range block.Children {
			walk(&block.Children[i])
		}
	}
	walk(root)
	return out
}

// floatElement draws a floating mod panel anchored to an edge of the page.
// It takes no room from the layout and can be dragged; its offset is saved.
func (t *Plugin) floatElement(c *ui.Context, ch chrome, block *config.Block) {
	width, height := block.Width, block.Height
	if width < 120 {
		width = 320
	}
	if height < 80 {
		height = 260
	}
	inset := block.Offset
	if inset == 0 {
		inset = 12
	}
	edge, align := block.Edge, block.Align
	if edge == "" {
		edge = "right"
	}
	if align == "" {
		align = "center"
	}
	peek := block.Peek
	collapsed := peek > 0 && (edge == "left" || edge == "right") && !t.floatHover[block.ID]
	if collapsed {
		width = min(max(peek, 32), 120)
	}
	frame := ui.Box(c).Absolute().Width(float32(width)).Height(float32(height)).
		Background(ch.pal.PanelBg).Border(1, ch.pal.Hairline).Radius(10)
	dragOffset := t.floatOffset(block)
	options := ui.DraggableOptions{}
	if block.Snap && t.pageW > 0 && t.pageH > 0 {
		baseX, baseY := t.floatBase(edge, align, float32(width), float32(height), float32(inset))
		left := insetOffset(float32(inset), baseX)
		right := insetOffset(t.pageW-float32(width)-float32(inset), baseX)
		top := insetOffset(float32(inset), baseY)
		bottom := insetOffset(t.pageH-float32(height)-float32(inset), baseY)
		options.Snap = ui.Nearest(
			ui.Point{X: left, Y: dragOffset.Y},
			ui.Point{X: right, Y: dragOffset.Y},
			ui.Point{X: dragOffset.X, Y: top},
			ui.Point{X: dragOffset.X, Y: bottom},
		)
	}
	frame = frame.Draggable(dragOffset, options)
	switch edge {
	case "left":
		frame = frame.Left(float32(inset))
	case "top":
		frame = frame.Top(float32(inset))
	case "bottom":
		frame = frame.Bottom(float32(inset))
	default:
		frame = frame.Right(float32(inset))
	}
	if edge == "left" || edge == "right" {
		switch align {
		case "start":
			frame = frame.Top(float32(inset))
		case "end":
			frame = frame.Bottom(float32(inset))
		default:
			frame = frame.TopPercent(50).Translate(0, -float32(height)/2)
		}
	} else {
		switch align {
		case "start":
			frame = frame.Left(float32(inset))
		case "end":
			frame = frame.Right(float32(inset))
		default:
			frame = frame.LeftPercent(50).Translate(-float32(width)/2, 0)
		}
	}
	if collapsed {
		frame = frame.Clip()
		frame.Children(func() {
			ui.Column(c).Fill().Center().Padding(2).Children(func() {
				ui.Text(c, "⋮ "+block.ID).FontSize(9).TextColor(ch.pal.Muted).
					SingleLine().Ellipsis("…")
			})
		})
	} else {
		frame.Children(func() {
			t.modBlock(c, ch, block)
		})
	}
	if peek > 0 {
		hovered := frame.Hovered()
		if hovered != t.floatHover[block.ID] {
			t.floatHover[block.ID] = hovered
			if t.ctx != nil {
				t.ctx.Post(func() { t.ctx.Invalidate() })
			}
		}
	}
	if _, _, ended := frame.DragEnded(); ended && t.ctx != nil {
		x, y := dragOffset.X, dragOffset.Y
		t.ctx.Post(func() { t.saveFloatOffset(block.ID, x, y) })
	}
}

// floatBase is where the layout would put a float with no drag offset.
func (t *Plugin) floatBase(edge, align string, width, height, inset float32) (x, y float32) {
	switch edge {
	case "left":
		x = inset
	case "top", "bottom":
		switch align {
		case "start":
			x = inset
		case "end":
			x = t.pageW - width - inset
		default:
			x = (t.pageW - width) / 2
		}
	default:
		x = t.pageW - width - inset
	}
	switch edge {
	case "top":
		y = inset
	case "bottom":
		y = t.pageH - height - inset
	default:
		switch align {
		case "start":
			y = inset
		case "end":
			y = t.pageH - height - inset
		default:
			y = (t.pageH - height) / 2
		}
	}
	return x, y
}

// insetOffset is the drag offset that puts an element at target.
func insetOffset(target, base float32) float32 { return target - base }

// floatOffset is the offset a drag has given a float, or the saved one. The
// same pointer is kept across frames so Draggable can update it.
func (t *Plugin) floatOffset(block *config.Block) *ui.Point {
	if t.floatDrag == nil {
		t.floatDrag = map[string]*ui.Point{}
	}
	if offset := t.floatDrag[block.ID]; offset != nil {
		return offset
	}
	offset := &ui.Point{}
	if t.floatOffsets != nil {
		if saved, ok := t.floatOffsets.Get()[block.ID]; ok {
			offset.X, offset.Y = saved[0], saved[1]
		}
	}
	t.floatDrag[block.ID] = offset
	return offset
}

// saveFloatOffset records where a drag let a float rest.
func (t *Plugin) saveFloatOffset(id string, x, y float32) {
	if t.floatOffsets == nil {
		return
	}
	offsets := t.floatOffsets.Get()
	if offsets == nil {
		offsets = map[string][2]float32{}
	}
	offsets[id] = [2]float32{x, y}
	_ = t.floatOffsets.Set(offsets)
}

// resetFloat puts a float back at its layout anchor.
func (t *Plugin) resetFloat(id string) error {
	found := false
	for _, block := range t.floatingBlocks(t.layoutTree()) {
		if block.ID == id {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("no float %q in this layout", id)
	}
	// Keep the pointer Draggable holds; setting it to zero is what moves the
	// element back. Replacing it would leave the old gesture state behind.
	if offset := t.floatDrag[id]; offset != nil {
		offset.X, offset.Y = 0, 0
	}
	if t.floatOffsets != nil {
		offsets := t.floatOffsets.Get()
		delete(offsets, id)
		_ = t.floatOffsets.Set(offsets)
	}
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
	return nil
}

// blockHidden reports whether a block is left out of this frame.
func (t *Plugin) blockHidden(block *config.Block) bool {
	if block == nil || block.Hidden {
		return true
	}
	return block.Type == "workspaces" && !t.sidebar.Get()
}

// blockElement builds one layout block and its children. The axis is where the
// block sits, so a pixel size is a width in a row and a height in a column.
func (t *Plugin) blockElement(c *ui.Context, ch chrome, block *config.Block, axis layoutAxis, position blockPosition, path string) ui.Element {
	if t.blockHidden(block) {
		return ui.Box(c)
	}
	switch block.Type {
	case "row", "column", "stack":
		var element ui.Element
		switch block.Type {
		case "row":
			element = ui.Row(c)
		case "column":
			element = ui.Column(c)
		default:
			element = ui.Column(c)
		}
		element = element.MinWidth(0).MinHeight(0)
		element = t.paintBlock(element, ch, block)
		if block.Gap > 0 {
			element = element.Gap(float32(block.Gap))
		}
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		element = t.sizeBlock(element, block, axis, path)
		if block.Type == "stack" {
			child, ok := t.stackChild(block)
			if ok {
				element.Children(func() {
					t.blockElement(c, ch, child, axisNone, posOnly, path+"/stack/"+child.ID)
				})
			}
			return element
		}
		childAxis := axisRow
		if block.Type == "column" {
			childAxis = axisColumn
		}
		element.Children(func() {
			visible := make([]int, 0, len(block.Children))
			for i := range block.Children {
				if !t.blockHidden(&block.Children[i]) {
					visible = append(visible, i)
				}
			}
			for seen, index := range visible {
				child := &block.Children[index]
				childPosition := posOnly
				switch {
				case len(visible) == 1:
					childPosition = posOnly
				case seen == 0:
					childPosition = posFirst
				case seen == len(visible)-1:
					childPosition = posLast
				default:
					childPosition = posMiddle
				}
				childPath := path + "/" + strconv.Itoa(index)
				t.blockElement(c, ch, child, childAxis, childPosition, childPath)
				if seen < len(visible)-1 {
					next := &block.Children[visible[seen+1]]
					nextPath := path + "/" + strconv.Itoa(visible[seen+1])
					t.layoutDivider(c, ch, block.Type, child, childPath, next, nextPath)
				}
			}
		})
		return element

	case "workspaces":
		element := t.workspaceView(c, ch, position)
		element = t.paintBlock(element, ch, block)
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		return t.sizeBlock(element, block, axis, path)

	case "panes":
		element := t.content(c, ch)
		element = t.paintBlock(element, ch, block)
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		return t.sizeBlock(element, block, axis, path)

	case "title":
		element := t.titleActions(c)
		element = t.paintBlock(element, ch, block)
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		return t.sizeBlock(element, block, axis, path)

	case "tabs":
		element := t.strip(c)
		element = t.paintBlock(element, ch, block)
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		return t.sizeBlock(element, block, axis, path)

	case "machines":
		element := t.machinesPanel(c, ch)
		element = t.paintBlock(element, ch, block)
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		return t.sizeBlock(element, block, axis, path)

	case "agents":
		element := t.agentsPanel(c, ch)
		element = t.paintBlock(element, ch, block)
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		return t.sizeBlock(element, block, axis, path)

	case "float":
		// A float has no place in the flow; the page draws it over the root.
		return ui.Box(c)

	case "mod":
		element := t.modBlock(c, ch, block)
		element = t.paintBlock(element, ch, block)
		if block.Padding > 0 {
			element = element.Padding(float32(block.Padding))
		}
		return t.sizeBlock(element, block, axis, path)
	}
	return ui.Box(c)
}

// blockPx is a block's fixed size along its parent's axis, including a size
// the pointer has given it. A weight block has no fixed size.
func (t *Plugin) blockPx(block *config.Block, path string) int {
	if t.layoutSizes != nil {
		if px, ok := t.layoutSizes.Get()[t.blockKey(block, path)]; ok {
			return int(px)
		}
	}
	if block.Size != nil && block.Size.Px > 0 {
		return block.Size.Px
	}
	return 0
}

// blockKey names a block's saved size: its id when it has one, else its place
// in the tree.
func (t *Plugin) blockKey(block *config.Block, path string) string {
	if block.ID != "" {
		return block.ID
	}
	return path
}

// setLayoutSize stores a pointer-resized block and clamps it to the block's
// limits. It runs after the frame, from the divider's post.
func (t *Plugin) setLayoutSize(key string, px float32, block *config.Block) {
	low, high := float32(80), float32(800)
	if block.Min > 0 {
		low = float32(block.Min)
	}
	if block.Max > 0 {
		high = float32(block.Max)
	}
	px = min(max(px, low), high)
	sizes := t.layoutSizes.Get()
	if sizes == nil {
		sizes = map[string]float32{}
	}
	sizes[key] = px
	_ = t.layoutSizes.Set(sizes)
	if t.ctx != nil {
		t.ctx.Invalidate()
	}
}

// layoutDivider draws the draggable line between two blocks. It resizes a
// fixed-size neighbour by the pointer's delta; two fixed neighbours trade
// room, and two flexible neighbours have no divider.
func (t *Plugin) layoutDivider(c *ui.Context, ch chrome, parentType string, left *config.Block, leftPath string, right *config.Block, rightPath string) {
	leftPx := t.blockPx(left, leftPath)
	rightPx := t.blockPx(right, rightPath)
	if leftPx <= 0 && rightPx <= 0 {
		return
	}
	row := parentType == "row"
	divider := ui.Box(c).Shrink(0).Background(ui.Transparent)
	if row {
		divider = divider.Width(6).FillHeight().Cursor(ui.CursorResizeEW)
	} else {
		divider = divider.Height(6).FillWidth().Cursor(ui.CursorResizeNS)
	}
	dx, dy, held := divider.Dragged()
	if divider.Hovered() || held {
		divider = divider.Background(ch.pal.Hairline)
	}
	if !held {
		return
	}
	delta := dx
	if !row {
		delta = dy
	}
	if delta == 0 {
		return
	}
	leftKey, rightKey := t.blockKey(left, leftPath), t.blockKey(right, rightPath)
	if leftPx > 0 {
		t.ctx.Post(func() { t.setLayoutSize(leftKey, float32(leftPx)+delta, left) })
	}
	if rightPx > 0 {
		t.ctx.Post(func() { t.setLayoutSize(rightKey, float32(rightPx)-delta, right) })
	}
}

// paintBlock applies the palette color a block asks for. The chrome never
// takes a raw color from a file; only names that mean palette entries.
func (t *Plugin) paintBlock(element ui.Element, ch chrome, block *config.Block) ui.Element {
	switch block.Background {
	case "drawer":
		return element.Background(ch.pal.DrawerBg)
	case "panel":
		return element.Background(ch.pal.PanelBg)
	case "raised":
		return element.Background(ch.pal.Raised)
	case "transparent":
		return element.Background(ui.Transparent)
	}
	return element
}

// sizeBlock applies a block's size along its parent's direction, with a share
// of the room or fixed pixels, and its min and max.
func (t *Plugin) sizeBlock(element ui.Element, block *config.Block, axis layoutAxis, path string) ui.Element {
	px := t.blockPx(block, path)
	switch axis {
	case axisNone:
		element = element.Fill()
	case axisRow:
		switch {
		case px > 0:
			element = element.Width(float32(px)).Shrink(0)
		case block.Size != nil && block.Size.Weight > 0:
			element = element.Grow(float32(block.Size.Weight))
		default:
			element = element.Grow(1)
		}
		element = element.FillHeight()
		if block.Min > 0 {
			element = element.MinWidth(float32(block.Min))
		}
		if block.Max > 0 {
			element = element.MaxWidth(float32(block.Max))
		}
	case axisColumn:
		switch {
		case px > 0:
			element = element.Height(float32(px)).Shrink(0)
		case block.Size != nil && block.Size.Weight > 0:
			element = element.Grow(float32(block.Size.Weight))
		default:
			element = element.Grow(1)
		}
		element = element.FillWidth()
		if block.Min > 0 {
			element = element.MinHeight(float32(block.Min))
		}
		if block.Max > 0 {
			element = element.MaxHeight(float32(block.Max))
		}
	}
	return element
}

// agentsPanel is the permanent agents list a layout can put beside the panes:
// every session that is an agent, with the state it reports and a click to
// give it the keyboard.
func (t *Plugin) agentsPanel(c *ui.Context, ch chrome) ui.Element {
	pal := ch.pal
	var agents []entry
	for _, e := range t.indexEntries() {
		if e.s.isAgent() {
			agents = append(agents, e)
		}
	}
	panel := ui.Column(c).MinWidth(0).MinHeight(0).Background(pal.DrawerBg)
	panel.Children(func() {
		ui.Row(c).Height(30).FillWidth().Shrink(0).Padding(0, 8).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Text(c, "Agents").FontSize(12).FontWeight(600).TextColor(pal.Text)
			if st, n := t.attention(); n > 0 {
				label := "need you"
				if n == 1 {
					label = "needs you"
				}
				ui.Box(c).Width(6).Height(6).Radius(3).Background(stateColor(ch, st))
				ui.Text(c, strconv.Itoa(n)+" "+label).FontSize(9.5).TextColor(stateColor(ch, st)).
					SingleLine().Ellipsis("…")
			}
		})
		ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 8, 8, 8).Gap(1).Children(func() {
				if len(agents) == 0 {
					ui.Text(c, "No agent has reported yet.").FontSize(10.5).TextColor(pal.Faint).
						Padding(0, 4, 0, 4)
					ui.Text(c, "A program becomes an agent with OSC 7501.").FontSize(9.5).
						TextColor(pal.Faint).Padding(0, 4, 0, 4)
					return
				}
				for _, e := range agents {
					t.agentPanelRow(c, ch, e)
				}
			})
		})
	})
	return panel
}

// agentPanelRow is one agent in the permanent list.
func (t *Plugin) agentPanelRow(c *ui.Context, ch chrome, e entry) {
	pal := ch.pal
	s := e.s
	row := ui.Box(c).Height(40).FillWidth().Shrink(0).Radius(7).Padding(0, 6)
	if row.Hovered() {
		row.Background(pal.Hover)
	}
	row.Cursor(ui.CursorPointer).OnClick(func() { t.focusOn(s) })
	row.Children(func() {
		ui.Row(c).FillHeight().AlignItems(ui.Center).Gap(8).Children(func() {
			t.sessionIcon(c, ch, s, 16)
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				ui.Text(c, s.title()).FontSize(11.5).FontWeight(600).TextColor(pal.Text).
					SingleLine().Ellipsis("…")
				ui.Text(c, s.subtitle()).FontSize(9).TextColor(pal.Faint).
					SingleLine().Ellipsis("…")
			})
			t.stateChip(c, ch, s)
		})
	})
}

// hasTabsBlock reports whether a layout places the tab strip itself.
func (t *Plugin) hasTabsBlock(block *config.Block) bool {
	if block == nil {
		return false
	}
	if block.Type == "tabs" {
		return true
	}
	for i := range block.Children {
		if t.hasTabsBlock(&block.Children[i]) {
			return true
		}
	}
	return false
}

// hasTitleBlock reports whether a layout places the title-bar actions itself.
func (t *Plugin) hasTitleBlock(block *config.Block) bool {
	if block == nil {
		return false
	}
	if block.Type == "title" {
		return true
	}
	for i := range block.Children {
		if t.hasTitleBlock(&block.Children[i]) {
			return true
		}
	}
	return false
}

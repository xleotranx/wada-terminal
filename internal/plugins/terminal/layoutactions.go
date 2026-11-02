package terminal

import (
	"fmt"
	"strconv"

	"wada-terminal/internal/config"
)

// stackByID finds a stack in the layout tree.
func (t *Plugin) stackByID(id string) *config.Block {
	var find func(*config.Block) *config.Block
	find = func(block *config.Block) *config.Block {
		if block == nil {
			return nil
		}
		if block.Type == "stack" && block.ID == id {
			return block
		}
		for i := range block.Children {
			if found := find(&block.Children[i]); found != nil {
				return found
			}
		}
		return nil
	}
	return find(t.layoutTree())
}

// activeStack is the child a stack currently shows.
func (t *Plugin) activeStack(block *config.Block) string {
	if child, ok := t.stackChild(block); ok {
		return child.ID
	}
	return ""
}

// switchStack shows a named child of a stack, or the first one when active is
// empty. It saves the choice so a new window opens on the same tab.
func (t *Plugin) switchStack(id, active string) (string, error) {
	stack := t.stackByID(id)
	if stack == nil {
		return "", fmt.Errorf("no stack %q in this layout", id)
	}
	if active == "" {
		if first, ok := t.stackChild(stack); ok {
			return first.ID, nil
		}
		return "", fmt.Errorf("stack %q has no visible child", id)
	}
	for i := range stack.Children {
		child := &stack.Children[i]
		if child.ID == active {
			if child.Hidden {
				return "", fmt.Errorf("stack child %q is hidden", active)
			}
			chosen := t.layoutStacks.Get()
			if chosen == nil {
				chosen = map[string]string{}
			}
			chosen[id] = active
			_ = t.layoutStacks.Set(chosen)
			if t.ctx != nil {
				t.ctx.Invalidate()
			}
			return active, nil
		}
	}
	return "", fmt.Errorf("stack %q has no child %q", id, active)
}

// cycleStack shows the next or previous visible child of a stack.
func (t *Plugin) cycleStack(id string, delta int) (string, error) {
	stack := t.stackByID(id)
	if stack == nil {
		return "", fmt.Errorf("no stack %q in this layout", id)
	}
	var visible []string
	current := t.activeStack(stack)
	index := -1
	for i := range stack.Children {
		child := &stack.Children[i]
		if child.Hidden {
			continue
		}
		visible = append(visible, child.ID)
		if child.ID == current {
			index = len(visible) - 1
		}
	}
	if len(visible) == 0 {
		return "", fmt.Errorf("stack %q has no visible child", id)
	}
	if index < 0 {
		index = 0
	}
	index = (index + delta + len(visible)) % len(visible)
	return t.switchStack(id, visible[index])
}

// blockByID finds a block by its id and returns the path that names a block
// without one.
func (t *Plugin) blockByID(block *config.Block, id, path string) (*config.Block, string) {
	if block == nil {
		return nil, ""
	}
	if block.ID == id {
		return block, path
	}
	for i := range block.Children {
		childPath := path + "/" + strconv.Itoa(i)
		if found, foundPath := t.blockByID(&block.Children[i], id, childPath); found != nil {
			return found, foundPath
		}
	}
	return nil, ""
}

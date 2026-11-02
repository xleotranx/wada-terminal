// Package mods runs a small JavaScript panel: the script returns a tree of
// rows, columns, text and buttons, and the window draws it. A mod is trusted
// code, like a shell script the person installed; it is not sandboxed.
package mods

import "fmt"

// Node is one element a mod's render returned. The window turns it into a
// native element; a mod never draws directly.
type Node struct {
	Type        string
	ID          string
	Text        string
	Value       string // an input's starting value
	Placeholder string
	Children    []*Node
	Style       Style
	Click       bool // id is sent to the mod's click(id, api)
	Submit      bool // an input calls click(id, api) on Enter
	AutoFocus   bool // an input takes the keyboard when it first draws
}

// Style is what a node asks of the window's layout and palette. Colors are
// palette names, not raw values; the window rejects what it does not know.
type Style struct {
	Padding    float32
	Gap        float32
	Grow       float32
	Width      float32
	Height     float32
	MinWidth   float32
	MaxWidth   float32
	MinHeight  float32
	MaxHeight  float32
	Align      string // start, center, end, stretch
	Justify    string
	Color      string // palette name
	Bg         string // palette name
	FontSize   float32
	Bold       bool
	Italic     bool
	Radius     float32
	Border     string // palette name for a 1px border
	TranslateX float32
	TranslateY float32
	Scale      float32
	Rotate     float32
	Opacity    float32
}

// limits keep a broken render from making the window draw forever.
const (
	maxDepth = 32
	maxNodes = 1000
	maxText  = 4000
)

// parseNode turns the value render returned into a Node. It is deliberately
// strict about types and styles so a typo is an error beside the mod, not a
// blank panel.
func parseNode(value any, depth int, nodes *int) (*Node, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("the tree is deeper than %d", maxDepth)
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("a node is an object, not %T", value)
	}
	*nodes++
	if *nodes > maxNodes {
		return nil, fmt.Errorf("the tree has more than %d nodes", maxNodes)
	}
	kind := stringField(object, "type")
	switch kind {
	case "row", "column", "box", "text", "button", "input", "spacer", "divider", "scroll":
	case "":
		return nil, fmt.Errorf("a node has no type")
	default:
		return nil, fmt.Errorf("no node type %q", kind)
	}
	node := &Node{Type: kind, ID: stringField(object, "id"), Text: stringField(object, "text"),
		Value: stringField(object, "value"), Placeholder: stringField(object, "placeholder")}
	if node.Text == "" {
		node.Text = stringField(object, "label")
	}
	if len(node.Text) > maxText {
		node.Text = node.Text[:maxText]
	}
	if len(node.Value) > maxText {
		node.Value = node.Value[:maxText]
	}
	if len(node.Placeholder) > maxText {
		node.Placeholder = node.Placeholder[:maxText]
	}
	if click, ok := object["on_click"].(bool); ok {
		node.Click = click
	}
	if submit, ok := object["on_submit"].(bool); ok {
		node.Submit = submit
	}
	if autofocus, ok := object["autofocus"].(bool); ok {
		node.AutoFocus = autofocus
	}
	if node.Click && node.ID == "" {
		return nil, fmt.Errorf("a clickable %s needs an id", kind)
	}
	if (node.Type == "input" || node.Submit) && node.ID == "" {
		return nil, fmt.Errorf("an input needs an id")
	}
	if node.Type == "input" && len(node.Children) > 0 {
		return nil, fmt.Errorf("an input cannot have children")
	}
	style, err := parseStyle(object)
	if err != nil {
		return nil, err
	}
	node.Style = style

	children := object["children"]
	if one, ok := object["child"]; ok && children == nil {
		children = []any{one}
	}
	if list, ok := children.([]any); ok {
		if len(list) > 200 {
			return nil, fmt.Errorf("a node has more than 200 children")
		}
		for _, child := range list {
			parsed, err := parseNode(child, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			node.Children = append(node.Children, parsed)
		}
	} else if children != nil {
		return nil, fmt.Errorf("%s children is a list, not %T", kind, children)
	}
	return node, nil
}

// parseStyle reads the style fields, accepting snake_case and the names the
// guide uses.
func parseStyle(object map[string]any) (Style, error) {
	var style Style
	style.Padding = numberField(object, "padding")
	style.Gap = numberField(object, "gap")
	style.Grow = numberField(object, "grow")
	style.Width = numberField(object, "width")
	style.Height = numberField(object, "height")
	style.MinWidth = numberField(object, "min_width")
	style.MaxWidth = numberField(object, "max_width")
	style.MinHeight = numberField(object, "min_height")
	style.MaxHeight = numberField(object, "max_height")
	style.FontSize = numberField(object, "font_size")
	style.Radius = numberField(object, "radius")
	style.TranslateX = numberField(object, "translate_x")
	style.TranslateY = numberField(object, "translate_y")
	style.Scale = numberField(object, "scale")
	style.Rotate = numberField(object, "rotate")
	style.Opacity = numberField(object, "opacity")
	style.Align = stringField(object, "align")
	style.Justify = stringField(object, "justify")
	style.Color = stringField(object, "color")
	style.Bg = stringField(object, "background")
	style.Border = stringField(object, "border")
	style.Bold = boolField(object, "bold")
	style.Italic = boolField(object, "italic")
	switch style.Align {
	case "", "start", "center", "end", "stretch":
	default:
		return Style{}, fmt.Errorf("align is start, center, end or stretch, not %q", style.Align)
	}
	switch style.Justify {
	case "", "start", "center", "end", "between":
	default:
		return Style{}, fmt.Errorf("justify is start, center, end or between, not %q", style.Justify)
	}
	if style.TranslateX < -4000 || style.TranslateX > 4000 || style.TranslateY < -4000 || style.TranslateY > 4000 {
		return Style{}, fmt.Errorf("translate is out of range")
	}
	if style.Scale < 0 || style.Scale > 10 {
		return Style{}, fmt.Errorf("scale is between 0 and 10")
	}
	if style.Rotate < -3600 || style.Rotate > 3600 {
		return Style{}, fmt.Errorf("rotate is out of range")
	}
	if style.Opacity < 0 || style.Opacity > 1 {
		return Style{}, fmt.Errorf("opacity is between 0 and 1")
	}
	for name, value := range map[string]float32{
		"padding": style.Padding, "gap": style.Gap, "grow": style.Grow,
		"width": style.Width, "height": style.Height, "font_size": style.FontSize, "radius": style.Radius,
	} {
		if value < 0 || value > 4000 {
			return Style{}, fmt.Errorf("%s is out of range", name)
		}
	}
	return style, nil
}

// stringField reads a string property, empty when absent.
func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

// numberField reads a number property; JSON numbers export as float64 and a
// whole number may be int64.
func numberField(object map[string]any, key string) float32 {
	switch value := object[key].(type) {
	case float64:
		return float32(value)
	case int64:
		return float32(value)
	case int:
		return float32(value)
	}
	return 0
}

// boolField reads a boolean property.
func boolField(object map[string]any, key string) bool {
	value, _ := object[key].(bool)
	return value
}

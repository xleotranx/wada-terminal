package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Template is one reusable agent role in templates/<name>.md: a command, an
// optional description, how to know it is ready, how to submit text, and the
// instructions it starts with.
type Template struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command"`
	Ready       string `json:"ready,omitempty"`  // "" waits for OSC 7501; "quiet" waits for quiet output
	Submit      string `json:"submit,omitempty"` // key name; empty means Enter
	Stop        string `json:"stop,omitempty"`   // kept for compatibility; not used yet
	PasteGapMS  int    `json:"paste_gap_ms,omitempty"`
	Body        string `json:"-"`
}

// TemplatesDir is where templates live in a config folder.
func TemplatesDir(dir string) string { return filepath.Join(dir, "templates") }

// ListTemplates reads every templates/<name>.md in a config folder.
func ListTemplates(dir string) ([]Template, error) {
	entries, err := os.ReadDir(TemplatesDir(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Template
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		template, err := LoadTemplate(dir, name)
		if err != nil {
			return nil, err
		}
		out = append(out, template)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LoadTemplate reads templates/<name>.md. The header is line-based, not
// YAML: a field is `key: literal text`, and no quotes are stripped.
func LoadTemplate(dir, name string) (Template, error) {
	if !safeName(name) {
		return Template{}, fmt.Errorf("template is a file name, not %q", name)
	}
	path := filepath.Join(TemplatesDir(dir), name+".md")
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Template{}, fmt.Errorf("no template %q in %s", name, TemplatesDir(dir))
		}
		return Template{}, err
	}
	template, err := parseTemplate(string(body))
	if err != nil {
		return Template{}, fmt.Errorf("%s.md: %w", name, err)
	}
	template.Name = name
	return template, nil
}

// parseTemplate reads the frontmatter and the instructions.
func parseTemplate(text string) (Template, error) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return Template{}, fmt.Errorf("the first line must be ---")
	}
	end := -1
	fields := map[string]string{}
	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			end = i
			break
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue // an unknown line is ignored, as herdr-core does
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	if end < 0 {
		return Template{}, fmt.Errorf("the header has no closing ---")
	}
	template := Template{
		Description: fields["description"],
		Command:     fields["command"],
		Ready:       fields["ready"],
		Submit:      fields["submit"],
		Stop:        fields["stop"],
		Body:        strings.Join(lines[end+1:], "\n"),
	}
	switch template.Ready {
	case "", "status", "quiet":
	default:
		return Template{}, fmt.Errorf("ready is quiet (or empty), not %q", template.Ready)
	}
	if template.Command == "" {
		return Template{}, fmt.Errorf("command is required")
	}
	if gap := fields["paste_gap_ms"]; gap != "" {
		n, err := strconv.Atoi(gap)
		if err != nil || n < 0 {
			return Template{}, fmt.Errorf("paste_gap_ms is a non-negative number")
		}
		template.PasteGapMS = n
	}
	return template, nil
}

// EnsureTemplates makes the templates folder, without writing a role nobody
// asked for.
func EnsureTemplates(dir string) error {
	return os.MkdirAll(TemplatesDir(dir), 0o700)
}

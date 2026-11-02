package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Agent is a program the Session menu can start besides the built-ins.
type Agent struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Glyph   string   `json:"glyph,omitempty"`
	Color   string   `json:"color,omitempty"`
	Icon    string   `json:"icon,omitempty"`
	Tint    bool     `json:"tint,omitempty"`
	Match   []string `json:"match,omitempty"`
}

// programFile is programs.json: entries with a run command join the Session
// menu; entries without one are only display metadata, which this window does
// not yet use.
type programFile struct {
	Programs map[string]struct {
		Name  string   `json:"name"`
		Run   []string `json:"run"`
		Glyph string   `json:"glyph"`
		Color string   `json:"color"`
		Icon  string   `json:"icon"`
		Tint  bool     `json:"tint"`
		Match []string `json:"match"`
	} `json:"programs"`
}

// Agents is agents.json: custom programs that behave like agents even before
// they report OSC 7501.
type Agents map[string]Agent

// LoadAgents reads agents.json. A missing file is an empty set.
func LoadAgents(dir string) (Agents, error) {
	var agents Agents
	if err := decodeStrict(dir+"/agents.json", &agents); err != nil {
		if os.IsNotExist(err) {
			agents = Agents{}
		} else {
			return nil, err
		}
	}
	var programs programFile
	if err := decodeStrict(dir+"/programs.json", &programs); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for name, program := range programs.Programs {
		if len(program.Run) == 0 {
			continue
		}
		if _, exists := agents[name]; exists {
			continue
		}
		agents[name] = Agent{Name: program.Name, Command: program.Run,
			Glyph: program.Glyph, Color: program.Color, Icon: program.Icon, Tint: program.Tint,
			Match: program.Match}
	}
	for name, agent := range agents {
		if !safeName(name) {
			return nil, fmt.Errorf("agent name %q is not a folder-safe name", name)
		}
		if agent.Name == "" {
			agent.Name = name
		}
		if len(agent.Command) == 0 {
			agent.Command = []string{name}
		}
		if agent.Command[0] == "" {
			return nil, fmt.Errorf("agent %q has an empty command", name)
		}
		if agent.Glyph == "" {
			agent.Glyph = "✦"
		}
		if agent.Color != "" && !validAgentColor(agent.Color) {
			return nil, fmt.Errorf("agent %q has color %q, not #rrggbb", name, agent.Color)
		}
		if agent.Icon != "" && (filepath.IsAbs(agent.Icon) || strings.Contains(agent.Icon, "..")) {
			return nil, fmt.Errorf("agent %q has icon %q, not a path under the config folder", name, agent.Icon)
		}
		agents[name] = agent
	}
	return agents, nil
}

// validColor checks an icon color written as #rrggbb.
func validAgentColor(color string) bool {
	if len(color) != 7 || color[0] != '#' {
		return false
	}
	for _, c := range color[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// AgentNames lists the configured agents in a stable order.
func (a Agents) AgentNames() []string {
	names := make([]string, 0, len(a))
	for name := range a {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

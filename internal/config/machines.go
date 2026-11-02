package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Machine is a core on another machine, reached with SSH.
type Machine struct {
	SSH string `json:"ssh"`           // the host as ssh takes it, e.g. vps or me@box
	Bin string `json:"bin,omitempty"` // the wada-terminal on that machine; default in $PATH
}

// Machines is machines.json: a name for each remote core.
type Machines map[string]Machine

// DefaultMachineBin is what a machine runs unless it names another binary.
const DefaultMachineBin = "wada-terminal"

// LoadMachines reads machines.json, an empty set when the file is absent.
func LoadMachines(dir string) (Machines, error) {
	var machines Machines
	if err := decodeStrict(filepath.Join(dir, "machines.json"), &machines); err != nil {
		if os.IsNotExist(err) {
			return Machines{}, nil
		}
		return nil, err
	}
	for name, machine := range machines {
		if !safeName(name) {
			return nil, fmt.Errorf("machine name %q is not a folder-safe name", name)
		}
		if machine.SSH == "" {
			return nil, fmt.Errorf("machine %q has no ssh host", name)
		}
		if machine.Bin == "" {
			machine.Bin = DefaultMachineBin
			machines[name] = machine
		}
	}
	return machines, nil
}

// SaveMachines writes machines.json atomically.
func SaveMachines(dir string, machines Machines) error {
	body, err := jsonMarshalIndent(machines)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "machines.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MachineTransport is what the CLI runs to reach a core over SSH. It is kept
// here as arguments so the config package needs no network or core code.
func (m Machine) SSHArgs() []string {
	return []string{"ssh", "-o", "BatchMode=yes", m.SSH, m.Bin + " core-proxy"}
}

// SSHCoreStart is the remote shell command that starts a detached core.
func (m Machine) SSHCoreStart() string {
	return "nohup " + m.Bin + " --serve-core >/dev/null 2>&1 &"
}

package config

import (
	"strings"
	"testing"
)

// TestMachines checks machines.json's defaults, round trip and errors.
func TestMachines(t *testing.T) {
	dir := testDir(t)
	machines, err := LoadMachines(dir)
	if err != nil || len(machines) != 0 {
		t.Fatalf("empty machines = %+v, %v", machines, err)
	}
	machines["vps"] = Machine{SSH: "me@vps", Bin: "/usr/local/bin/wada-terminal"}
	if err := SaveMachines(dir, machines); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadMachines(dir)
	if err != nil {
		t.Fatal(err)
	}
	machine := loaded["vps"]
	if machine.SSH != "me@vps" || machine.Bin != "/usr/local/bin/wada-terminal" {
		t.Fatalf("machine = %+v", machine)
	}
	args := strings.Join(machine.SSHArgs(), " ")
	if !strings.Contains(args, "ssh") || !strings.Contains(args, "me@vps") || !strings.Contains(args, "core-proxy") {
		t.Fatalf("SSHArgs = %q", args)
	}
	if !strings.Contains(machine.SSHCoreStart(), "--serve-core") {
		t.Fatalf("SSHCoreStart = %q", machine.SSHCoreStart())
	}

	write(t, dir+"/machines.json", `{"box": {"ssh": "", "bin": "wada"}}`)
	if _, err := LoadMachines(dir); err == nil {
		t.Fatal("a machine with no ssh host was accepted")
	}
	write(t, dir+"/machines.json", `{"../box": {"ssh": "box"}}`)
	if _, err := LoadMachines(dir); err == nil {
		t.Fatal("a machine name that leaves the folder was accepted")
	}
}

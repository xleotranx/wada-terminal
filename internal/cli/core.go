package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"wada-terminal/internal/appdir"
	"wada-terminal/internal/config"
	"wada-terminal/internal/core"
)

// RunCore is the core's command line: the terminal runtime without the
// window. It is what an agent uses to start a program, read it, type into it,
// wait for it, and stop it. The window is another client of the same core.
func RunCore(folder, env string, args []string) int {
	if len(args) == 0 {
		printCoreUsage()
		return 2
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printCoreUsage()
		return 0
	}
	prefixMachine := ""
	for len(args) > 1 && args[0] == "--on" {
		if prefixMachine != "" {
			return coreUsageError("--on is named twice")
		}
		prefixMachine, args = args[1], args[2:]
	}
	if len(args) == 0 {
		printCoreUsage()
		return 2
	}
	dir, err := appdir.Dir(folder, env)
	if err != nil {
		coreError(err)
		return 1
	}
	if args[0] == "templates" {
		return coreTemplates()
	}
	if args[0] == "machine" {
		return coreMachines(dir, args[1:])
	}
	machine, rest, err := extractOn(args[1:])
	if err != nil {
		return coreUsageError(err.Error())
	}
	if machine != "" && prefixMachine != "" {
		return coreUsageError("--on is named twice")
	}
	if prefixMachine != "" {
		machine = prefixMachine
	}
	client, err := connectCore(dir, machine)
	if err != nil {
		coreError(err)
		return 1
	}
	switch args[0] {
	case "ls":
		list, err := client.List()
		if err != nil {
			coreError(err)
			return 1
		}
		return printJSON(list)
	case "run":
		return coreRun(client, rest)
	case "read":
		return coreRead(client, rest)
	case "send":
		return coreSend(client, rest)
	case "wait":
		return coreWait(client, rest)
	case "kill":
		return coreKill(client, rest)
	case "report":
		return coreReport(client, rest)
	case "events":
		return coreEvents(client, rest)
	case "ask":
		return coreAsk(client, rest)
	case "answer":
		return coreAnswer(client, rest)
	case "questions":
		return coreQuestions(client, rest)
	case "layout":
		return coreLayout(client, rest)
	default:
		fmt.Fprintf(os.Stderr, "wada-terminal core: no command %q\n\n", args[0])
		printCoreUsage()
		return 2
	}
}

// extractOn takes --on MACHINE out of a command's arguments. Arguments after
// -- are left alone, so a run program keeps its own --on.
func extractOn(args []string) (string, []string, error) {
	machine := ""
	out := make([]string, 0, len(args))
	protected := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			protected = true
			out = append(out, args[i])
			continue
		}
		if protected || args[i] != "--on" {
			out = append(out, args[i])
			continue
		}
		if i+1 >= len(args) {
			return "", nil, fmt.Errorf("--on needs a machine")
		}
		if machine != "" {
			return "", nil, fmt.Errorf("--on is named twice")
		}
		machine = args[i+1]
		i++
	}
	return machine, out, nil
}

// connectCore opens the core this command talks to: the local one, or one on
// a machine from machines.json over SSH.
func connectCore(dir, machine string) (*core.Client, error) {
	if machine == "" || machine == "local" {
		if err := core.Ensure(dir); err != nil {
			return nil, err
		}
		return core.Connect(dir)
	}
	machines, err := config.LoadMachines(dir)
	if err != nil {
		return nil, err
	}
	remote, ok := machines[machine]
	if !ok {
		return nil, fmt.Errorf("no machine %q; add it with `wada-terminal core machine add`", machine)
	}
	client, err := core.ConnectMachine(remote.SSH, remote.Bin, os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("machine %s: %w", machine, err)
	}
	return client, nil
}

// coreMachines manages machines.json: the cores this window can reach over
// SSH. It needs no running core.
func coreMachines(dir string, args []string) int {
	if len(args) == 0 || args[0] == "ls" {
		machines, err := config.LoadMachines(dir)
		if err != nil {
			coreError(err)
			return 1
		}
		type view struct {
			Name string `json:"name"`
			SSH  string `json:"ssh"`
			Bin  string `json:"bin"`
		}
		out := make([]view, 0, len(machines))
		for name, machine := range machines {
			out = append(out, view{Name: name, SSH: machine.SSH, Bin: machine.Bin})
		}
		return printJSON(out)
	}
	machines, err := config.LoadMachines(dir)
	if err != nil {
		coreError(err)
		return 1
	}
	switch args[0] {
	case "add":
		if len(args) < 2 {
			return coreUsageError("machine add needs a name")
		}
		name := args[1]
		machine := config.Machine{SSH: name, Bin: config.DefaultMachineBin}
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--ssh":
				if i+1 >= len(args) {
					return coreUsageError("--ssh needs a host")
				}
				machine.SSH, i = args[i+1], i+1
			case "--bin":
				if i+1 >= len(args) {
					return coreUsageError("--bin needs a path")
				}
				machine.Bin, i = args[i+1], i+1
			default:
				return coreUsageError("unknown option " + args[i])
			}
		}
		machines[name] = machine
		if err := config.SaveMachines(dir, machines); err != nil {
			coreError(err)
			return 1
		}
		return printJSON(map[string]any{"machine": name, "ssh": machine.SSH, "bin": machine.Bin})
	case "remove":
		if len(args) != 2 {
			return coreUsageError("machine remove needs a name")
		}
		delete(machines, args[1])
		if err := config.SaveMachines(dir, machines); err != nil {
			coreError(err)
			return 1
		}
		return printJSON(map[string]any{"removed": args[1]})
	default:
		return coreUsageError("machine takes ls, add or remove")
	}
}

// coreRun starts a program. In the foreground it streams the program's output
// and returns its exit code; --bg prints the terminal's address and returns.
func coreRun(client *core.Client, args []string) int {
	background, notify, notifySet := false, false, false
	name, cwd, kind, templateName := "", "", "shell", ""
	labels := map[string]string{}
	readySeconds := 30
	cols, rows := envSize()
	for len(args) > 0 {
		switch arg := args[0]; arg {
		case "--":
			args = args[1:]
			goto parsed
		case "--bg":
			background = true
			args = args[1:]
		case "--notify":
			notify, notifySet, args = true, true, args[1:]
		case "--no-notify":
			notify, notifySet, args = false, true, args[1:]
		case "--name":
			if len(args) < 2 {
				return coreUsageError("--name needs a value")
			}
			name, args = args[1], args[2:]
		case "--cwd":
			if len(args) < 2 {
				return coreUsageError("--cwd needs a value")
			}
			cwd, args = args[1], args[2:]
		case "--kind":
			if len(args) < 2 {
				return coreUsageError("--kind needs a value")
			}
			kind, args = args[1], args[2:]
		case "--label":
			if len(args) < 2 || !strings.Contains(args[1], "=") {
				return coreUsageError("--label needs NAME=VALUE")
			}
			pair := strings.SplitN(args[1], "=", 2)
			if pair[0] == "" {
				return coreUsageError("--label needs a name")
			}
			labels[pair[0]] = pair[1]
			args = args[2:]
		case "--cols":
			if len(args) < 2 {
				return coreUsageError("--cols needs a value")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 2 {
				return coreUsageError("--cols is a number of columns")
			}
			cols, args = n, args[2:]
		case "--rows":
			if len(args) < 2 {
				return coreUsageError("--rows needs a value")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 2 {
				return coreUsageError("--rows is a number of rows")
			}
			rows, args = n, args[2:]
		case "--template":
			if len(args) < 2 {
				return coreUsageError("--template needs a name")
			}
			templateName, args = args[1], args[2:]
		case "--ready-timeout":
			if len(args) < 2 {
				return coreUsageError("--ready-timeout needs seconds")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				return coreUsageError("--ready-timeout is a positive number of seconds")
			}
			readySeconds, args = n, args[2:]
		default:
			if strings.HasPrefix(arg, "-") {
				return coreUsageError("unknown option " + arg)
			}
			break
		}
		if len(args) == 0 || !strings.HasPrefix(args[0], "-") {
			break
		}
	}
parsed:
	var template *config.Template
	program := args
	message := ""
	if templateName != "" {
		dir, err := config.Dir()
		if err != nil {
			coreError(err)
			return 1
		}
		tpl, err := config.LoadTemplate(dir, templateName)
		if err != nil {
			coreError(err)
			return 1
		}
		template = &tpl
		program = []string{"/bin/sh", "-c", tpl.Command}
		if name == "" {
			name = templateName
		}
		message = templateMessage(tpl, strings.Join(args, " "))
	} else if len(args) == 0 {
		return coreUsageError("run needs a program or --template")
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if !notifySet {
		notify = background // a background helper's exit is news to its parent
	}
	rec, err := client.Start(core.Start{
		Argv: program, Kind: kind, Name: name, Parent: parentAddress(), Notify: notify, Dir: cwd,
		Cols: cols, Rows: rows, Dark: true, Labels: labels,
	})
	if err != nil {
		coreError(err)
		return 1
	}
	if background {
		fmt.Println(address(rec.ID))
	}
	if template != nil && message != "" {
		if waitTemplateReady(client, rec.ID, *template, readySeconds) {
			if err := sendTemplateMessage(client, rec.ID, *template, message); err != nil {
				fmt.Fprintln(os.Stderr, "wada-terminal core:", err)
			}
		} else {
			fmt.Fprintf(os.Stderr, "wada-terminal core: %s did not become ready; its message was not sent. Read it first.\n", address(rec.ID))
		}
	}
	if background {
		return 0
	}
	stream, err := client.Attach(rec.ID, rec.Cols, rec.Rows, nil)
	if err != nil {
		coreError(err)
		return 1
	}
	defer stream.Close()
	for {
		data, exit, err := stream.Next()
		if err != nil {
			coreError(err)
			return 1
		}
		if len(data) > 0 {
			_, _ = os.Stdout.Write(data)
		}
		if exit != nil {
			// A normal nonzero exit is the program's code, not an error
			// to print; a signal or an unknown end has no code to return.
			if exit.Error != "" && exit.Code <= 0 {
				fmt.Fprintln(os.Stderr, exit.Error)
			}
			if exit.Code != 0 {
				return exit.Code
			}
			if exit.Error != "" {
				return 1
			}
			return 0
		}
	}
}

// templateMessage is a template's instructions and the user's task in one
// first message: the body, a blank line, then the task.
func templateMessage(template config.Template, task string) string {
	body := strings.Trim(template.Body, "\n")
	task = strings.TrimSpace(task)
	switch {
	case body != "" && task != "":
		return body + "\n\n" + task
	case body != "":
		return body
	default:
		return task
	}
}

// waitTemplateReady waits for the signal a template declares: an OSC 7501
// report by default, or one second of quiet output when ready: quiet.
func waitTemplateReady(client *core.Client, id int, template config.Template, seconds int) bool {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	last := ""
	quietSince := time.Now()
	for {
		list, err := client.List()
		if err != nil {
			return false
		}
		var record *core.Record
		for i := range list.Terminals {
			if list.Terminals[i].ID == id {
				record = &list.Terminals[i]
				break
			}
		}
		if record == nil || !record.Alive {
			return false
		}
		if template.Ready != "quiet" {
			if record.Reported {
				return true
			}
		} else {
			read, err := client.Read(id, 20)
			if err != nil {
				return false
			}
			text := strings.Join(read.Lines, "\n")
			if text != last {
				last, quietSince = text, time.Now()
			} else if !quietSince.IsZero() && time.Since(quietSince) >= time.Second {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// sendTemplateMessage types the first message and then the template's submit
// key, with the declared paste gap when one is set.
func sendTemplateMessage(client *core.Client, id int, template config.Template, message string) error {
	submit := submitBytes(template.Submit)
	if template.PasteGapMS > 0 {
		if err := client.Send(id, []byte(message)); err != nil {
			return err
		}
		time.Sleep(time.Duration(template.PasteGapMS) * time.Millisecond)
		return client.Send(id, []byte(submit))
	}
	return client.Send(id, []byte(message+submit))
}

// submitBytes turns a template's submit name into the keys a terminal gets.
func submitBytes(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "enter", "return":
		return "\r"
	case "tab":
		return "\t"
	case "space":
		return " "
	case "escape", "esc":
		return "\x1b"
	case "up":
		return "\x1b[A"
	case "down":
		return "\x1b[B"
	case "right":
		return "\x1b[C"
	case "left":
		return "\x1b[D"
	}
	if strings.HasPrefix(strings.ToLower(name), "ctrl+") && len(name) == len("ctrl+x") {
		if c := strings.ToLower(name[len("ctrl+"):]); len(c) == 1 && c[0] >= 'a' && c[0] <= 'z' {
			return string(rune(c[0] - 'a' + 1))
		}
	}
	return name
}

// coreTemplates lists the templates in the config folder.
func coreTemplates() int {
	dir, err := config.Dir()
	if err != nil {
		coreError(err)
		return 1
	}
	templates, err := config.ListTemplates(dir)
	if err != nil {
		coreError(err)
		return 1
	}
	return printJSON(templates)
}

// coreRead prints the last lines of a terminal's screen.
func coreRead(client *core.Client, args []string) int {
	ref, lines := "", 80
	for len(args) > 0 {
		switch arg := args[0]; arg {
		case "--lines":
			if len(args) < 2 {
				return coreUsageError("--lines needs a number")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				return coreUsageError("--lines is a positive number")
			}
			lines, args = n, args[2:]
		default:
			if strings.HasPrefix(arg, "-") {
				return coreUsageError("unknown option " + arg)
			}
			if ref != "" {
				return coreUsageError("read takes one terminal")
			}
			ref, args = arg, args[1:]
		}
	}
	if ref == "" {
		return coreUsageError("read needs a terminal")
	}
	rec, err := resolveTerminal(client, ref)
	if err != nil {
		coreError(err)
		return 1
	}
	read, err := client.Read(rec.ID, lines)
	if err != nil {
		coreError(err)
		return 1
	}
	for _, line := range read.Lines {
		fmt.Println(line)
	}
	return 0
}

// coreSend types text into a terminal, with Return unless asked not to.
func coreSend(client *core.Client, args []string) int {
	ref, enter, words := "", true, []string{}
	for len(args) > 0 {
		switch arg := args[0]; arg {
		case "--no-enter":
			enter, args = false, args[1:]
		case "--enter":
			enter, args = true, args[1:]
		default:
			if strings.HasPrefix(arg, "-") && ref == "" {
				return coreUsageError("unknown option " + arg)
			}
			if ref == "" {
				ref, args = arg, args[1:]
			} else {
				words = append(words, arg)
				args = args[1:]
			}
		}
	}
	if ref == "" {
		return coreUsageError("send needs a terminal and text")
	}
	rec, err := resolveTerminal(client, ref)
	if err != nil {
		coreError(err)
		return 1
	}
	text := strings.Join(words, " ")
	if enter {
		text += "\r"
	}
	if err := client.Send(rec.ID, []byte(text)); err != nil {
		coreError(err)
		return 1
	}
	return printJSON(map[string]any{"terminal": address(rec.ID), "sent": len(text)})
}

// coreWait blocks until a terminal's program ends and returns its exit code.
func coreWait(client *core.Client, args []string) int {
	ref, seconds, children, anyChild := "", 180, false, false
	for len(args) > 0 {
		switch arg := args[0]; arg {
		case "--children":
			children, args = true, args[1:]
		case "--any":
			anyChild, args = true, args[1:]
		case "--timeout":
			if len(args) < 2 {
				return coreUsageError("--timeout needs seconds")
			}
			n, err := strconv.Atoi(args[1])
			if err != nil || n < 1 {
				return coreUsageError("--timeout is a positive number of seconds")
			}
			seconds, args = n, args[2:]
		default:
			if strings.HasPrefix(arg, "-") {
				return coreUsageError("unknown option " + arg)
			}
			if ref != "" {
				return coreUsageError("wait takes one terminal")
			}
			ref, args = arg, args[1:]
		}
	}
	if children {
		if parentAddress() == 0 {
			return coreUsageError("--children must run inside a terminal (WADA_TERMINAL_ID is not set)")
		}
		return waitChildren(client, parentAddress(), seconds, anyChild)
	}
	if anyChild {
		return coreUsageError("--any goes with --children")
	}
	if ref == "" {
		return coreUsageError("wait needs a terminal or --children")
	}
	rec, err := resolveTerminal(client, ref)
	if err != nil {
		coreError(err)
		return 1
	}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for {
		read, err := client.Read(rec.ID, 1)
		if err != nil {
			coreError(err)
			return 1
		}
		if !read.Alive {
			exit := core.Exit{}
			if read.Exit != nil {
				exit = *read.Exit
			}
			_ = printJSON(map[string]any{"terminal": address(rec.ID), "exit": exit})
			if exit.Code != 0 {
				return exit.Code
			}
			if exit.Error != "" {
				return 1
			}
			return 0
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "wada-terminal core: %s is still running after %ds\n", ref, seconds)
			return 2
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// coreKill ends a terminal and its program.
func coreKill(client *core.Client, args []string) int {
	if len(args) != 1 {
		return coreUsageError("kill takes one terminal")
	}
	rec, err := resolveTerminal(client, args[0])
	if err != nil {
		coreError(err)
		return 1
	}
	if err := client.Kill(rec.ID); err != nil {
		coreError(err)
		return 1
	}
	return printJSON(map[string]any{"terminal": address(rec.ID), "killed": true})
}

// coreReport sends a tagged note to the terminal that started this one. It is
// the small version of herdr-core's report: the parent sees the text in its
// input, with the child's address, instead of polling the child's screen.
func coreReport(client *core.Client, args []string) int {
	text := strings.Join(args, " ")
	if text == "" {
		return coreUsageError("report needs text")
	}
	self := parentAddress()
	if self == 0 {
		return coreUsageError("report must run inside a terminal (WADA_TERMINAL_ID is not set)")
	}
	rec, err := resolveTerminal(client, address(self))
	if err != nil {
		coreError(err)
		return 1
	}
	if rec.Parent == 0 {
		return coreUsageError("this terminal has no parent to report to")
	}
	from := address(rec.ID)
	if rec.Name != "" {
		from += " " + rec.Name
	}
	note := fmt.Sprintf("\n<wada from=\"%s\">%s</wada>\n", escapeNoteText(from), escapeNoteText(text))
	if err := client.Send(rec.Parent, []byte(note+"\r")); err != nil {
		coreError(err)
		return 1
	}
	return printJSON(map[string]any{"from": address(rec.ID), "to": address(rec.Parent), "sent": len(note)})
}

// waitChildren waits until none of this terminal's children is still working.
// A child with an OSC 7501 status is done when it leaves working or blocked; a
// child without one is done when its program ends.
func waitChildren(client *core.Client, parent, seconds int, anyChild bool) int {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	before := map[int]core.Record{}
	first := true
	for {
		list, err := client.List()
		if err != nil {
			coreError(err)
			return 1
		}
		var children []core.Record
		for _, rec := range list.Terminals {
			if rec.Parent == parent {
				children = append(children, rec)
			}
		}
		if anyChild {
			// News is a child that ended or changed what it is doing. A
			// child already ended before the wait started is news too.
			for _, rec := range children {
				old, seen := before[rec.ID]
				news := !rec.Alive || rec.Word == "blocked" || rec.Word == "error" || rec.Word == "done"
				if !first && seen && (old.Alive != rec.Alive || old.Word != rec.Word) {
					news = true
				}
				before[rec.ID] = rec
				if news {
					return printJSON(map[string]any{"children": children})
				}
			}
			first = false
		} else {
			active := 0
			for _, rec := range children {
				if !rec.Alive {
					continue
				}
				switch rec.Word {
				case "working", "blocked", "error":
					active++
				case "idle", "done":
					// An agent at its prompt is not working.
				default:
					active++ // a plain program is still running
				}
			}
			if active == 0 {
				return printJSON(map[string]any{"children": children})
			}
		}
		if time.Now().After(deadline) {
			return printJSON(map[string]any{"children": children, "timeout": true})
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// coreEvents prints the core's changes as JSON lines: created terminals,
// status and title changes, and exits. With --children it keeps only the
// terminals this one started.
func coreEvents(client *core.Client, args []string) int {
	onlyChildren := false
	for _, arg := range args {
		switch arg {
		case "--children":
			onlyChildren = true
		default:
			return coreUsageError("unknown option " + arg)
		}
	}
	parent := 0
	if onlyChildren {
		parent = parentAddress()
		if parent == 0 {
			return coreUsageError("--children must run inside a terminal (WADA_TERMINAL_ID is not set)")
		}
	}
	events, err := client.Events()
	if err != nil {
		coreError(err)
		return 1
	}
	defer events.Close()
	encoder := json.NewEncoder(os.Stdout)
	for {
		event, err := events.Next()
		if err != nil {
			return 0
		}
		if onlyChildren && event.Record != nil && event.Record.Parent != parent {
			continue
		}
		if err := encoder.Encode(event); err != nil {
			return 1
		}
	}
}

// escapeNoteText keeps a value from closing a note's attribute or adding a line.
func escapeNoteText(text string) string {
	return strings.NewReplacer("\"", "'", "\n", " ", "\r", " ").Replace(text)
}

// resolveTerminal finds a terminal by address (t7 or 7) or by the name it was
// started with.
func resolveTerminal(client *core.Client, ref string) (core.Record, error) {
	list, err := client.List()
	if err != nil {
		return core.Record{}, err
	}
	if id, ok := parseAddress(ref); ok {
		for _, rec := range list.Terminals {
			if rec.ID == id {
				return rec, nil
			}
		}
		return core.Record{}, fmt.Errorf("no terminal %s", ref)
	}
	var found *core.Record
	for i := range list.Terminals {
		rec := &list.Terminals[i]
		if rec.Name == ref || rec.Title == ref {
			if found != nil {
				return core.Record{}, fmt.Errorf("%q names more than one terminal; use its address", ref)
			}
			found = rec
		}
	}
	if found == nil {
		return core.Record{}, fmt.Errorf("no terminal %q", ref)
	}
	return *found, nil
}

// parentAddress is the terminal this command runs in, when it runs in one.
func parentAddress() int {
	if id, ok := parseAddress(os.Getenv("WADA_TERMINAL_ID")); ok {
		return id
	}
	return 0
}

// parseAddress reads t7 or 7.
func parseAddress(ref string) (int, bool) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "t")
	n, err := strconv.Atoi(ref)
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// address is how a terminal is named in output.
func address(id int) string { return "t" + strconv.Itoa(id) }

// envSize is the terminal size the shell reports, or a useful default.
func envSize() (int, int) {
	cols, rows := 80, 24
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n >= 2 {
		cols = n
	}
	if n, err := strconv.Atoi(os.Getenv("LINES")); err == nil && n >= 2 {
		rows = n
	}
	return cols, rows
}

// coreError prints a core command failure.
func coreError(err error) { fmt.Fprintln(os.Stderr, "wada-terminal core:", err) }

// coreUsageError prints a bad argument.
func coreUsageError(message string) int {
	fmt.Fprintln(os.Stderr, "wada-terminal core:", message)
	return 2
}

// printCoreUsage prints the core command line.
func printCoreUsage() {
	fmt.Print(`wada-terminal core: programs that outlive the window

  wada-terminal core ls
  wada-terminal core run [--bg] [--name NAME] [--cwd DIR] [--label NAME=VALUE] [--notify|--no-notify] [--template NAME TASK] [--ready-timeout S] [--] PROGRAM...
  wada-terminal core read TERMINAL [--lines N]
  wada-terminal core send TERMINAL [--no-enter] TEXT...
  wada-terminal core wait TERMINAL [--timeout SECONDS]
  wada-terminal core wait --children [--any] [--timeout SECONDS]
  wada-terminal core --on MACHINE COMMAND...  (or add --on MACHINE to a command)
  wada-terminal core machine ls|add NAME [--ssh HOST] [--bin PATH]|remove NAME
  wada-terminal core templates
  wada-terminal core report TEXT...
  wada-terminal core layout get|set FILE
  wada-terminal core events
  wada-terminal core ask "Which file should I edit?"
  wada-terminal core answer t7 --question q1 "src/x.go"
  wada-terminal core questions [--children]
  wada-terminal core kill TERMINAL
  wada-terminal core help

A TERMINAL is an address (t7) or a name given with --name. A foreground run
streams the program and exits with its code; --bg prints the address.
`)
}

// coreLayout reads or replaces a core's saved spaces document. The window
// owns the shape; the core keeps the bytes.
func coreLayout(client *core.Client, args []string) int {
	if len(args) == 0 {
		return coreUsageError("layout needs get or set")
	}
	switch args[0] {
	case "get":
		layout, err := client.Layout()
		if err != nil {
			coreError(err)
			return 1
		}
		if len(layout) == 0 {
			fmt.Println("{}")
			return 0
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, layout, "", "  "); err != nil {
			coreError(err)
			return 1
		}
		fmt.Println(pretty.String())
		return 0
	case "set":
		if len(args) < 2 {
			return coreUsageError("layout set needs a file, or - for stdin")
		}
		var body []byte
		var err error
		if args[1] == "-" {
			body, err = io.ReadAll(os.Stdin)
		} else {
			body, err = os.ReadFile(args[1])
		}
		if err != nil {
			coreError(err)
			return 1
		}
		if !json.Valid(body) {
			coreError(fmt.Errorf("layout is not valid JSON"))
			return 1
		}
		if err := client.SetLayout(body); err != nil {
			coreError(err)
			return 1
		}
		fmt.Println("saved")
		return 0
	default:
		return coreUsageError("layout needs get or set")
	}
}

// coreAsk asks this terminal's parent a question and waits for the answer.
// The parent is told how to answer in a tagged note in its input.
func coreAsk(client *core.Client, args []string) int {
	timeout := 5 * time.Minute
	for len(args) > 0 && args[0] != "--" {
		switch args[0] {
		case "--timeout":
			if len(args) < 2 {
				return coreUsageError("--timeout needs a duration")
			}
			parsed, err := questionTimeout(args[1])
			if err != nil {
				return coreUsageError(err.Error())
			}
			timeout, args = parsed, args[2:]
		default:
			if strings.HasPrefix(args[0], "-") {
				return coreUsageError("unknown option " + args[0])
			}
			goto text
		}
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
text:
	text := strings.Join(args, " ")
	if strings.TrimSpace(text) == "" {
		return coreUsageError("ask needs a question")
	}
	self := parentAddress()
	if self == 0 {
		return coreUsageError("ask must run inside a terminal (WADA_TERMINAL_ID is not set)")
	}
	rec, err := resolveTerminal(client, address(self))
	if err != nil {
		coreError(err)
		return 1
	}
	if rec.Parent == 0 {
		return coreUsageError("this terminal has no parent to ask")
	}
	question, err := client.Ask(rec.ID, rec.Parent, text)
	if err != nil {
		coreError(err)
		return 1
	}
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			_ = client.CancelQuestion(question.ID)
			fmt.Fprintf(os.Stderr, "wada-terminal core: no answer to %s after %s\n", question.ID, timeout)
			return 1
		}
		wait := min(remaining, 20*time.Second)
		current, err := client.WaitQuestion(question.ID, wait)
		if err != nil {
			coreError(err)
			return 1
		}
		if current.Answer != "" {
			fmt.Println(current.Answer)
			return 0
		}
		if current.Closed {
			fmt.Fprintf(os.Stderr, "wada-terminal core: question %s was closed\n", question.ID)
			return 1
		}
	}
}

// questionTimeout reads a duration, or a plain number of seconds.
func questionTimeout(text string) (time.Duration, error) {
	if seconds, err := strconv.Atoi(text); err == nil {
		if seconds <= 0 {
			return 0, fmt.Errorf("timeout must be positive")
		}
		return time.Duration(seconds) * time.Second, nil
	}
	duration, err := time.ParseDuration(text)
	if err != nil {
		return 0, fmt.Errorf("timeout is a duration such as 5m or 30s")
	}
	if duration <= 0 {
		return 0, fmt.Errorf("timeout must be positive")
	}
	return duration, nil
}

// coreAnswer answers one of a child's questions, the oldest by default.
func coreAnswer(client *core.Client, args []string) int {
	if len(args) < 1 {
		return coreUsageError("answer needs a terminal")
	}
	child, err := resolveTerminal(client, args[0])
	if err != nil {
		coreError(err)
		return 1
	}
	args = args[1:]
	questionID := ""
	for len(args) > 0 && args[0] != "--" {
		if args[0] == "--question" {
			if len(args) < 2 {
				return coreUsageError("--question needs an id")
			}
			questionID, args = args[1], args[2:]
			continue
		}
		if strings.HasPrefix(args[0], "-") {
			return coreUsageError("unknown option " + args[0])
		}
		break
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	answer := strings.Join(args, " ")
	if strings.TrimSpace(answer) == "" {
		return coreUsageError("answer needs text")
	}
	if questionID == "" {
		questions, err := client.Questions()
		if err != nil {
			coreError(err)
			return 1
		}
		for _, question := range questions {
			if question.Child == child.ID && !question.Closed && question.Answer == "" {
				questionID = question.ID
				break
			}
		}
		if questionID == "" {
			return coreUsageError("no open question for " + address(child.ID))
		}
	}
	answered, err := client.AnswerQuestion(questionID, answer)
	if err != nil {
		coreError(err)
		return 1
	}
	return printJSON(answered)
}

// coreQuestions lists questions, open ones by default.
func coreQuestions(client *core.Client, args []string) int {
	childID, all := 0, false
	for len(args) > 0 {
		switch args[0] {
		case "--child":
			if len(args) < 2 {
				return coreUsageError("--child needs a terminal")
			}
			rec, err := resolveTerminal(client, args[1])
			if err != nil {
				coreError(err)
				return 1
			}
			childID, args = rec.ID, args[2:]
		case "--all":
			all, args = true, args[1:]
		default:
			return coreUsageError("unknown option " + args[0])
		}
	}
	questions, err := client.Questions()
	if err != nil {
		coreError(err)
		return 1
	}
	out := make([]core.Question, 0, len(questions))
	for _, question := range questions {
		if childID != 0 && question.Child != childID {
			continue
		}
		if !all && (question.Closed || question.Answer != "") {
			continue
		}
		out = append(out, question)
	}
	return printJSON(out)
}

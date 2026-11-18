package bw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type failure struct {
	code    int
	message string
}

func (e *failure) Error() string { return e.message }
func fail(code int, format string, args ...any) error {
	return &failure{code, fmt.Sprintf(format, args...)}
}
func marshal(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

type options struct {
	root                                        string
	command                                     string
	args                                        []string
	statuses                                    []string
	as, worker                                  string
	effort, port, shell                         string
	role, timeout, interval                     string
	undo                                        bool
	json, all, next, force, help, markdown      bool
	open, mine, self, check, technical          bool
	branch, head, note, notes, model, sha, task string
	reason                                      string
}

func parse(args []string) (options, error) {
	if len(args) > 0 && args[0] == "_complete" {
		return options{command: "_complete", args: args[1:]}, nil
	}
	o := options{command: "help"}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--undo":
			o.undo = true
		case "--json":
			o.json = true
		case "--check":
			o.check = true
		case "--open":
			o.open = true
		case "--mine":
			o.mine = true
		case "--self":
			o.self = true
		case "--technical":
			o.technical = true
		case "--branch", "--head", "--note", "--notes", "--model", "--sha", "--task", "--reason", "--worker", "--state", "--effort", "--port", "--shell", "--role", "--timeout", "--interval":
			i++
			if i == len(args) || strings.HasPrefix(args[i], "--") {
				return o, fail(1, "%s needs a value", arg)
			}
			switch arg {
			case "--role":
				o.role = args[i]
			case "--timeout":
				o.timeout = args[i]
			case "--interval":
				o.interval = args[i]
			case "--effort":
				o.effort = args[i]
			case "--port":
				o.port = args[i]
			case "--shell":
				o.shell = args[i]
			case "--branch":
				o.branch = args[i]
			case "--head":
				o.head = args[i]
			case "--note":
				o.note = args[i]
			case "--notes":
				o.notes = args[i]
			case "--model":
				o.model = args[i]
			case "--sha":
				o.sha = args[i]
			case "--task":
				o.task = args[i]
			case "--state":
				o.statuses = append(o.statuses, args[i])
			case "--worker":
				o.worker = args[i]
			case "--reason":
				o.reason = args[i]
			}
		case "--all":
			o.all = true
		case "--next":
			o.next = true
		case "--force":
			o.force = true
		case "--help", "-h":
			o.help = true
		case "--markdown":
			o.markdown = true
		case "--as":
			i++
			if i == len(args) || strings.HasPrefix(args[i], "-") {
				return o, fail(1, "--as needs a name. Example: bw claim task-id --as sam")
			}
			o.as = args[i]
		default:
			if strings.HasPrefix(arg, "--as=") {
				o.as = strings.TrimPrefix(arg, "--as=")
			} else if strings.HasPrefix(arg, "-") {
				return o, fail(1, "Unknown flag %q. Run bw help.", arg)
			} else {
				positional = append(positional, arg)
			}
		}
	}
	if len(positional) > 0 {
		o.command = positional[0]
		o.args = positional[1:]
	}
	if o.command == "dashboard" {
		o.command = "web"
	}
	if o.effort == "" {
		o.effort = os.Getenv("BW_EFFORT")
	}
	if o.role == "" {
		o.role = os.Getenv("BW_ROLE")
	}
	if _, err := workerRoles(o.role); err != nil {
		return o, err
	}
	if o.timeout != "" || o.interval != "" || o.command == "wait" {
		if o.command != "wait" {
			return o, fail(1, "--timeout and --interval require bw wait")
		}
		if _, _, err := waitDurations(o); err != nil {
			return o, err
		}
	}
	if !validEffort(o.effort) {
		return o, fail(1, "effort must be low, medium or high")
	}
	if o.undo && o.command != "step" || o.port != "" && o.command != "web" || o.shell != "" && o.command != "completion" {
		return o, fail(1, "Flag is not supported here")
	}
	if o.command == "q" {
		o.command = "queue"
	}
	if o.command != "help" && !slices.Contains(commandOrder, o.command) {
		return o, fail(1, "Unknown command %q. Run bw help.", o.command)
	}
	if o.command == "crack" && !o.help {
		o.command, o.next = "claim", true
	}
	if o.check && o.command != "holds" {
		return o, fail(1, "--check is only supported by bw holds.")
	}
	if len(o.statuses) > 0 && o.command != "tasks" || o.worker != "" && o.command != "stats" {
		return o, fail(1, "Flag is not supported here. Run bw %s --help.", o.command)
	}
	for _, status := range o.statuses {
		if !slices.Contains(taskStates, status) {
			return o, fail(1, "Unknown state %q; use claimed, ready, waiting or done.", status)
		}
	}

	if o.command == "completion" || o.command == "_complete" {
		return o, nil
	}
	if boardCommand(o.command) {
		return parseBoard(o)
	}
	if o.open && o.command != "web" || o.mine && o.command != "todo" || o.self || o.branch != "" || o.head != "" || o.note != "" || o.notes != "" || o.model != "" || o.sha != "" || o.task != "" && o.command != "stats" || o.technical {
		return o, fail(1, "Flag is not supported here. Run bw %s --help.", o.command)
	}
	if o.all && o.command != "queue" || o.next && o.command != "claim" || o.force && o.command != "release" || o.reason != "" && !o.force || o.markdown && o.command != "help" && o.command != "guide" {
		return o, fail(1, "Flag is not supported here. Run bw %s --help.", o.command)
	}
	if o.help {
		return o, nil
	}
	want := 0
	switch o.command {
	case "step":
		want = 2
	case "show", "release", "done", "reopen":
		want = 1
	case "claim":
		if !o.next {
			want = 1
		}
	case "holds", "help", "guide", "workers":
		if len(o.args) <= 1 {
			return o, nil
		}
	}
	if len(o.args) != want {
		return o, fail(1, "Wrong arguments. Run bw %s --help.", o.command)
	}
	return o, nil
}

type outcome struct {
	description   string
	usefulWorkers *int
	result        any
	source        string // where the task catalog came from: "local" or "origin@<commit>"
}

// Run emits exactly one JSON value in agent mode, including usage failures.
func Run(args []string, stdout, stderr io.Writer) int {
	o, e := parse(args)
	// A parse error can precede --json; still honor the requested output format.
	for _, arg := range args {
		if arg == "--json" && o.command != "_complete" {
			o.json = true
		}
	}
	var out outcome
	if e == nil && o.command == "tui" && !o.help && !o.json {
		if err := runTUI(stdout, o); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	if !o.json && !o.markdown {
		humanOut := &humanWriter{Writer: stdout, width: outputWidth(stdout)}
		humanErr := &humanWriter{Writer: stderr, width: outputWidth(stderr)}
		stdout, stderr = humanOut, humanErr
		defer humanOut.flush()
		defer humanErr.flush()
	}
	if e == nil {
		out, e = execute(o)
	}
	code := 0
	if e != nil {
		code = 1
		var f *failure
		if errors.As(e, &f) {
			code = f.code
		}
		if !o.json && !((o.command == "idle" || o.command == "wait") && out.result != nil) {
			fmt.Fprintln(stderr, e)
		}
	}
	if o.json {
		envelope := map[string]any{"ok": e == nil, "exit_code": code}
		if e != nil {
			envelope["error"] = e.Error()
		}
		if e == nil || (o.command == "holds" || o.command == "idle" || o.command == "wait") && out.result != nil {
			envelope["data"] = out.result
		}
		if out.description != "" {
			envelope["description"] = out.description
		}
		if out.usefulWorkers != nil {
			envelope["useful_workers"] = *out.usefulWorkers
		}
		if out.source != "" {
			envelope["catalog"] = out.source
		}
		_ = json.NewEncoder(stdout).Encode(envelope)
		return code
	}
	if e != nil && !((o.command == "idle" || o.command == "wait") && out.result != nil) {
		return code
	}
	if out.source == "local" && o.command != "init" {
		fmt.Fprintln(stderr, "Note: using the local "+catalogPath+"; it is not on origin's default branch yet.")
	}
	if printBoard(stdout, out.result) {
		return code
	}
	switch v := out.result.(type) {
	case nil:
		return code
	case Todo:
		printTodo(stdout, v)
	case Shape:
		fmt.Fprintln(stdout, shapeText(v))
	case ResourceReport:
		fmt.Fprintln(stdout, resourceText(v))
	case WorkerList:
		fmt.Fprintln(stdout, workersText(v))
	case TaskList:
		if out.usefulWorkers != nil {
			fmt.Fprintf(stdout, "Useful workers now: %d\n", *out.usefulWorkers)
		}
		printTasks(stdout, v)
	case Summary:
		printSummary(stdout, v)
	case IdleAdvice:
		fmt.Fprintf(stdout, "%s: %s\n", v.Action, v.Reason)
	case Statistics:
		printStats(stdout, v)
	case Holdings:
		fmt.Fprintln(stdout, v.text())
	case string:
		fmt.Fprintln(stdout, v)
	case View:
		fmt.Fprintln(stdout, renderView(v))
		if v.LastWrite != "" {
			fmt.Fprintf(stdout, "  last write: %s · %s ago\n", v.LastWrite, v.Idle)
		}
		if v.ReviewStatus != "" {
			fmt.Fprintln(stdout, "  review:", v.ReviewStatus)
		}
		for i, step := range v.Steps {
			tick := " "
			if slices.Contains(v.CompletedSteps, step.key()) {
				tick = "x"
			}
			fmt.Fprintf(stdout, "  %d. [%s] %s\n", i+1, tick, displayText(step.Text))
		}
		if len(v.Needs) > 0 {
			fmt.Fprintf(stdout, "  needs: %s\n", strings.Join(v.Needs, ", "))
		}
		if len(v.Unlocks) > 0 {
			fmt.Fprintf(stdout, "  unlocks: %s\n", strings.Join(v.Unlocks, ", "))
		}
		if v.Ref != "" {
			fmt.Fprintln(stdout, "  ref:", v.Ref)
		}
		if v.Section != "" {
			fmt.Fprintln(stdout, v.Section)
		}
		if len(v.UntickedSteps) > 0 && o.command == "done" {
			fmt.Fprintln(stderr, "Warning: unticked steps:", strings.Join(v.UntickedSteps, "; "))
		}
		if v.StepChanges != nil {
			fmt.Fprintf(stdout, "  steps changed: added %v; removed %v; reopened %v\n", v.StepChanges.Added, v.StepChanges.Removed, v.StepChanges.Reopened)
		}
		if v.Warning != "" {
			fmt.Fprintln(stderr, "Warning:", v.Warning)
		}
		if v.Unchanged {
			fmt.Fprintln(stdout, "  (already so; nothing changed)")
		}
	case []View:
		if len(v) == 0 {
			fmt.Fprintln(stdout, "No tasks to show. Run bw tasks to see everything.")
		}
		if len(v) > 0 {
			printViews(stdout, v)
		}
	default:
		data, _ := marshal(v)
		fmt.Fprintln(stdout, string(data))
	}
	return code
}

// printViews shares the width-aware formatter with task and statistics lists.
func printViews(w io.Writer, rows []View) {
	cells := [][]string{}
	for _, row := range rows {
		status, worker, age, idle := activityLabel(row), row.Holder, row.Age, row.Idle
		if row.Stale {
			status += " (stale)"
		}
		if worker == "" {
			worker = "-"
		}
		if age == "" {
			age = "-"
		}
		if idle == "" {
			idle = "-"
		}
		// User-supplied text must stay within one row and column.
		worker = strings.Join(strings.Fields(worker), " ")
		title := strings.Join(strings.Fields(row.Title), " ")
		title += metadataText(row)
		cells = append(cells, []string{row.ID, status, worker, age, idle, title})
	}
	printTable(w, []string{"TASK", "STATUS", "WORKER", "AGE", "IDLE", "TITLE"}, cells)
}

func renderView(v View) string {
	state := activityLabel(v)
	if state == "ready" {
		state = "ready, free"
	}
	if v.Holder != "" {
		state += " by " + v.Holder + " (" + v.Age + " ago"
		if v.Idle != "" && v.Idle != v.Age {
			state += ", idle " + v.Idle
		}
		state += ")"
	}
	if v.Stale {
		state += " STALE"
	}
	metadata := metadataText(v)
	return fmt.Sprintf("%s  [%s]\n  title: %s\n  priority: %d", clean(v.ID), clean(state), clean(v.Title), v.Priority) + metadata
}

const exampleTasks = `{
  "stale_hours": 4,
  "tasks": [
    {"id": "first-task", "title": "Replace this with your first task"}
  ]
}
`

// identity resolves --as, then BW_AS, then git user.name. Scripts and agents
// (--json) must be explicit: a shared user.name would let two agents act as one.
func identity(o options, root string) (string, error) {
	who := strings.TrimSpace(o.as)
	if who == "" {
		who = strings.TrimSpace(os.Getenv("BW_AS"))
	}
	if who == "" && o.json {
		return "", fail(1, "--json callers need an explicit, distinct identity. Pass --as <name> or set BW_AS.")
	}
	if who == "" {
		name, _ := git(root, nil, "config", "user.name")
		who = strings.TrimSpace(string(name))
	}
	if who == "" || strings.ContainsAny(who, "\r\n\x00") {
		return "", fail(1, "Set your identity with --as <name>, BW_AS, or git config user.name.")
	}
	return who, nil
}

func findView(c Catalog, s State, id string) (View, bool) {
	for _, v := range c.views(s, time.Now().UTC()) {
		if v.ID == id {
			return v, true
		}
	}
	return View{}, false
}

func execute(o options) (outcome, error) {
	if o.command == "help" || o.command == "help-2" || o.help {
		topic := o.command
		if o.command == "help" && len(o.args) > 0 && !o.help {
			topic = o.args[0]
			if topic == "q" {
				topic = "queue"
			}
		}

		text, ok := helpText[topic]
		if !ok {
			return outcome{}, fail(1, "Unknown help topic %q. Run bw help.", topic)
		}
		if o.markdown {
			if topic == "help" {
				return outcome{result: readme()}, nil
			}
			heading := "Bullwhip " + topic
			if topic == "agents" {
				heading = "Bullwhip agent workflow"
			}
			return outcome{result: "## " + heading + "\n\n" + text}, nil
		}
		return outcome{result: text}, nil
	}
	if o.command == "guide" {
		return guide(o)
	}
	if o.command == "completion" {
		return completion(o)
	}
	if o.command == "_complete" {
		return outcome{result: completeWords(o.args)}, nil
	}
	if o.command == "version" {
		return outcome{result: buildVersion()}, nil
	}
	root, e := repoRoot()
	if e != nil {
		return outcome{}, e
	}
	if o.command == "web" {
		if o.json {
			who, err := identity(o, root)
			if err != nil {
				return outcome{}, err
			}
			data, err := readWeb(context.Background(), root, who)
			return outcome{result: data, source: data.Source}, err
		}
		return outcome{}, runWeb(root, o)
	}
	if o.command == "tui" {
		data, err := readMonitor(context.Background(), root)
		return outcome{result: data, source: data.Source}, err
	}
	if o.command == "wait" {
		return executeWait(o, root)
	}
	if o.command == "holds" {
		return executeHolds(o, root)
	}
	if boardCommand(o.command) {
		return executeBoard(o, root)
	}
	if slices.Contains([]string{"tasks", "summary", "idle", "stats", "todo", "shape", "doctor", "workers"}, o.command) {
		return executeRead(o, root)
	}
	path := filepath.Join(root, catalogPath)
	switch o.command {
	case "init":
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			return outcome{}, fail(1, "Cannot create .bullwhip: %v", e)
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			_, err = io.WriteString(f, exampleTasks)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		}
		if err != nil && !errors.Is(err, os.ErrExist) {
			return outcome{}, fail(1, "Cannot create task file: %v", err)
		}
	case "check":
		// check validates the local file: the change you are about to merge.
		data, e := os.ReadFile(path)
		if e != nil {
			return outcome{}, fail(1, "Cannot read %s. Run bw init, or check file permissions.", catalogPath)
		}
		if _, e = parseCatalog(data); e != nil {
			return outcome{}, fail(1, "Invalid %s: %v. Edit the file, then run bw check.", catalogPath, e)
		}
		return outcome{result: "Task file is valid."}, nil
	}
	who := ""
	if o.command == "claim" || o.command == "release" || o.command == "done" || o.command == "heartbeat" || o.command == "step" {
		if who, e = identity(o, root); e != nil {
			return outcome{}, e
		}
	}
	s, e := openStore(root)
	if e != nil {
		return outcome{}, e
	}
	defer os.RemoveAll(s.dir)
	if o.command == "queue" || o.command == "show" || o.command == "status" {
		snap, e := s.load()
		if e != nil {
			return outcome{}, e
		}
		out := snapshotOutcome(snap)
		if snap.tip == "" {
			return out, fail(1, "State is not initialized. Run bw init.")
		}
		out.result, e = list(o, snap)
		out.result = addLinks(resolveRefs(out.result, root), snap.catalog, root)
		return out, e
	}
	o.root = root
	source := ""
	var usedCatalog Catalog
	result, e := s.update(o.command == "init", func(snap snapshot) (any, bool, error) {
		source = snap.source
		usedCatalog = snap.catalog
		if o.command == "init" {
			if snap.tip != "" {
				return "Already initialized; existing tasks and " + stateRef + " were kept.", false, nil
			}
			return "Initialized " + catalogPath + " and " + stateRef + ". Commit and merge the task file to share it.", true, nil
		}
		return mutate(o, who, snap)
	})
	return outcome{result: addLinks(resolveRefs(result, root), usedCatalog, root), source: source}, e
}

func list(o options, snap snapshot) (any, error) {
	catalog, state, now := snap.catalog, snap.state, time.Now().UTC()
	rows := []View{}
	for _, v := range catalog.views(state, now) {
		if o.command == "show" {
			if v.ID == o.args[0] {
				return v, nil
			}
			continue
		}
		include := o.all || v.State == "ready" && taskAllowed(o, v.Task)
		if o.command == "queue" && include || o.command == "status" && v.State == "claimed" {
			rows = append(rows, v)
		}
	}
	if o.command == "show" {
		return nil, fail(1, "Unknown task %q. Run bw tasks.", o.args[0])
	}
	// Removed tasks still hold their claims until explicitly released.
	if o.command == "status" {
		known := map[string]bool{}
		for _, t := range catalog.Tasks {
			known[t.ID] = true
		}
		for id, m := range state.Tasks {
			if !known[id] && !m.Done {
				rows = append(rows, View{Task: Task{ID: id, Title: "(removed from tasks.json)"}, State: "claimed", Holder: m.Holder, Since: &m.Since, Age: age(now.Sub(m.Since)), Idle: age(now.Sub(m.active())), Stale: catalog.stale(m.active(), now), Unlocks: []string{}})
			}
		}
	}
	return rows, nil
}

// mutate applies claim, release, done or reopen to a fresh snapshot. Repeating a
// command whose outcome already holds succeeds without a write (Unchanged), so
// retrying after a timeout is safe.
func mutate(o options, who string, snap snapshot) (any, bool, error) {
	catalog, state := snap.catalog, snap.state
	now := time.Now().UTC()
	if o.command == "heartbeat" {
		if !state.touch(who, now) {
			return nil, false, fail(3, "%s holds no open claims. If you were working on one, it was released: stop, run bw notes --task <id>, and do not mark that work done.", who)
		}
		held := []View{}
		for _, v := range catalog.views(state, now) {
			if v.State == "claimed" && v.Holder == who {
				held = append(held, v)
			}
		}
		return held, true, nil
	}
	var task View
	found := false
	if o.next {
		// An agent that lost the reply to its last claim gets that task back
		// instead of silently taking a second one.
		views := catalog.views(state, time.Now().UTC())
		for _, v := range views {
			if v.State == "claimed" && v.Holder == who {
				v.Unchanged = true
				return v, false, nil
			}
		}
		for _, v := range views {
			if v.State == "ready" && taskAllowed(o, v.Task) {
				task, found = v, true
				break
			}
		}
		if !found {
			return nil, false, fail(3, "No free ready task. Run bw tasks to see dependencies and holders.")
		}
	} else if task, found = findView(catalog, state, o.args[0]); !found {
		// Allow cleanup when the task was removed from the catalog.
		m, ok := state.Tasks[o.args[0]]
		if !ok || o.command != "release" && o.command != "reopen" {
			return nil, false, fail(1, "Unknown task %q. Run bw tasks.", o.args[0])
		}
		task = View{Task: Task{ID: o.args[0]}, State: "claimed", Holder: m.Holder, Idle: age(now.Sub(m.active())), Stale: catalog.stale(m.active(), now), Unlocks: []string{}}
		if m.Done {
			task.State = "done"
		}
	}
	id := task.ID
	unchanged := func() (any, bool, error) {
		task.Unchanged = true
		return task, false, nil
	}
	mine := task.Holder == who
	switch o.command {
	case "step":
		return tickStep(o, who, snap, task, now)
	case "claim":
		if task.State == "claimed" && mine {
			return unchanged()
		}
		if task.State == "claimed" {
			return nil, false, fail(2, "Task %s is claimed by %s (%s ago). Run bw queue for other ready work.", id, task.Holder, task.Age)
		}
		if task.State != "ready" {
			return nil, false, fail(2, "Task %s is %s. Run bw show %s for its holder and dependencies, or bw queue for other work.", id, task.State, id)
		}
		if e := guardClaim(catalog, state, o.root); e != nil {
			return nil, false, e
		}
		roles, _ := workerRoles(o.role)
		state.Tasks[id] = Mark{Holder: who, Since: now, Seen: now, Roles: roles, Effort: o.effort}
	case "done":
		if task.State == "done" && mine {
			return unchanged()
		}
		if task.State != "claimed" || !mine {
			holder := ""
			if task.Holder != "" {
				holder = " by " + task.Holder
			}
			return nil, false, fail(2, "Task %s is %s%s. Only your own claimed task can be marked done; run bw show %s.", id, task.State, holder, id)
		}
		m := state.Tasks[id]
		m.Done = true
		state.Tasks[id] = m
	case "release":
		if task.State == "ready" || task.State == "waiting" {
			return unchanged()
		}
		if task.State != "claimed" {
			return nil, false, fail(2, "Task %s is %s. Only a claimed task can be released; run bw show %s.", id, task.State, id)
		}
		if !mine {
			if !o.force {
				return nil, false, fail(2, "Task %s is claimed by %s. Only its holder can release it. A coordinator, or anyone once the holder has been inactive for %dh, can run: bw release %s --force --reason \"why\"", id, task.Holder, catalog.staleHours(), id)
			}
			if strings.TrimSpace(o.reason) == "" {
				return nil, false, fail(1, "Releasing another holder's claim needs --reason \"why\"; it is recorded as a note.")
			}
			if !task.Stale && !catalog.coordinator(who) {
				return nil, false, fail(2, "Task %s is claimed by %s, active %s ago. --force needs %dh without holder activity, or a coordinator listed in tasks.json.", id, task.Holder, task.Idle, catalog.staleHours())
			}
			appendEvent(state.Files, "notes.jsonl", Event{TS: now, Actor: who, Event: "noted", Task: id, Note: "Released " + task.Holder + "'s claim: " + strings.TrimSpace(o.reason)})
		}
		delete(state.Tasks, id)
	case "reopen":
		if task.State == "ready" || task.State == "waiting" {
			return unchanged()
		}
		if task.State != "done" {
			return nil, false, fail(2, "Task %s is %s. Only a done task can be reopened; run bw show %s.", id, task.State, id)
		}
		delete(state.Tasks, id)
	}
	reconcileSteps(state, catalog)
	state.touch(who, now)
	if v, ok := findView(catalog, state, id); ok {
		return v, true, nil
	}
	return View{Task: Task{ID: id, Title: "(removed from tasks.json)"}, State: "released", Unlocks: []string{}}, true, nil
}

func snapshotOutcome(snap snapshot) outcome {
	useful := 0
	for _, t := range snap.catalog.Tasks {
		p := phase(t, snap.state)
		if p == "ready" || p == "claimed" {
			useful++
		}
	}
	return outcome{source: snap.source, description: snap.catalog.Description, usefulWorkers: &useful}
}

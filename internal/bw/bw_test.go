package bw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var binary string

func TestMain(m *testing.M) {
	if os.Getenv("BW_TEST_CHILD") != "" {
		TestProcessBarrier(nil)
	}
	dir, e := os.MkdirTemp("", "bw-test-bin-*")
	if e != nil {
		panic(e)
	}
	binary = filepath.Join(dir, "bw")
	// The subprocesses are instrumented too when running the race suite.
	args := []string{"build", "-o", binary, "../../cmd/bw"}
	if raceEnabled {
		args = append([]string{"build", "-race", "-o", binary}, "../../cmd/bw")
	}
	cmd := exec.Command("go", args...)
	if out, e := cmd.CombinedOutput(); e != nil {
		fmt.Fprintln(os.Stderr, string(out))
		os.RemoveAll(dir)
		os.Exit(1)
	}
	// Keep ordinary coordination tests independent of host capacity. Resource
	// guard tests explicitly enable the guard against disposable repositories.
	_ = os.Setenv("BW_RESOURCE_GUARD", "off")
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, e := git(dir, nil, args...)
	if e != nil {
		t.Fatalf("git %v: %v", args, e)
	}
	return strings.TrimSpace(string(out))
}

func fixture(t *testing.T, tasks string) string {
	t.Helper()
	base := t.TempDir()
	remote := filepath.Join(base, "origin.git")
	root := filepath.Join(base, "work")
	mustGit(t, base, "init", "--bare", "--quiet", remote)
	mustGit(t, base, "init", "--quiet", root)
	mustGit(t, root, "remote", "add", "origin", remote)
	mustGit(t, root, "config", "user.name", "git-fallback")
	invoke(t, root, 0, "init")
	writeTasks(t, root, tasks)
	return root
}

func writeTasks(t *testing.T, root, data string) {
	t.Helper()
	if e := os.WriteFile(filepath.Join(root, ".bullwhip", "tasks.json"), []byte(data), 0644); e != nil {
		t.Fatal(e)
	}
}

type response struct {
	OK      bool            `json:"ok"`
	Code    int             `json:"exit_code"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
	Catalog string          `json:"catalog"`
}

func command(root string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, append(args, "--json")...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "BW_AS=", "GORACE=atexit_sleep_ms=0")
	return cmd
}
func outputCode(out []byte, e error) (response, int, error) {
	code := 0
	if e != nil {
		x, ok := e.(*exec.ExitError)
		if !ok {
			return response{}, 0, e
		}
		code = x.ExitCode()
	}
	var r response
	d := json.NewDecoder(bytes.NewReader(out))
	if e = d.Decode(&r); e != nil {
		return r, code, fmt.Errorf("decode %q: %w", out, e)
	}
	if e = d.Decode(new(any)); e != io.EOF {
		return r, code, fmt.Errorf("extra stdout: %s", out)
	}
	if r.Code != code || r.OK != (code == 0) {
		return r, code, fmt.Errorf("wrong envelope: %s", out)
	}
	return r, code, nil
}
func invoke(t *testing.T, root string, want int, args ...string) response {
	t.Helper()
	cmd := command(root, args...)
	out, e := cmd.Output()
	r, code, e := outputCode(out, e)
	if e != nil || code != want {
		t.Fatalf("bw %v: code %d want %d, error %v, output %s", args, code, want, e, out)
	}
	return r
}
func views(t *testing.T, r response) []View {
	t.Helper()
	var v []View
	if e := json.Unmarshal(r.Data, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func view(t *testing.T, r response) View {
	t.Helper()
	var v View
	if e := json.Unmarshal(r.Data, &v); e != nil {
		t.Fatal(e)
	}
	return v
}

const graph = `{"tasks":[{"id":"a","title":"A","priority":10},{"id":"b","title":"B","needs":["a"]},{"id":"c","title":"C"}]}`

func TestWorkflowFromNestedDirectory(t *testing.T) {
	root := fixture(t, graph)
	nested := filepath.Join(root, "src", "deep")
	os.MkdirAll(nested, 0755)
	invoke(t, nested, 0, "init")
	invoke(t, nested, 0, "check")
	q := views(t, invoke(t, nested, 0, "queue"))
	if len(q) != 2 || q[0].ID != "a" {
		t.Fatal(q)
	}
	invoke(t, nested, 2, "claim", "b", "--as", "sam")
	a := view(t, invoke(t, nested, 0, "claim", "--next", "--as", "sam"))
	if a.ID != "a" || a.Holder != "sam" {
		t.Fatal(a)
	}
	invoke(t, nested, 2, "done", "a", "--as", "other")
	invoke(t, nested, 2, "release", "a", "--as", "other", "--force", "--reason", "too soon")
	invoke(t, nested, 0, "done", "a", "--as", "sam")
	b := view(t, invoke(t, nested, 0, "show", "b"))
	if b.State != "ready" {
		t.Fatal(b)
	}
	invoke(t, nested, 0, "claim", "b", "--as", "sam")
	invoke(t, nested, 0, "release", "b", "--as", "sam")
	invoke(t, nested, 0, "reopen", "a")
	if v := view(t, invoke(t, nested, 0, "show", "b")); v.State != "waiting" {
		t.Fatal(v)
	}
	invoke(t, nested, 0, "claim", "a", "--as", "sam")
	invoke(t, nested, 0, "claim", "c", "--as", "other")
	invoke(t, nested, 3, "claim", "--next", "--as", "third")
	if len(views(t, invoke(t, nested, 0, "status"))) != 2 {
		t.Fatal("missing claims")
	}
	// Local discovery never touches the index, branch or worktree files.
	if got := mustGit(t, root, "status", "--porcelain"); got != "?? .bullwhip/" {
		t.Fatal(got)
	}
}

func TestTaskValidation(t *testing.T) {
	cases := []string{
		`{"tasks":[{"id":"a","title":"A","needs":["missing"]}]}`,
		`{"tasks":[{"id":"a","title":"A","needs":["b"]},{"id":"b","title":"B","needs":["a"]}]}`,
		`{"tasks":[{"id":"a","title":"A"},{"id":"a","title":"A"}]}`,
		`{"tasks":[],"tasks":[]}`, `{"tasks":null}`, `{"tasks":[],"extra":1}`,
		`{"tasks":[{"id":"a","title":"A","priority":1.5}]}`, `{"stale_hours":-1,"tasks":[]}`,
		`{"tasks":[{"id":"../x","title":"bad"}]}`, `{"tasks":[]} {}`,
	}
	root := fixture(t, graph)
	for _, data := range cases {
		writeTasks(t, root, data)
		invoke(t, root, 1, "check")
	}
	writeTasks(t, root, `{"tasks":[]}`)
	invoke(t, root, 0, "check")
}

// Each independent process waits on its own pipe until every process has
// started. Real Git serializes their pushes to the same disposable bare remote.
func raceCommands(t *testing.T, root string, arguments [][]string) []response {
	t.Helper()
	start := make([]*os.File, len(arguments))
	cmds := make([]*exec.Cmd, len(arguments))
	outs := make([]bytes.Buffer, len(arguments))
	for i, args := range arguments {
		r, w, e := os.Pipe()
		if e != nil {
			t.Fatal(e)
		}
		start[i] = w
		// This test helper is a process barrier; bw itself has no test-only behavior.
		cmd := exec.Command(os.Args[0], "-test.run=^TestProcessBarrier$")
		cmd.Dir = root
		encoded, _ := json.Marshal(append(args, "--json"))
		cmd.Env = append(os.Environ(), "BW_TEST_CHILD="+binary, "BW_TEST_ARGS="+string(encoded), "BW_AS=", "GORACE=atexit_sleep_ms=0")
		cmd.Stdin = r
		cmd.Stdout = &outs[i]
		cmd.Stderr = os.Stderr
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		r.Close()
		cmds[i] = cmd
	}
	for _, w := range start {
		w.Close()
	}
	results := make([]response, len(cmds))
	var wg sync.WaitGroup
	for i, cmd := range cmds {
		wg.Add(1)
		go func(i int, cmd *exec.Cmd) {
			defer wg.Done()
			e := cmd.Wait()
			r, _, err := outputCode(outs[i].Bytes(), e)
			if err != nil {
				t.Errorf("process %d: %v", i, err)
			}
			results[i] = r
		}(i, cmd)
	}
	wg.Wait()
	return results
}

func TestProcessBarrier(t *testing.T) {
	bin := os.Getenv("BW_TEST_CHILD")
	if bin == "" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	var args []string
	if e := json.Unmarshal([]byte(os.Getenv("BW_TEST_ARGS")), &args); e != nil {
		os.Exit(99)
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	e := cmd.Run()
	if e != nil {
		if x, ok := e.(*exec.ExitError); ok {
			os.Exit(x.ExitCode())
		}
		os.Exit(99)
	}
	os.Exit(0)
}

func TestManyProcessesOneHolder(t *testing.T) {
	root := fixture(t, `{"tasks":[{"id":"a","title":"A"}]}`)
	for round := 0; round < 5; round++ {
		args := make([][]string, 24)
		for i := range args {
			if i%2 == 0 {
				args[i] = []string{"claim", "--next", "--as", fmt.Sprintf("worker-%d", i)}
			} else {
				args[i] = []string{"crack", "--as", fmt.Sprintf("worker-%d", i)}
			}
		}
		result := raceCommands(t, root, args)
		wins := 0
		holder := ""
		for _, r := range result {
			if r.Code == 0 {
				wins++
				holder = view(t, r).Holder
			} else if r.Code != 3 {
				t.Fatalf("unexpected result: %+v", r)
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: got %d winners", round, wins)
		}
		got := view(t, invoke(t, root, 0, "show", "a"))
		if got.Holder != holder {
			t.Fatalf("holder changed: %+v", got)
		}
		invoke(t, root, 0, "release", "a", "--as", holder)
	}
}

func TestDisjointClaimsPreserved(t *testing.T) {
	root := fixture(t, `{"tasks":[]}`)
	c := Catalog{Tasks: []Task{}}
	args := [][]string{}
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("task-%02d", i)
		c.Tasks = append(c.Tasks, Task{ID: id, Title: id})
		args = append(args, []string{"claim", id, "--as", id})
	}
	data, _ := marshal(c)
	writeTasks(t, root, string(data))
	for _, r := range raceCommands(t, root, args) {
		if r.Code != 0 {
			t.Fatalf("lost disjoint update: %+v", r)
		}
	}
	if got := views(t, invoke(t, root, 0, "status")); len(got) != 16 {
		t.Fatal(got)
	}
}

// idle backdates a claim's start and last activity.
func idle(t *testing.T, root, id string, d time.Duration) {
	t.Helper()
	s, e := openStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(s.dir)
	_, e = s.update(false, func(snap snapshot) (any, bool, error) {
		m := snap.state.Tasks[id]
		m.Since = time.Now().Add(-d)
		m.Seen = m.Since
		snap.state.Tasks[id] = m
		return nil, true, nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

func TestStaleClaimsAndRemovedTasks(t *testing.T) {
	root := fixture(t, graph)
	invoke(t, root, 0, "claim", "a", "--as", "old")
	idle(t, root, "a", 5*time.Hour)
	if v := views(t, invoke(t, root, 0, "status")); len(v) != 1 || !v[0].Stale || v[0].Idle != "5h" {
		t.Fatal(v)
	}
	invoke(t, root, 2, "claim", "a", "--as", "new")
	invoke(t, root, 2, "release", "a", "--as", "new")
	invoke(t, root, 1, "release", "a", "--as", "new", "--force")
	writeTasks(t, root, `{"tasks":[]}`)
	if len(views(t, invoke(t, root, 0, "status"))) != 1 {
		t.Fatal("removed claim hidden")
	}
	invoke(t, root, 0, "release", "a", "--as", "new", "--force", "--reason", "old stopped working")
	notes := invoke(t, root, 0, "notes", "--task", "a")
	if !strings.Contains(string(notes.Data), "Released old's claim: old stopped working") {
		t.Fatal(string(notes.Data))
	}
}

func TestActivityKeepsClaimsAndCoordinatorsRelease(t *testing.T) {
	root := fixture(t, graph)
	invoke(t, root, 0, "claim", "a", "--as", "worker")
	idle(t, root, "a", 5*time.Hour)
	// A heartbeat makes the claim active again; nobody may force it.
	invoke(t, root, 0, "heartbeat", "--as", "worker")
	if v := view(t, invoke(t, root, 0, "show", "a")); v.Stale {
		t.Fatal(v)
	}
	invoke(t, root, 2, "release", "a", "--as", "other", "--force", "--reason", "looks idle")
	// Board activity counts as well.
	idle(t, root, "a", 5*time.Hour)
	invoke(t, root, 0, "note", "a", "still", "going", "--as", "worker")
	if v := view(t, invoke(t, root, 0, "show", "a")); v.Stale {
		t.Fatal(v)
	}
	invoke(t, root, 3, "heartbeat", "--as", "nobody")
	invoke(t, root, 1, "status", "--reason", "x")
	// A coordinator may release an active claim, with a reason.
	writeTasks(t, root, `{"coordinators":["lead"],"tasks":[{"id":"a","title":"A"},{"id":"b","title":"B","needs":["a"]}]}`)
	invoke(t, root, 1, "release", "a", "--as", "lead", "--force")
	invoke(t, root, 0, "release", "a", "--as", "lead", "--force", "--reason", "agent crashed")
	if v := view(t, invoke(t, root, 0, "show", "a")); v.State != "ready" {
		t.Fatal(v)
	}
	writeTasks(t, root, `{"coordinators":[" lead"],"tasks":[]}`)
	invoke(t, root, 1, "check")
}

func TestHelpAndReadme(t *testing.T) {
	for topic, text := range helpText {
		args := []string{topic, "--help"}
		if topic == "format" || topic == "agents" || topic == "setup" || topic == "planning" {
			args = []string{"help", topic}
		}
		r := invoke(t, t.TempDir(), 0, args...)
		if len(r.Data) == 0 {
			t.Fatal(topic)
		}
		if !strings.Contains(text, "Example:") && topic != "agents" && topic != "help" {
			t.Errorf("no example for %s", topic)
		}
		if topic != "help" && !strings.Contains(text, "Exit codes:") && !strings.Contains(text, "Exits:") {
			t.Errorf("no exit codes for %s", topic)
		}
	}
	var b bytes.Buffer
	if code := Run([]string{"help", "agents", "--markdown"}, &b, io.Discard); code != 0 || !strings.HasPrefix(b.String(), "## Bullwhip agent workflow") {
		t.Fatal(b.String())
	}
	data, e := os.ReadFile("../../README.md")
	if e != nil || string(data) != readme()+"\n" {
		t.Fatal("README drift: run scripts/docs.sh", e)
	}
}

func TestErrorsIdentityAndOfflineCheck(t *testing.T) {
	root := fixture(t, graph)
	invoke(t, root, 1, "claim")
	invoke(t, root, 1, "claim", "a", "--next")
	invoke(t, root, 1, "done", "a", "--force")
	invoke(t, root, 1, "unknown")
	invoke(t, root, 0, "tasks")
	invoke(t, root, 1, "agents")
	invoke(t, root, 1, "show", "absent")
	invoke(t, root, 1, "queue", "--project-id", "old")
	// Scripts and agents must name themselves; git user.name is interactive-only.
	invoke(t, root, 1, "claim", "a")
	text := exec.Command(binary, "claim", "a")
	text.Dir = root
	text.Env = append(os.Environ(), "BW_AS=")
	if out, e := text.CombinedOutput(); e != nil || !strings.Contains(string(out), "by git-fallback") {
		t.Fatalf("git fallback: %v %s", e, out)
	}
	invoke(t, root, 0, "release", "a", "--as", "git-fallback")
	if v := view(t, invoke(t, root, 0, "claim", "a", "--as", "  padded ")); v.Holder != "padded" {
		t.Fatal("identity not trimmed", v)
	}
	invoke(t, root, 0, "done", "a", "--as", "padded")
	cmd := command(root, "claim", "c")
	cmd.Env = append(cmd.Env, "BW_AS=environment")
	out, e := cmd.Output()
	r, _, err := outputCode(out, e)
	if err != nil || view(t, r).Holder != "environment" {
		t.Fatal(r, err)
	}
	mustGit(t, root, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	invoke(t, root, 0, "check")
	invoke(t, root, 4, "queue")
	invoke(t, t.TempDir(), 1, "status")
}

func TestPushAcceptedReplyLost(t *testing.T) {
	root := fixture(t, graph)
	realGit, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	// All Git commands are real. Only a successful push's acknowledgement is lost.
	script := "#!/bin/sh\nlost=0\nfor arg do [ \"$arg\" != push ] || lost=1; done\n'" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\"\nresult=$?\nif [ \"$result\" = 0 ] && [ \"$lost\" = 1 ]; then exit 1; fi\nexit \"$result\"\n"
	if e = os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0755); e != nil {
		t.Fatal(e)
	}
	cmd := command(root, "claim", "a", "--as", "sam")
	cmd.Env = append(cmd.Env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, e := cmd.Output()
	r, code, err := outputCode(out, e)
	if err != nil || code != 0 || view(t, r).Holder != "sam" {
		t.Fatalf("lost ack not recovered: %s %v", out, err)
	}
	invoke(t, root, 2, "claim", "a", "--as", "other")
}

func TestConcurrentInitialization(t *testing.T) {
	root := fixture(t, graph)
	// Start with a repository/task catalog but no state ref.
	mustGit(t, root, "push", "origin", ":"+stateRef)
	args := make([][]string, 12)
	for i := range args {
		args[i] = []string{"init"}
	}
	for _, r := range raceCommands(t, root, args) {
		if r.Code != 0 {
			t.Fatalf("init race: %+v", r)
		}
	}
	invoke(t, root, 0, "claim", "a", "--as", "sam")
	invoke(t, root, 0, "init")
	if got := view(t, invoke(t, root, 0, "show", "a")); got.Holder != "sam" {
		t.Fatal("init cleared existing state", got)
	}
}

func TestMultipleDependenciesAndPriority(t *testing.T) {
	root := fixture(t, `{"stale_hours":1,"tasks":[{"id":"z","title":"Z","priority":1},{"id":"a","title":"A","priority":1},{"id":"join","title":"Join","needs":["a","z"],"priority":100}]}`)
	if got := view(t, invoke(t, root, 0, "claim", "--next", "--as", "sam")); got.ID != "a" {
		t.Fatal("priority tie order", got)
	}
	invoke(t, root, 0, "done", "a", "--as", "sam")
	if got := view(t, invoke(t, root, 0, "show", "join")); got.State != "waiting" {
		t.Fatal(got)
	}
	invoke(t, root, 0, "claim", "z", "--as", "sam")
	invoke(t, root, 0, "done", "z", "--as", "sam")
	if got := view(t, invoke(t, root, 0, "claim", "--next", "--as", "sam")); got.ID != "join" {
		t.Fatal(got)
	}
	// A manual reopen does not revoke existing descendant ownership.
	invoke(t, root, 0, "reopen", "a")
	if got := view(t, invoke(t, root, 0, "show", "join")); got.State != "claimed" {
		t.Fatal(got)
	}
}

func TestInvalidStateRefFailsClosed(t *testing.T) {
	root := fixture(t, graph)
	s, e := openStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(s.dir)
	snap, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	// Publish a syntactically valid but semantically invalid mark on the test ref.
	candidate, e := s.commit(State{Tasks: map[string]Mark{"a": {}}}, snap.tip, strings.Repeat("0", 32))
	if e != nil {
		t.Fatal(e)
	}
	mustGit(t, s.dir, "push", s.remote, candidate+":"+stateRef)
	invoke(t, root, 1, "init")
	invoke(t, root, 1, "claim", "a", "--as", "sam")
}

func TestNullStateAndLargeStaleThreshold(t *testing.T) {
	var s State
	if e := decode([]byte("null"), &s); e == nil {
		t.Fatal("null state accepted")
	}
	c := Catalog{StaleHours: int(^uint(0) >> 1), Tasks: []Task{{ID: "a", Title: "A"}}}
	if c.stale(time.Now().Add(-48*time.Hour), time.Now()) {
		t.Fatal("threshold overflow permitted stealing")
	}
}

func TestStateValidation(t *testing.T) {
	for _, data := range []string{`{}`, `{"tasks":null}`, `{"tasks":{"a":{}}}`, `{"tasks":{},"unknown":true}`, `{"tasks":{},"tasks":{}}`, `{"tasks":{},"ops":["x"]}`} {
		var s State
		e := decode([]byte(data), &s)
		if e == nil {
			e = s.validate()
		}
		if e == nil {
			t.Fatalf("accepted invalid state %s", data)
		}
	}
}

func TestRetriesAreIdempotent(t *testing.T) {
	root := fixture(t, graph)
	first := view(t, invoke(t, root, 0, "claim", "a", "--as", "sam"))
	again := view(t, invoke(t, root, 0, "claim", "a", "--as", "sam"))
	if first.Unchanged || !again.Unchanged || again.Holder != "sam" {
		t.Fatal(first, again)
	}
	// --next hands back the task already held instead of taking a second one.
	if v := view(t, invoke(t, root, 0, "claim", "--next", "--as", "sam")); v.ID != "a" || !v.Unchanged {
		t.Fatal(v)
	}
	invoke(t, root, 2, "claim", "a", "--as", "other")
	invoke(t, root, 0, "done", "a", "--as", "sam")
	if v := view(t, invoke(t, root, 0, "done", "a", "--as", "sam")); !v.Unchanged {
		t.Fatal(v)
	}
	invoke(t, root, 2, "done", "a", "--as", "other")
	invoke(t, root, 0, "claim", "c", "--as", "sam")
	invoke(t, root, 0, "release", "c", "--as", "sam")
	if v := view(t, invoke(t, root, 0, "release", "c", "--as", "sam")); !v.Unchanged || v.State != "ready" {
		t.Fatal(v)
	}
	invoke(t, root, 0, "reopen", "a")
	if v := view(t, invoke(t, root, 0, "reopen", "a")); !v.Unchanged {
		t.Fatal(v)
	}
}

func TestCatalogFromOriginDefaultBranch(t *testing.T) {
	root := fixture(t, graph)
	if r := invoke(t, root, 0, "queue"); r.Catalog != "local" {
		t.Fatal("expected local catalog before merge", r.Catalog)
	}
	mustGit(t, root, "add", catalogPath)
	mustGit(t, root, "commit", "--quiet", "-m", "tasks")
	mustGit(t, root, "push", "--quiet", "origin", "HEAD")
	// A worker's branch drops a dependency; origin's catalog still governs.
	writeTasks(t, root, `{"tasks":[{"id":"a","title":"A"},{"id":"b","title":"B"}]}`)
	invoke(t, root, 0, "check")
	r := invoke(t, root, 2, "claim", "b", "--as", "sam")
	if !strings.HasPrefix(r.Catalog, "origin@") {
		t.Fatal(r)
	}
	if v := view(t, invoke(t, root, 0, "show", "b")); v.State != "waiting" {
		t.Fatal(v)
	}
	// Only the tip of the state history is fetched, however long it grows.
	for i := 0; i < 3; i++ {
		invoke(t, root, 0, "claim", "c", "--as", "sam")
		invoke(t, root, 0, "release", "c", "--as", "sam")
	}
	s, e := openStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(s.dir)
	if _, e = s.load(); e != nil {
		t.Fatal(e)
	}
	if n := mustGit(t, s.dir, "rev-list", "--count", localState); n != "1" {
		t.Fatal("state history fetched:", n)
	}
	writeTasks(t, root, `{"tasks":[]}`)
	mustGit(t, root, "commit", "--quiet", "-am", "empty")
	mustGit(t, root, "push", "--quiet", "origin", "HEAD")
	if got := views(t, invoke(t, root, 0, "tasks")); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestOpLogBounded(t *testing.T) {
	root := fixture(t, graph)
	s, e := openStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(s.dir)
	snap, e := s.load()
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < maxOps; i++ {
		snap.state.Ops = append(snap.state.Ops, fmt.Sprintf("%032x", i))
	}
	candidate, e := s.commit(snap.state, snap.tip, strings.Repeat("f", 32))
	if e != nil {
		t.Fatal(e)
	}
	var got State
	if e = decode([]byte(mustGit(t, s.dir, "show", candidate+":state.json")), &got); e != nil || got.validate() != nil {
		t.Fatal(e)
	}
	if len(got.Ops) != maxOps || !got.hasOp(strings.Repeat("f", 32)) || got.hasOp(fmt.Sprintf("%032x", 0)) {
		t.Fatal(got.Ops)
	}
}

func TestRemoteSchemes(t *testing.T) {
	root := fixture(t, graph)
	for _, url := range []string{"http://example.invalid/x.git", "git://example.invalid/x.git", "ext::sh -c true"} {
		mustGit(t, root, "remote", "set-url", "origin", url)
		if r := invoke(t, root, 1, "queue"); !strings.Contains(r.Error, "origin") || strings.Contains(r.Error, "example.invalid") {
			t.Fatal(r)
		}
	}
}

func TestHelpers(t *testing.T) {
	if got := redact("fatal: unable to access 'https://user:tok@host/x.git/'"); strings.Contains(got, "tok") || !strings.Contains(got, "https://***@host") {
		t.Fatal(got)
	}
	for d, want := range map[time.Duration]string{-time.Second: "0s", 42 * time.Second: "42s", 17 * time.Minute: "17m", 3*time.Hour + 59*time.Minute: "3h", 72 * time.Hour: "3d"} {
		if got := age(d); got != want {
			t.Errorf("age(%v) = %s, want %s", d, got, want)
		}
	}
	data := []byte(`{"tasks":[{"id":"b","title":"B"},{"id":"a","title":"A"},{"id":"c","title":"C","priority":5}]}`)
	var c Catalog
	if e := decode(data, &c); e != nil || c.validate() != nil || c.Tasks[0].ID != "b" {
		t.Fatal("validate must not reorder", c)
	}
	if p, e := parseCatalog(data); e != nil || p.Tasks[0].ID != "c" || p.Tasks[1].ID != "a" {
		t.Fatal(p, e)
	}
}

func TestCrackAlias(t *testing.T) {
	root := fixture(t, graph)
	invoke(t, root, 1, "crack")
	invoke(t, root, 1, "crack", "a", "--as", "sam")
	first := view(t, invoke(t, root, 0, "crack", "--as", "sam"))
	if first.ID != "a" || first.Holder != "sam" {
		t.Fatal(first)
	}
	again := view(t, invoke(t, root, 0, "claim", "--next", "--as", "sam"))
	if again.ID != first.ID || !again.Unchanged {
		t.Fatal(again)
	}
	if next := view(t, invoke(t, root, 0, "crack", "--as", "other")); next.ID != "c" {
		t.Fatal(next)
	}
	invoke(t, root, 3, "crack", "--as", "third")
}

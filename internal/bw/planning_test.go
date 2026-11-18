package bw

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPlanningFields(t *testing.T) {
	for _, body := range []string{
		`"description":"` + strings.Repeat("x", 201) + `","tasks":[]`,
		`"idle_minutes":-1,"tasks":[]`,
		`"roles":["bad role"],"tasks":[]`,
		`"roles":["a","a"],"tasks":[]`,
		`"roles":[],"tasks":[{"id":"a","title":"A","roles":["frontend"]}]`,
		`"roles":["frontend"],"tasks":[{"id":"a","title":"A","review_roles":["backend"]}]`,
		`"resources":{"min_free_disk_gb":2},"tasks":[]`,
		`"resources":{"max_claims":0},"tasks":[]`,
	} {
		if _, e := parseCatalog([]byte("{" + body + "}")); e == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	c, e := parseCatalog([]byte(`{"description":"A plan","roles":["front","back"],"idle_review":false,"idle_minutes":5,"resources":{"max_claims":2},"tasks":[{"id":"a","title":"A","roles":["front"],"review_roles":["back"]}]}`))
	if e != nil || c.Description != "A plan" {
		t.Fatal(c, e)
	}
	for _, args := range [][]string{{"wait", "--timeout", "61m"}, {"wait", "--timeout", "0s"}, {"wait", "--interval", "9s"}, {"queue", "--interval", "30s"}, {"idle", "--role", "bad role"}} {
		if _, e := parse(args); e == nil {
			t.Fatal(args)
		}
	}
}
func TestShapeAndTodo(t *testing.T) {
	c, e := parseCatalog([]byte(`{"description":"Wide plan","tasks":[{"id":"a","title":"A"},{"id":"b","title":"B"},{"id":"c","title":"C"},{"id":"d","title":"D"},{"id":"final","title":"Final","priority":99,"needs":["a","b","c","d"]}]}`))
	if e != nil {
		t.Fatal(e)
	}
	state := State{Tasks: map[string]Mark{}}
	s := planShape(c, state)
	if s.ChainLength != 2 || s.MostReady != 4 || s.SuggestedCrew != 4 || len(s.FanIn) != 1 || len(s.Warnings) == 0 {
		t.Fatal(s)
	}
	todo := todoRows(options{}, snapshot{catalog: c, state: state}, time.Now())
	if len(todo.Tasks) != 5 || todo.Tasks[4].ID != "final" || todo.Description != "Wide plan" || todo.UsefulWorkers != 4 {
		t.Fatal(todo)
	}
	state.Tasks["a"] = Mark{Done: true}
	s = planShape(c, state)
	if s.Remaining != 4 || s.MostReady != 3 {
		t.Fatal(s)
	}
	for _, t := range c.Tasks {
		state.Tasks[t.ID] = Mark{Done: true}
	}
	s = planShape(c, state)
	if s.Remaining != 0 || s.ChainLength != 0 || len(s.Warnings) != 0 {
		t.Fatal(s)
	}
}
func TestRolesAndReviewAdvice(t *testing.T) {
	root := fixture(t, `{"description":"Role plan","roles":["front","back"],"tasks":[{"id":"front","title":"Front","roles":["front"],"effort":"high","review_roles":["back"]},{"id":"back","title":"Back","roles":["back"],"priority":99},{"id":"follow","title":"Follow","needs":["back"],"roles":["front"]}]}`)
	if rows := views(t, invoke(t, root, 0, "queue", "--role", "front", "--effort", "low")); len(rows) != 0 {
		t.Fatal(rows)
	}
	if rows := views(t, invoke(t, root, 0, "queue", "--all", "--role", "front", "--effort", "low")); len(rows) != 3 {
		t.Fatal(rows)
	}
	if v := view(t, invoke(t, root, 0, "crack", "--as", "f", "--role", "front", "--effort", "high")); v.ID != "front" {
		t.Fatal(v)
	}
	if v := view(t, invoke(t, root, 0, "crack", "--as", "general")); v.ID != "back" {
		t.Fatal(v)
	}
	mine := readData[Todo](t, invoke(t, root, 0, "todo", "--mine", "--as", "f", "--role", "front", "--effort", "high"))
	if len(mine.Tasks) != 2 {
		t.Fatal(mine)
	}
	// Explicit IDs bypass both roles and effort.
	invoke(t, root, 0, "release", "back", "--as", "general")
	invoke(t, root, 0, "claim", "back", "--as", "f2", "--role", "front", "--effort", "low")
	a := readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", "f3", "--role", "front", "--effort", "low"))
	if a.Action != "wait" {
		t.Fatal(a)
	}
	head := commitFixture(t, root)
	invoke(t, root, 0, "review", "request", "front", "--branch", "feat/front", "--head", head, "--as", "f")
	a = readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", "r", "--role", "back", "--effort", "high"))
	if a.Action != "review" || a.Review != "feat--front" {
		t.Fatal(a)
	}
	invoke(t, root, 3, "review", "take", "--as", "wrong", "--role", "front", "--effort", "high")
	invoke(t, root, 0, "review", "take", "--as", "r", "--role", "back", "--effort", "high")
	workers := readData[WorkerList](t, invoke(t, root, 0, "workers", "r"))
	if len(workers) != 1 || workers[0].Activity != "reviewing" || workers[0].Effort != "high" || len(workers[0].Roles) != 1 {
		t.Fatal(workers)
	}
}
func guardEnvs(t *testing.T) {
	t.Helper()
	t.Setenv("BW_RESOURCE_GUARD", "")
	for _, env := range []string{"BW_MIN_FREE_DISK_GB", "BW_MIN_FREE_MEMORY_GB", "BW_CRITICAL_FREE_DISK_GB", "BW_CRITICAL_FREE_MEMORY_GB"} {
		t.Setenv(env, "0")
	}
	t.Setenv("BW_MAX_CLAIMS", "100")
}
func TestResourcesAndConcurrentCapacity(t *testing.T) {
	guardEnvs(t)
	root := fixture(t, `{"tasks":[{"id":"a","title":"A"},{"id":"b","title":"B"}]}`)
	t.Setenv("BW_MAX_CLAIMS", "1")
	results := raceCommands(t, root, [][]string{{"claim", "a", "--as", "a"}, {"claim", "b", "--as", "b"}})
	wins, refused := 0, 0
	holder := ""
	for _, r := range results {
		if r.Code == 0 {
			wins++
			holder = view(t, r).Holder
		}
		if r.Code == 5 {
			refused++
			if !strings.Contains(r.Error, "BW_MAX_CLAIMS") {
				t.Fatal(r)
			}
		}
	}
	if wins != 1 || refused != 1 {
		t.Fatal(results)
	}
	a := readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", "other"))
	if a.Action != "wait" || a.ReasonCode != "low_resources" {
		t.Fatal(a)
	}
	a = readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", holder))
	if a.Action != "work" {
		t.Fatal(a)
	}
	t.Setenv("BW_MIN_FREE_DISK_GB", "1000000000")
	t.Setenv("BW_CRITICAL_FREE_DISK_GB", "1000000000")
	a = readData[IdleAdvice](t, invoke(t, root, 3, "idle", "--as", holder))
	if a.ReasonCode != "critical_resources" {
		t.Fatal(a)
	}
	a = readData[IdleAdvice](t, invoke(t, root, 3, "wait", "--as", holder))
	if a.Action != "stop" {
		t.Fatal(a)
	}
	doctor := readData[ResourceReport](t, invoke(t, root, 0, "doctor"))
	if doctor.Levels["min_free_disk_gb"].Source != "environment" || doctor.DiskGB <= 0 {
		t.Fatal(doctor)
	}
	t.Setenv("BW_RESOURCE_GUARD", "off")
	invoke(t, root, 0, "claim", map[string]string{"a": "b", "b": "a"}[holder], "--as", "other")
}
func TestWaitReadOnlyTimeoutUnlockAndSignal(t *testing.T) {
	root := fixture(t, `{"tasks":[{"id":"a","title":"A"},{"id":"b","title":"B","needs":["a"]}]}`)
	invoke(t, root, 1, "wait", "--timeout", "1ns")
	invoke(t, root, 0, "claim", "a", "--as", "holder")
	before := mustGit(t, root, "ls-remote", "origin", stateRef)
	a := readData[IdleAdvice](t, invoke(t, root, 0, "wait", "--as", "waiting", "--timeout", "300ms"))
	if a.Action != "wait" {
		t.Fatal(a)
	}
	if after := mustGit(t, root, "ls-remote", "origin", stateRef); after != before {
		t.Fatal("wait wrote state")
	}
	// A real wait process observes a competing process unlocking a task.
	cmd := command(root, "wait", "--as", "waiting", "--interval", "10s", "--timeout", "20s")
	var buf strings.Builder
	cmd.Stdout = &buf
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	time.Sleep(300 * time.Millisecond)
	invoke(t, root, 0, "done", "a", "--as", "holder")
	if e := cmd.Wait(); e != nil {
		t.Fatal(e)
	}
	var r response
	if e := json.Unmarshal([]byte(buf.String()), &r); e != nil {
		t.Fatal(e)
	}
	a = readData[IdleAdvice](t, r)
	if a.Action != "claim" {
		t.Fatal(a)
	}
	invoke(t, root, 0, "claim", "b", "--as", "holder")
	cmd = command(root, "wait", "--as", "waiting")
	buf.Reset()
	cmd.Stdout = &buf
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	time.Sleep(300 * time.Millisecond)
	if e := cmd.Process.Signal(os.Interrupt); e != nil {
		t.Fatal(e)
	}
	if e := cmd.Wait(); e != nil {
		t.Fatal(e)
	}
}
func TestWaitCancellationDuringFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, e := waitAdvice(ctx, options{}, t.TempDir(), time.Second)
	if e != nil || out.result.(IdleAdvice).Action != "wait" {
		t.Fatal(out, e)
	}
}
func TestStepDurationAmbiguity(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	steps := []Step{{Text: "one"}, {Text: "two"}}
	cases := []struct {
		name  string
		r     stepRecord
		known bool
	}{
		{"ordered", stepRecord{steps: steps, ticks: map[string]time.Time{"one": start.Add(time.Minute), "two": start.Add(3 * time.Minute)}}, true},
		{"batch", stepRecord{steps: steps, ticks: map[string]time.Time{"one": start.Add(time.Minute), "two": start.Add(90 * time.Second)}}, false},
		{"out of order", stepRecord{steps: steps, ticks: map[string]time.Time{"two": start.Add(time.Minute), "one": start.Add(3 * time.Minute)}}, false},
		{"reticked", stepRecord{steps: steps, ticks: map[string]time.Time{"one": start.Add(time.Minute)}, undone: map[string]bool{"one": true}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, m := range c.r.metrics(start) {
				if m.Known != c.known {
					t.Fatal(m)
				}
			}
		})
	}
}
func TestActivityDerivation(t *testing.T) {
	now := time.Now().UTC()
	c := Catalog{Tasks: []Task{{ID: "a", Title: "A"}}}
	state := State{Tasks: map[string]Mark{"a": {Holder: "w", Since: now, Seen: now}}, Files: map[string][]byte{}}
	check := func(want string, at time.Time) {
		t.Helper()
		if v := c.views(state, at)[0]; v.Activity != want {
			t.Fatalf("%+v want %s", v, want)
		}
	}
	check("claimed", now)
	check("idle", now.Add(11*time.Minute))
	check("stale", now.Add(5*time.Hour))
	m := state.Tasks["a"]
	m.Seen = now.Add(time.Minute)
	state.Tasks["a"] = m
	check("in progress", now.Add(2*time.Minute))
	appendEvent(state.Files, reviewPath("a"), Event{TS: now.Add(time.Minute), Actor: "w", Event: "requested", Task: "a", Branch: "a", Head: "abc"})
	check("in review", now.Add(2*time.Minute))
}
func TestReadmeCommandsCovered(t *testing.T) {
	for _, cmd := range commandOrder {
		if !strings.Contains(readme(), "`bw "+cmd+"`") {
			t.Fatal("missing command", cmd)
		}
	}
}

func TestStepTimingSurvivesCatalogChangesAndUndo(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	r := stepRecord{}
	first, second := start.Add(2*time.Minute), start.Add(4*time.Minute)
	r.apply(TaskEvent{StepCatalog: []Step{{Text: "one"}, {ID: "two", Text: "two"}}, StepTicks: map[string]time.Time{"one": first, "two": second}})
	// Removing the first step retains the second step's true predecessor.
	r.apply(TaskEvent{StepCatalog: []Step{{ID: "two", Text: "Reworded"}}, StepTicks: map[string]time.Time{"two": second}})
	ms := r.metrics(start)
	if len(ms) != 1 || !ms[0].Known || ms[0].Seconds != 120 || ms[0].Text != "Reworded" {
		t.Fatal(ms)
	}
	r.apply(TaskEvent{StepCatalog: r.steps, StepTicks: map[string]time.Time{}})
	r.apply(TaskEvent{StepCatalog: r.steps, StepTicks: map[string]time.Time{"two": start.Add(10 * time.Minute)}})
	ms = r.metrics(start)
	if len(ms) != 1 || ms[0].Known || !strings.Contains(ms[0].Reason, "unticked") {
		t.Fatal(ms)
	}
}

func TestWorkersUseCompleteHistoryAndBoardOnlyAuthors(t *testing.T) {
	root, base := scriptedRun(t)
	before := mustGit(t, root, "ls-remote", "origin", stateRef)
	ws := readData[WorkerList](t, invoke(t, root, 0, "workers"))
	byName := map[string]WorkerDetail{}
	for _, w := range ws {
		byName[w.Worker] = w
	}
	alice, bob, lead := byName["alice"], byName["bob"], byName["lead"]
	if alice.Completed != 1 || alice.Released != 1 || len(alice.Tasks) != 2 || alice.HeldSeconds != 70 || alice.Notes != 2 || alice.Questions != 1 || !alice.Last.Equal(base.Add(180*time.Second)) {
		t.Fatalf("alice: %+v", alice)
	}
	if bob.Completed != 2 || bob.Released != 1 || len(bob.Tasks) != 3 || len(bob.Verdicts) != 3 {
		t.Fatalf("bob: %+v", bob)
	}
	if lead.Worker != "lead" || lead.Notes != 1 || len(lead.Tasks) != 0 {
		t.Fatalf("board-only worker missing: %+v", lead)
	}
	if len(alice.Tasks[1].Rounds) != 2 || alice.Tasks[1].Rounds[0].Seconds != 10 || alice.Tasks[1].Rounds[1].Seconds != 10 {
		t.Fatalf("review timing: %+v", alice.Tasks)
	}
	invoke(t, root, 1, "workers", "unknown")
	if after := mustGit(t, root, "ls-remote", "origin", stateRef); after != before {
		t.Fatal("workers wrote state")
	}
}
func TestIdleReviewCanBeDisabledAndRoleStopNamesRequirement(t *testing.T) {
	now := time.Now().UTC()
	disabled := false
	c := Catalog{IdleReview: &disabled, Tasks: []Task{{ID: "a", Title: "A", Roles: []string{"backend"}}}}
	state := State{Tasks: map[string]Mark{"a": {Holder: "author", Since: now, Seen: now}}, Files: map[string][]byte{}}
	appendEvent(state.Files, reviewPath("a"), Event{TS: now, Event: "requested", Actor: "author", Task: "a", Branch: "a", Head: "abc"})
	a := workerAdvice(options{role: "frontend"}, "worker", snapshot{catalog: c, state: state}, now, ResourceReport{})
	if a.Action != "stop" || !strings.Contains(a.Reason, "backend") {
		t.Fatal(a)
	}
	c.IdleReview = nil
	a = workerAdvice(options{role: "frontend"}, "worker", snapshot{catalog: c, state: state}, now, ResourceReport{})
	if a.Action != "review" {
		t.Fatal(a)
	}
}

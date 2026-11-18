package bw

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func readData[T any](t *testing.T, r response) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(r.Data, &v); err != nil {
		t.Fatal(err, string(r.Data))
	}
	return v
}
func textCommand(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v: %v %s", args, err, out)
	}
	return string(out)
}
func publishCatalog(t *testing.T, root string) {
	t.Helper()
	mustGit(t, root, "add", catalogPath)
	mustGit(t, root, "commit", "--quiet", "-m", "catalog")
	mustGit(t, root, "push", "--quiet", "origin", "HEAD")
}

func TestTaskViewsAndSummaryReadOnly(t *testing.T) {
	root := fixture(t, `{"coordinators":["lead"],"tasks":[
 {"id":"schema","title":"Schema"},
 {"id":"api","title":"API","needs":["schema"],"priority":10},
 {"id":"docs","title":"Docs","priority":35},
 {"id":"frontend","title":"Frontend","needs":["api"]}
 ]}`)
	publishCatalog(t, root)
	invoke(t, root, 0, "claim", "schema", "--as", "worker-2")
	invoke(t, root, 0, "done", "schema", "--as", "worker-2")
	for _, result := range raceCommands(t, root, [][]string{
		{"claim", "api", "--as", "worker-1"},
		{"claim", "docs", "--as", "worker-3"},
	}) {
		if !result.OK {
			t.Fatal(result)
		}
	}
	invoke(t, root, 0, "release", "docs", "--as", "worker-3")
	for _, branch := range []string{"feature/one", "feature/two"} {
		invoke(t, root, 0, "review", "request", "api", "--branch", branch, "--as", "worker-1")
	}
	invoke(t, root, 0, "review", "take", "feature--two", "--as", "worker-2")
	invoke(t, root, 0, "ask", "frontend", "Product choice?", "--as", "worker-1")
	invoke(t, root, 0, "ask", "api", "Technical choice?", "--technical", "--as", "worker-2")
	invoke(t, root, 0, "ask", "docs", "Already answered?", "--technical", "--as", "worker-1")
	invoke(t, root, 0, "answer", "3", "Yes.", "--as", "worker-2")
	// Read commands must work even when every write would be rejected.
	remote := mustGit(t, root, "remote", "get-url", "origin")
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, root, "ls-remote", "origin", stateRef)
	localRefs := mustGit(t, root, "show-ref")
	rows := readData[TaskList](t, invoke(t, root, 0, "tasks"))
	ids := []string{}
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	if !reflect.DeepEqual(ids, []string{"api", "docs", "frontend", "schema"}) {
		t.Fatal(ids)
	}
	if len(rows[2].BlockedBy) != 1 || rows[2].BlockedBy[0].ID != "api" || rows[2].BlockedBy[0].Holder != "worker-1" || rows[2].BlockedBy[0].State != "claimed" {
		t.Fatal(rows[2])
	}
	if rows[3].Holder != "worker-2" || rows[3].DoneAt == nil {
		t.Fatal(rows[3])
	}
	for _, tc := range []struct {
		flags []string
		ids   []string
	}{
		{[]string{"--state", "ready"}, []string{"docs"}},
		{[]string{"--state", "done"}, []string{"schema"}},
		{[]string{"--state", "waiting", "--state", "claimed"}, []string{"api", "frontend"}},
		{[]string{"--state", "claimed", "--state", "claimed"}, []string{"api"}},
	} {
		selected := readData[TaskList](t, invoke(t, root, 0, append([]string{"tasks"}, tc.flags...)...))
		got := []string{}
		for _, r := range selected {
			got = append(got, r.ID)
		}
		if !reflect.DeepEqual(got, tc.ids) {
			t.Fatalf("%v: %v", tc.flags, got)
		}
	}
	if q := views(t, invoke(t, root, 0, "queue")); len(q) != 1 || q[0].ID != "docs" {
		t.Fatal(q)
	}
	summary := readData[Summary](t, invoke(t, root, 0, "summary"))
	if !reflect.DeepEqual(summary.Counts, map[string]int{"claimed": 1, "ready": 1, "waiting": 1, "done": 1}) || len(summary.Claims) != 1 || summary.ReviewWaiting != 1 || summary.ReviewTaken != 1 || len(summary.Questions) != 2 || summary.CoordinatorQuestions != 1 || summary.AllDone {
		t.Fatalf("%+v", summary)
	}
	waiting := textCommand(t, root, "tasks", "--state", "waiting")
	if !strings.Contains(waiting, "on api (claimed by worker-1, active ") || strings.Contains(waiting, "Schema") {
		t.Fatal(waiting)
	}
	summaryText := textCommand(t, root, "summary")
	if !strings.Contains(summaryText, "1 waiting, 1 taken") || !strings.HasSuffix(strings.TrimSpace(summaryText), summary.Progress) {
		t.Fatal(summaryText)
	}
	t.Logf("Sample tasks:\n%s", textCommand(t, root, "tasks"))
	t.Logf("Sample summary:\n%s", summaryText)
	invoke(t, root, 0, "stats")
	invoke(t, root, 0, "idle", "--as", "idle-1")
	if after := mustGit(t, root, "ls-remote", "origin", stateRef); after != before {
		t.Fatal("read commands changed remote state")
	}
	if after := mustGit(t, root, "show-ref"); after != localRefs {
		t.Fatal("read commands changed working refs")
	}
	for _, args := range [][]string{{"tasks", "--state", "bogus"}, {"tasks", "--state"}, {"summary", "--state", "ready"}, {"queue", "--done"}, {"queue", "--ready"}, {"queue", "--claimed"}, {"queue", "--waiting"}, {"tasks", "--all"}, {"notes", "--worker", "a"}, {"stats", "--state", "ready"}, {"idle"}} {
		invoke(t, root, 1, args...)
	}
}

func TestIdleConcurrentOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, tasks, action string
		waiting, code       int
	}{
		{"one unlocked task", `{"tasks":[{"id":"api","title":"API"},{"id":"ui","title":"UI","needs":["api"]}]}`, "wait", 1, 0},
		{"three unlocked tasks", `{"tasks":[{"id":"api","title":"API"},{"id":"a","title":"A","needs":["api"]},{"id":"b","title":"B","needs":["api"]},{"id":"c","title":"C","needs":["api"]}]}`, "wait", 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t, tc.tasks)
			invoke(t, root, 0, "claim", "api", "--as", "holder")
			before := mustGit(t, root, "ls-remote", "origin", stateRef)
			calls := [][]string{}
			for _, name := range []string{"idle-1", "idle-2", "idle-3", "idle-4"} {
				calls = append(calls, []string{"idle", "--as", name})
			}
			for _, r := range raceCommands(t, root, calls) {
				a := readData[IdleAdvice](t, r)
				if r.Code != tc.code || a.Action != tc.action || a.Waiting != tc.waiting || a.Claimed != 1 || a.Reason == "" {
					t.Fatalf("%+v %+v", r, a)
				}
			}
			if after := mustGit(t, root, "ls-remote", "origin", stateRef); after != before {
				t.Fatal("idle wrote state")
			}
			work := readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", "holder"))
			if work.Action != "work" || work.Task != "api" {
				t.Fatal(work)
			}
			invoke(t, root, 0, "done", "api", "--as", "holder")
			claim := readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", "idle-1"))
			if claim.Action != "claim" {
				t.Fatal(claim)
			}
		})
	}
	root := fixture(t, `{"tasks":[{"id":"api","title":"API"},{"id":"docs","title":"Docs"}]}`)
	// Competing real claims precede the ownership-first idle decision.
	winners := raceCommands(t, root, [][]string{{"claim", "api", "--as", "alice"}, {"claim", "api", "--as", "bob"}})
	holder := ""
	for _, r := range winners {
		if r.OK {
			holder = view(t, r).Holder
		}
	}
	if holder == "" {
		t.Fatal(winners)
	}
	if a := readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", holder)); a.Action != "work" {
		t.Fatal(a)
	}
	invoke(t, root, 0, "done", "api", "--as", holder)
	invoke(t, root, 0, "claim", "docs", "--as", holder)
	invoke(t, root, 0, "done", "docs", "--as", holder)
	if a := readData[IdleAdvice](t, invoke(t, root, 3, "idle", "--as", holder)); a.Action != "stop" || a.Waiting != 0 || a.Claimed != 0 {
		t.Fatal(a)
	}
	summary := readData[Summary](t, invoke(t, root, 0, "summary"))
	if !summary.AllDone || summary.Counts["done"] != 2 {
		t.Fatal(summary)
	}
	cmd := exec.Command(binary, "idle", "--as", holder)
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil || !strings.HasPrefix(string(out), "stop:") {
		t.Fatalf("%s %v", out, err)
	}
}

func TestEmptyViewsAndStaleBlockers(t *testing.T) {
	root := fixture(t, `{"tasks":[]}`)
	if rows := readData[TaskList](t, invoke(t, root, 0, "tasks")); rows == nil || len(rows) != 0 {
		t.Fatal(rows)
	}
	invoke(t, root, 3, "idle", "--as", "idle")
	summary := readData[Summary](t, invoke(t, root, 0, "summary"))
	if !summary.AllDone || len(summary.Claims)+len(summary.Reviews)+len(summary.Questions) != 0 {
		t.Fatal(summary)
	}
	invoke(t, root, 0, "stats")
	writeTasks(t, root, graph)
	invoke(t, root, 0, "claim", "a", "--as", "holder")
	idle(t, root, "a", 5*time.Hour)
	rows := readData[TaskList](t, invoke(t, root, 0, "tasks"))
	if !rows[0].Stale || rows[0].Idle != "5h" {
		t.Fatal(rows)
	}
	if text := textCommand(t, root, "tasks", "--state", "waiting"); !strings.Contains(text, "stale") || !strings.Contains(text, "holder") {
		t.Fatal(text)
	}
	// Priority and ID ordering are retained within each group.
	writeTasks(t, root, `{"tasks":[{"id":"z","title":"Z"},{"id":"a","title":"A","priority":10},{"id":"b","title":"B","priority":10}]}`)
	invoke(t, root, 0, "release", "a", "--as", "holder")
	rows = readData[TaskList](t, invoke(t, root, 0, "tasks"))
	if rows[0].ID != "a" || rows[1].ID != "b" || rows[2].ID != "z" {
		t.Fatal(rows)
	}
}

func TestReopenedDependencyDisplayKeepsOwnership(t *testing.T) {
	root := fixture(t, graph)
	invoke(t, root, 0, "claim", "a", "--as", "first")
	invoke(t, root, 0, "done", "a", "--as", "first")
	invoke(t, root, 0, "claim", "b", "--as", "second")
	invoke(t, root, 0, "reopen", "a")
	rows := readData[TaskList](t, invoke(t, root, 0, "tasks", "--state", "waiting"))
	if len(rows) != 1 || rows[0].ID != "b" || rows[0].Holder != "second" || len(rows[0].BlockedBy) != 1 || rows[0].BlockedBy[0].State != "ready" {
		t.Fatal(rows)
	}
	summary := readData[Summary](t, invoke(t, root, 0, "summary"))
	if summary.Counts["waiting"] != 1 || len(summary.Claims) != 1 || summary.Claims[0].ID != "b" {
		t.Fatal(summary)
	}
	if a := readData[IdleAdvice](t, invoke(t, root, 0, "idle", "--as", "second")); a.Action != "work" || a.Task != "b" {
		t.Fatal(a)
	}
	if v := view(t, invoke(t, root, 0, "show", "b")); v.State != "claimed" || v.Holder != "second" {
		t.Fatal(v)
	}
}

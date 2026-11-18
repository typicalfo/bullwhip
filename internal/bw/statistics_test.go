package bw

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Script real state-ref commits in a disposable remote using fixed timestamps.
// No extra production data or clock hooks are needed for deterministic figures.
func scriptedRun(t *testing.T) (string, time.Time) {
	t.Helper()
	root := fixture(t, `{"coordinators":["lead"],"tasks":[{"id":"a","title":"API","priority":10},{"id":"b","title":"UI","needs":["a"]},{"id":"c","title":"Docs"}]}`)
	publishCatalog(t, root)
	s, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(s.dir)
	snap, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	parent := snap.tip
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	at := func(n int) time.Time { return base.Add(time.Duration(n) * time.Second) }
	state := State{BoardVersion: 1, Tasks: map[string]Mark{}, Files: map[string][]byte{}}
	commit := func(n int, change func()) {
		change()
		data, err := marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		files := map[string][]byte{"state.json": data}
		for name, data := range state.Files {
			files[name] = data
		}
		tree, err := s.tree(files)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("git", "-c", "commit.gpgSign=false", "commit-tree", tree, "-p", parent, "-m", fmt.Sprintf("script %d", n))
		cmd.Dir = s.dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=script", "GIT_AUTHOR_EMAIL=script@localhost", "GIT_COMMITTER_NAME=script", "GIT_COMMITTER_EMAIL=script@localhost", "GIT_AUTHOR_DATE="+at(n).Format(time.RFC3339), "GIT_COMMITTER_DATE="+at(n).Format(time.RFC3339))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("commit: %v %s", err, out)
		}
		parent = strings.TrimSpace(string(out))
	}
	claim := func(id, who string, n int) func() {
		return func() { state.Tasks[id] = Mark{Holder: who, Since: at(n), Seen: at(n)} }
	}
	done := func(id string) func() { return func() { m := state.Tasks[id]; m.Done = true; state.Tasks[id] = m } }
	event := func(path, kind, actor, task string, n int, note string) {
		appendEvent(state.Files, path, Event{TS: at(n), Event: kind, Actor: actor, Task: task, Note: note})
	}
	review := reviewPath("feature--api")
	commit(10, claim("a", "alice", 10))
	commit(20, claim("c", "bob", 20))
	commit(30, func() { delete(state.Tasks, "a") })
	commit(32, func() { event("notes.jsonl", "noted", "alice", "a", 32, "handoff") })
	commit(40, claim("a", "bob", 40))
	commit(45, func() {
		appendEvent(state.Files, "questions.jsonl", Event{TS: at(45), Event: "asked", Actor: "alice", Task: "a", ID: 1, Note: "Which format?"})
	})
	commit(50, func() {
		delete(state.Tasks, "a")
		event("notes.jsonl", "noted", "lead", "a", 50, "Released bob's claim: worker stopped")
	})
	commit(60, claim("a", "alice", 60))
	commit(70, func() {
		appendEvent(state.Files, review, Event{TS: at(70), Event: "requested", Actor: "alice", Task: "a", Branch: "feature/api", Head: "head-1"})
	})
	commit(71, func() { event(review, "taken", "bob", "", 71, "") })
	commit(80, func() { event(review, "changes_requested", "bob", "", 80, "Fix it") })
	commit(90, func() {
		appendEvent(state.Files, review, Event{TS: at(90), Event: "requested", Actor: "alice", Task: "a", Branch: "feature/api", Head: "head-2"})
	})
	commit(100, func() { event(review, "approved", "bob", "", 100, "") })
	commit(110, done("a"))
	commit(120, claim("b", "bob", 120))
	self := reviewPath("feature--ui")
	commit(130, func() {
		appendEvent(state.Files, self, Event{TS: at(130), Event: "requested", Actor: "bob", Task: "b", Branch: "feature/ui", Head: "head-3"})
	})
	commit(140, func() { event(self, "taken", "bob", "", 140, "") })
	commit(150, func() { event(self, "self-approved", "bob", "", 150, "") })
	commit(160, done("b"))
	// Publish a partial run before the final completion to check the live view.
	mustGit(t, s.dir, "push", "--quiet", s.remote, parent+":"+stateRef)
	live := readData[Statistics](t, invoke(t, root, 0, "stats", "--task", "c"))
	if live.Run.Finished || live.Run.Ended != nil || live.Run.Done != 2 || len(live.Workers) != 1 || len(live.Workers[0].Current) != 1 || live.Run.ElapsedSeconds < 150 {
		t.Fatalf("live run: %+v", live)
	}
	commit(170, done("c"))
	commit(180, func() { event("notes.jsonl", "noted", "alice", "a", 180, "after the run") })
	mustGit(t, s.dir, "push", "--quiet", s.remote, parent+":"+stateRef)
	return root, base
}

func TestStatsScriptedHistory(t *testing.T) {
	root, base := scriptedRun(t)
	tip := mustGit(t, root, "ls-remote", "origin", stateRef)
	report := readData[Statistics](t, invoke(t, root, 0, "stats"))
	if report.Run.Started == nil || !report.Run.Started.Equal(base.Add(10*time.Second)) || report.Run.Ended == nil || !report.Run.Ended.Equal(base.Add(170*time.Second)) || !report.Run.Finished || report.Run.ElapsedSeconds != 160 || report.Run.Done != 3 || report.Run.Total != 3 || math.Abs(report.Run.DonePerHour-67.5) > 0.001 {
		t.Fatalf("run: %+v", report.Run)
	}
	workers := map[string]WorkerMetrics{}
	for _, w := range report.Workers {
		workers[w.Worker] = w
	}
	bob, alice, lead := workers["bob"], workers["alice"], workers["lead"]
	if len(report.Workers) != 3 || report.Workers[0].Worker != "bob" || bob.Completed != 2 || bob.HeldSeconds != 200 || len(bob.Releases) != 1 || len(bob.ReviewsGiven) != 3 || len(bob.ReviewsReceived) != 1 || len(bob.Current) != 0 {
		t.Fatalf("bob: %+v", bob)
	}
	if alice.Completed != 1 || alice.HeldSeconds != 70 || len(alice.Releases) != 1 || alice.Releases[0].Forced || len(alice.ReviewsReceived) != 2 || alice.Questions != 1 || alice.Notes != 1 {
		t.Fatalf("alice: %+v", alice)
	}
	if len(lead.Releases) != 1 || !lead.Releases[0].Forced || lead.Releases[0].Holder != "bob" || lead.Releases[0].Reason != "worker stopped" || lead.Notes != 1 {
		t.Fatalf("lead: %+v", lead)
	}
	tasks := map[string]TaskMetrics{}
	for _, row := range report.Tasks {
		tasks[row.ID] = row
	}
	a, b, c := tasks["a"], tasks["b"], tasks["c"]
	if a.ReadySeconds != 20 || a.WaitingSeconds != 0 || a.HeldSeconds != 80 || a.ClaimToDoneSeconds == nil || *a.ClaimToDoneSeconds != 100 || len(a.Holders) != 3 || a.Holders[0].Worker != "alice" || a.Holders[1].Worker != "bob" || a.Holders[2].Worker != "alice" || a.Holders[0].Seconds != 20 || a.Holders[1].Seconds != 10 || a.Holders[2].Seconds != 50 || len(a.Rounds) != 2 || a.Rounds[0].Verdict != "changes_requested" || a.Rounds[1].Verdict != "approved" {
		t.Fatalf("a: %+v", a)
	}
	if b.WaitingSeconds != 100 || b.ReadySeconds != 10 || b.HeldSeconds != 40 || len(b.Rounds) != 1 || !b.Rounds[0].SelfApproved || b.Rounds[0].Verdict != "self-approved" {
		t.Fatalf("b: %+v", b)
	}
	if c.ReadySeconds != 10 || c.WaitingSeconds != 0 || c.HeldSeconds != 150 {
		t.Fatalf("c: %+v", c)
	}
	selected := readData[Statistics](t, invoke(t, root, 0, "stats", "--worker", "bob", "--task", "a"))
	if selected.Run.ElapsedSeconds != 160 || len(selected.Workers) != 1 || selected.Workers[0].Worker != "bob" || selected.Workers[0].HeldSeconds != 10 || selected.Workers[0].Completed != 0 || len(selected.Tasks) != 1 || selected.Tasks[0].ID != "a" {
		t.Fatalf("filtered: %+v", selected)
	}
	unknown := readData[Statistics](t, invoke(t, root, 0, "stats", "--worker", "unknown"))
	if len(unknown.Workers)+len(unknown.Tasks) != 0 {
		t.Fatal(unknown)
	}
	invoke(t, root, 1, "stats", "--task", "unknown")
	rows := readData[TaskList](t, invoke(t, root, 0, "tasks", "--state", "done"))
	for _, r := range rows {
		want := map[string]int{"a": 110, "b": 160, "c": 170}[r.ID]
		if r.DoneAt == nil || !r.DoneAt.Equal(base.Add(time.Duration(want)*time.Second)) {
			t.Fatal(r)
		}
	}
	text := textCommand(t, root, "stats")
	t.Logf("Sample stats:\n%s", text)
	for _, part := range []string{"67.50/h", "2m40s", "worker stopped", "changes_requested", "self-approved=true", "READY>CLAIM", "WAITING"} {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %s: %s", part, text)
		}
	}
	if after := mustGit(t, root, "ls-remote", "origin", stateRef); after != tip {
		t.Fatal("stats wrote state")
	}
}

func TestStatsNotStartedAndReopened(t *testing.T) {
	root := fixture(t, graph)
	report := readData[Statistics](t, invoke(t, root, 0, "stats"))
	if report.Run.Started != nil || report.Run.ElapsedSeconds != 0 || report.Run.Finished || len(report.Workers) != 0 || len(report.Tasks) != 3 {
		t.Fatalf("not started: %+v", report)
	}
	root, _ = scriptedRun(t)
	invoke(t, root, 0, "reopen", "a")
	report = readData[Statistics](t, invoke(t, root, 0, "stats"))
	if report.Run.Finished || report.Run.Ended != nil || report.Run.Done != 2 {
		t.Fatalf("reopened: %+v", report.Run)
	}
	for _, task := range report.Tasks {
		if task.ID == "a" && (task.DoneAt != nil || task.ClaimToDoneSeconds != nil) {
			t.Fatal(task)
		}
	}
}

func TestHistoryPinnedSnapshotAndCorruption(t *testing.T) {
	root := fixture(t, graph)
	s, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(s.dir)
	snap, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	invoke(t, root, 0, "claim", "a", "--as", "worker")
	log, err := s.taskHistory(snap)
	if err != nil || len(log) != 0 {
		t.Fatalf("history must remain at the original snapshot: %+v %v", log, err)
	}
	snap, err = s.load()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := s.tree(map[string][]byte{"state.json": []byte(`{"tasks":null}`)})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := git(s.dir, nil, "commit-tree", tree, "-p", snap.tip, "-m", "malformed historical state")
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.TrimSpace(string(raw))
	tip, err := s.commit(snap.state, bad, strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	mustGit(t, s.dir, "push", "--quiet", s.remote, tip+":"+stateRef)
	r := invoke(t, root, 1, "stats")
	if !strings.Contains(r.Error, "Invalid historical state") {
		t.Fatal(r)
	}
	// State-only summary remains usable: the latest snapshot itself is valid.
	invoke(t, root, 0, "summary")
	mustGit(t, root, "remote", "set-url", "origin", "/nonexistent/bw-history-remote")
	invoke(t, root, 4, "stats")
}

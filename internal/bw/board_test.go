package bw

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func commitFixture(t *testing.T, root string) string {
	t.Helper()
	mustGit(t, root, "add", catalogPath)
	mustGit(t, root, "commit", "--quiet", "-m", "catalog")
	mustGit(t, root, "push", "--quiet", "origin", "HEAD")
	return mustGit(t, root, "rev-parse", "HEAD")
}
func reviewResult(t *testing.T, r response) Review {
	t.Helper()
	var v Review
	if e := json.Unmarshal(r.Data, &v); e != nil {
		t.Fatal(e)
	}
	return v
}
func requestFixture(t *testing.T) (string, string) {
	t.Helper()
	root := fixture(t, graph)
	commitFixture(t, root)
	invoke(t, root, 0, "claim", "a", "--as", "author")
	r := reviewResult(t, invoke(t, root, 0, "review", "request", "a", "--branch", "feature/a", "--as", "author"))
	return root, r.ID
}
func TestReviewLifecycle(t *testing.T) {
	root, id := requestFixture(t)
	first := reviewResult(t, invoke(t, root, 0, "review", "request", "a", "--branch", "feature/a", "--as", "author"))
	if !first.Unchanged {
		t.Fatal(first)
	}
	invoke(t, root, 2, "review", "request", "b", "--branch", "feature/b", "--as", "author")
	invoke(t, root, 1, "review", "request", "a", "--head", "missing", "--as", "author")
	invoke(t, root, 1, "review", "request", "a", "--head", "--help", "--as", "author")
	invoke(t, root, 2, "review", "take", id, "--as", "author")
	invoke(t, root, 2, "review", "take", id, "--self", "--as", "author")
	invoke(t, root, 1, "review", "take", "--self", "--as", "author")
	invoke(t, root, 3, "review", "take", "--as", "author")
	r := reviewResult(t, invoke(t, root, 0, "review", "take", "--as", "reviewer"))
	if r.ID != id || r.Reviewer != "reviewer" {
		t.Fatal(r)
	}
	if r = reviewResult(t, invoke(t, root, 0, "review", "take", "--as", "reviewer")); !r.Unchanged {
		t.Fatal(r)
	}
	invoke(t, root, 2, "review", "take", id, "--as", "other")
	invoke(t, root, 2, "review", "approve", id, "--as", "author")
	invoke(t, root, 1, "review", "changes", id, "--as", "reviewer")
	notes := filepath.Join(root, "findings.md")
	os.WriteFile(notes, []byte("Fix the edge case."), 0644)
	r = reviewResult(t, invoke(t, root, 0, "review", "changes", id, "--notes", notes, "--model", "test-model", "--as", "reviewer"))
	if r.State != "changes_requested" || !strings.Contains(r.Findings, "Fix the edge case.") {
		t.Fatal(r)
	}
	if r = reviewResult(t, invoke(t, root, 0, "review", "changes", id, "--notes", notes, "--model", "test-model", "--as", "reviewer")); !r.Unchanged {
		t.Fatal(r)
	}
	invoke(t, root, 0, "review", "release", id, "--as", "reviewer")
	invoke(t, root, 3, "review", "take", "--as", "other")
	invoke(t, root, 0, "review", "request", "a", "--branch", "feature/a", "--as", "author")
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	invoke(t, root, 2, "review", "release", id, "--as", "other")
	invoke(t, root, 0, "review", "release", id, "--force", "--as", "human")
	if r = reviewResult(t, invoke(t, root, 0, "review", "release", id, "--as", "human")); !r.Unchanged {
		t.Fatal(r)
	}
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	invoke(t, root, 0, "review", "approve", id, "--model", "test-model", "--as", "reviewer")
	if r = reviewResult(t, invoke(t, root, 0, "review", "approve", id, "--model", "test-model", "--as", "reviewer")); !r.Unchanged {
		t.Fatal(r)
	}
	if r = reviewResult(t, invoke(t, root, 0, "review", "release", id, "--as", "reviewer")); !r.Unchanged {
		t.Fatal(r)
	}
	invoke(t, root, 2, "review", "merged", id, "--as", "other")
	r = reviewResult(t, invoke(t, root, 0, "review", "merged", id, "--as", "author"))
	if r.State != "merged" {
		t.Fatal(r)
	}
	if r = reviewResult(t, invoke(t, root, 0, "review", "merged", id, "--as", "author")); !r.Unchanged {
		t.Fatal(r)
	}
	if v := view(t, invoke(t, root, 0, "show", "a")); v.State != "claimed" {
		t.Fatal("merge ran done", v)
	}
	list := invoke(t, root, 0, "review", "list", "--open")
	if string(list.Data) != "[]" {
		t.Fatal(string(list.Data))
	}
	invoke(t, root, 0, "review", "show", id)
	invoke(t, root, 0, "done", "a", "--as", "author")
	if reviewResult(t, invoke(t, root, 0, "review", "show", id)).State != "merged" {
		t.Fatal("task write lost board")
	}
}
func TestReviewNewHeadAndDefaultAncestry(t *testing.T) {
	root, id := requestFixture(t)
	old := mustGit(t, root, "rev-parse", "HEAD")
	mustGit(t, root, "commit", "--quiet", "--allow-empty", "-m", "new head")
	head := mustGit(t, root, "rev-parse", "HEAD")
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	invoke(t, root, 0, "review", "approve", id, "--as", "reviewer")
	r := reviewResult(t, invoke(t, root, 0, "review", "request", "a", "--branch", "feature/a", "--as", "author"))
	if r.Head != head || r.State != "requested" {
		t.Fatal(r)
	}
	invoke(t, root, 2, "review", "merged", id, "--as", "author")
	r = reviewResult(t, invoke(t, root, 0, "review", "approve", id, "--as", "reviewer"))
	if r.Head != head {
		t.Fatal(r)
	}
	invoke(t, root, 2, "review", "merged", id, "--as", "author")
	mustGit(t, root, "push", "--quiet", "origin", "HEAD")
	// Force a shallow fetch of the current default tip; older ancestors must work.
	invoke(t, root, 0, "review", "merged", id, "--sha", old, "--as", "author")
}
func TestSelfReviewTimingAndSharedCatalog(t *testing.T) {
	root := fixture(t, `{"self_review_after":"0s","tasks":[{"id":"a","title":"A"}]}`)
	commitFixture(t, root)
	writeTasks(t, root, `{"self_review_after":"30m","tasks":[{"id":"a","title":"A"}]}`)
	invoke(t, root, 0, "claim", "a", "--as", "author")
	invoke(t, root, 0, "review", "request", "a", "--branch", "self/a", "--as", "author")
	r := reviewResult(t, invoke(t, root, 0, "review", "take", "self--a", "--self", "--as", "author"))
	if !r.Self {
		t.Fatal(r)
	}
	notes := filepath.Join(root, "self-findings.md")
	os.WriteFile(notes, []byte("Fix this first."), 0644)
	r = reviewResult(t, invoke(t, root, 0, "review", "changes", "self--a", "--notes", notes, "--as", "author"))
	if r.State != "self-changes" {
		t.Fatal(r)
	}
	invoke(t, root, 0, "review", "request", "a", "--branch", "self/a", "--as", "author")
	invoke(t, root, 0, "review", "approve", "self--a", "--as", "author")
	r = reviewResult(t, invoke(t, root, 0, "review", "show", "self--a"))
	if r.State != "self-approved" {
		t.Fatal(r)
	}
	invoke(t, root, 0, "review", "merged", "self--a", "--as", "author")
	// Boundary is checked against the request, using the shared setting.
	files := map[string][]byte{}
	ts := time.Now().UTC().Add(-30 * time.Minute)
	appendEvent(files, reviewPath("self--a"), Event{TS: ts, Actor: "author", Task: "a", Branch: "self/a", Event: "requested", Head: "abc"})
	snap := snapshot{catalog: Catalog{}, state: State{Tasks: map[string]Mark{}, Files: files}}
	o := options{command: "review", args: []string{"self--a"}, self: true}
	if _, _, e := boardOperation(o, "take", "author", snap, ts.Add(30*time.Minute-time.Nanosecond)); e == nil {
		t.Fatal("early self take")
	}
	if _, changed, e := boardOperation(o, "take", "author", snap, ts.Add(30*time.Minute)); e != nil || !changed {
		t.Fatal(changed, e)
	}
}
func TestReviewCompetingProcesses(t *testing.T) {
	root, id := requestFixture(t)
	for round := 0; round < 3; round++ {
		args := [][]string{{"review", "take", id, "--as", "r1"}, {"review", "take", id, "--as", "r2"}}
		wins, conflicts := 0, 0
		for _, r := range raceCommands(t, root, args) {
			switch r.Code {
			case 0:
				wins++
			case 2:
				conflicts++
			default:
				t.Fatalf("take race: %+v", r)
			}
		}
		if wins != 1 || conflicts != 1 {
			t.Fatalf("wins %d conflicts %d", wins, conflicts)
		}
		invoke(t, root, 0, "review", "release", id, "--force", "--as", "human")
	}
}
func TestBoardMessagesAndConcurrentIDs(t *testing.T) {
	root := fixture(t, graph)
	q := invoke(t, root, 0, "ask", "a", "Question?", "--as", "sam")
	var event Event
	json.Unmarshal(q.Data, &event)
	if event.ID != 1 {
		t.Fatal(string(q.Data))
	}
	if r := invoke(t, root, 0, "ask", "a", "Question?", "--as", "sam"); !strings.Contains(string(r.Data), `"unchanged":true`) {
		t.Fatal(string(r.Data))
	}
	invoke(t, root, 0, "answer", "1", "Answer.", "--as", "other")
	invoke(t, root, 0, "answer", "1", "Answer.", "--as", "other")
	invoke(t, root, 1, "answer", "2", "Unknown.", "--as", "other")
	invoke(t, root, 1, "answer", "1", "Answer.", "--technical", "--as", "other")
	if r := invoke(t, root, 0, "questions"); string(r.Data) != "[]" {
		t.Fatal(string(r.Data))
	}
	invoke(t, root, 0, "note", "a", "Handoff.", "--as", "sam")
	invoke(t, root, 0, "note", "b", "Another.", "--as", "sam")
	if r := invoke(t, root, 0, "note", "a", "Handoff.", "--as", "sam"); !strings.Contains(string(r.Data), `"unchanged":true`) {
		t.Fatal(string(r.Data))
	}
	var notes []Event
	json.Unmarshal(invoke(t, root, 0, "notes", "--task", "a").Data, &notes)
	if len(notes) != 1 || notes[0].Note != "Handoff." {
		t.Fatal(notes)
	}
	args := [][]string{}
	for i := 0; i < 8; i++ {
		args = append(args, []string{"ask", "a", fmt.Sprintf("Parallel %d?", i), "--as", fmt.Sprintf("worker-%d", i)})
	}
	for _, r := range raceCommands(t, root, args) {
		if r.Code != 0 {
			t.Fatal(r)
		}
	}
	var qs []Question
	json.Unmarshal(invoke(t, root, 0, "questions", "--all").Data, &qs)
	ids := map[int]bool{}
	for _, q := range qs {
		if ids[q.ID] {
			t.Fatal("duplicate id")
		}
		ids[q.ID] = true
	}
	if len(ids) != 9 {
		t.Fatal(len(ids))
	}
	// Check board survives another clone and unrelated claim updates.
	clone := filepath.Join(t.TempDir(), "clone")
	mustGit(t, root, "clone", "--quiet", mustGit(t, root, "remote", "get-url", "origin"), clone)
	os.MkdirAll(filepath.Join(clone, ".bullwhip"), 0755)
	writeTasks(t, clone, graph)
	invoke(t, clone, 0, "claim", "c", "--as", "elsewhere")
	var all []Question
	json.Unmarshal(invoke(t, clone, 0, "questions", "--all").Data, &all)
	if len(all) != 9 {
		t.Fatal("board not shared", len(all))
	}
}
func TestBoardHelpIdentityAndFlags(t *testing.T) {
	root := fixture(t, graph)
	for _, args := range [][]string{{"review", "take"}, {"ask", "a", "Q"}, {"answer", "1", "A"}, {"note", "a", "N"}, {"review", "list", "--mine"}} {
		invoke(t, root, 1, args...)
	}
	for _, args := range [][]string{{"review", "take", "--all"}, {"review", "show", "id", "--open"}, {"questions", "--self"}, {"note", "a", "x", "--force"}, {"show", "a", "--model", "test"}} {
		invoke(t, root, 1, args...)
	}
	for _, cmd := range []string{"request", "list", "take", "release", "approve", "changes", "merged", "show"} {
		invoke(t, t.TempDir(), 0, "review", cmd, "--help")
	}
}
func TestRefSections(t *testing.T) {
	root := fixture(t, `{"tasks":[{"id":"a","title":"A","ref":"plan.md#slice-4"},{"id":"c","title":"C","ref":"plan.md#missing"}]}`)
	os.WriteFile(filepath.Join(root, "plan.md"), []byte("# Plan\n\n## Slice 4\nDo this.\n### Detail\nMore.\n## Next\nOther.\n"), 0644)
	v := view(t, invoke(t, root, 0, "show", "a"))
	if !strings.Contains(v.Section, "Do this.") || strings.Contains(v.Section, "Other.") || v.Warning != "" {
		t.Fatal(v)
	}
	v = view(t, invoke(t, root, 0, "crack", "--as", "sam"))
	if v.Section == "" {
		t.Fatal(v)
	}
	v = view(t, invoke(t, root, 0, "claim", "c", "--as", "other"))
	if v.Warning == "" || v.State != "claimed" {
		t.Fatal(v)
	}
	outside := filepath.Join(t.TempDir(), "secret.md")
	os.WriteFile(outside, []byte("## Secret\nnever show"), 0644)
	os.Symlink(outside, filepath.Join(root, "link.md"))
	result := resolveRefs(View{Task: Task{Ref: "link.md#secret"}}, root).(View)
	if result.Warning == "" || result.Section != "" {
		t.Fatal(result)
	}
	for _, setting := range []string{`"-1m"`, `"bad"`, `42`} {
		if _, e := parseCatalog([]byte(`{"self_review_after":` + setting + `,"tasks":[]}`)); e == nil {
			t.Fatal(setting)
		}
	}
}
func TestBoardOldestAndStaleOwnership(t *testing.T) {
	now := time.Now().UTC()
	files := map[string][]byte{}
	for i, name := range []string{"own", "old", "new"} {
		actor := "other"
		if name == "own" {
			actor = "sam"
		}
		appendEvent(files, reviewPath(name), Event{TS: now.Add(time.Duration(i-3) * time.Hour), Actor: actor, Event: "requested", Task: "a", Branch: name, Head: "head"})
	}
	snap := snapshot{catalog: Catalog{}, state: State{Tasks: map[string]Mark{}, Files: files}}
	value, changed, err := boardOperation(options{command: "review"}, "take", "sam", snap, now)
	if err != nil || !changed || value.(Review).ID != "old" {
		t.Fatal(value, changed, err)
	}
	if _, _, err = boardOperation(options{command: "review", args: []string{"old"}}, "take", "someone", snap, now.Add(365*24*time.Hour)); err == nil {
		t.Fatal("old review stolen")
	}
	if _, _, err = boardOperation(options{command: "review", mine: true}, "list", "sam", snap, now); err != nil {
		t.Fatal(err)
	}
}
func TestBoardLostAcknowledgementAndMixedWrites(t *testing.T) {
	root, id := requestFixture(t)
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	realGit, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nlost=0\nfor arg do [ \"$arg\" != push ] || lost=1; done\n'" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\"\nresult=$?\nif [ \"$result\" = 0 ] && [ \"$lost\" = 1 ]; then exit 1; fi\nexit \"$result\"\n"
	os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0755)
	notes := filepath.Join(root, "verdict.md")
	os.WriteFile(notes, []byte("Reviewed."), 0644)
	cmd := command(root, "review", "approve", id, "--notes", notes, "--as", "reviewer")
	cmd.Env = append(cmd.Env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, e := cmd.Output()
	r, code, err := outputCode(out, e)
	if err != nil || code != 0 || reviewResult(t, r).State != "approved" {
		t.Fatal(string(out), err)
	}
	if v := reviewResult(t, invoke(t, root, 0, "review", "approve", id, "--notes", notes, "--as", "reviewer")); !v.Unchanged || strings.Count(v.Findings, "Reviewed.") != 1 {
		t.Fatal(v)
	}
	args := [][]string{{"claim", "c", "--as", "worker"}, {"note", "a", "Concurrent note.", "--as", "author"}, {"ask", "a", "Concurrent question?", "--as", "author"}}
	for _, r := range raceCommands(t, root, args) {
		if r.Code != 0 {
			t.Fatal(r)
		}
	}
	if v := view(t, invoke(t, root, 0, "show", "c")); v.Holder != "worker" {
		t.Fatal(v)
	}
	if v := reviewResult(t, invoke(t, root, 0, "review", "show", id)); v.State != "approved" {
		t.Fatal(v)
	}
}
func TestRefExplicitDuplicateAndFences(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "plan.md")
	os.WriteFile(path, []byte("# Plan\n```md\n## Fake\n```\n<a id=\"explicit\"></a>\n## Real\nBody.\n## Real\nSecond.\n## End\n"), 0644)
	for _, tt := range []struct{ anchor, want string }{{"explicit", "Body."}, {"real-1", "Second."}} {
		v := resolveRefs(View{Task: Task{Ref: "plan.md#" + tt.anchor}}, root).(View)
		if !strings.Contains(v.Section, tt.want) || strings.Contains(v.Section, "## End") {
			t.Fatal(v)
		}
	}
	v := resolveRefs(View{Task: Task{Ref: "plan.md#fake"}}, root).(View)
	if v.Warning == "" {
		t.Fatal("matched fenced heading", v)
	}
}

func TestOversizedBoardWriteDoesNotPublish(t *testing.T) {
	root, id := requestFixture(t)
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	notes := filepath.Join(root, "too-large.md")
	if e := os.WriteFile(notes, []byte(strings.Repeat("x", 4<<20)), 0644); e != nil {
		t.Fatal(e)
	}
	r := invoke(t, root, 1, "review", "approve", id, "--notes", notes, "--as", "reviewer")
	if !strings.Contains(r.Error, "4 MiB") {
		t.Fatal(r.Error)
	}
	if v := reviewResult(t, invoke(t, root, 0, "review", "show", id)); v.State != "taken" || v.Findings != "" {
		t.Fatal("oversized write published", v)
	}
}

func TestBoardSchemaAndTaskWrites(t *testing.T) {
	root := fixture(t, graph)
	invoke(t, root, 0, "note", "a", "Enable board.", "--as", "sam")
	s, e := openStore(root)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(s.dir)
	snap, e := s.load()
	if e != nil || snap.state.BoardVersion != 1 {
		t.Fatal(snap.state.BoardVersion, e)
	}
	invoke(t, root, 0, "claim", "a", "--as", "sam")
	var notes []Event
	json.Unmarshal(invoke(t, root, 0, "notes").Data, &notes)
	if len(notes) != 1 {
		t.Fatal(notes)
	}
}

func TestBoardPreservesUnrelatedStateFiles(t *testing.T) {
	root, id := requestFixture(t)
	s, err := openStore(root)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(s.dir)
	marker := "imports/" + strings.Repeat("a", 64)
	_, err = s.update(false, func(snap snapshot) (any, bool, error) {
		snap.state.Files[marker] = []byte("complete\n")
		return nil, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	invoke(t, root, 0, "review", "list")
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	h := heldResult(t, invoke(t, root, 2, "holds", "reviewer", "--check"))
	if len(h.Reviews) != 1 || h.Reviews[0].ID != id {
		t.Fatal(h)
	}
	invoke(t, root, 0, "review", "approve", id, "--as", "reviewer")
	invoke(t, root, 0, "ask", "a", "Question?", "--as", "author")
	invoke(t, root, 0, "answer", "1", "Answer.", "--as", "reviewer")
	invoke(t, root, 0, "note", "a", "Reviewed.", "--as", "author")
	invoke(t, root, 0, "done", "a", "--as", "author")
	snap, err := s.load()
	if err != nil {
		t.Fatal(err)
	}
	if string(snap.state.Files[marker]) != "complete\n" {
		t.Fatal("unrelated state file changed")
	}
	if snap.state.BoardVersion != 1 {
		t.Fatal(snap.state.BoardVersion)
	}
}

func TestCoordinatorAnswers(t *testing.T) {
	root := fixture(t, graph)
	writeTasks(t, root, `{"coordinators":["lead"],"tasks":[{"id":"a","title":"A"},{"id":"b","title":"B","needs":["a"]}]}`)
	invoke(t, root, 0, "ask", "a", "Cut the grace period?", "--as", "sam")
	invoke(t, root, 0, "ask", "a", "Which helper?", "--technical", "--as", "sam")
	if r := invoke(t, root, 2, "answer", "1", "Yes.", "--as", "reviewer"); !strings.Contains(r.Error, "coordinator") {
		t.Fatal(r.Error)
	}
	invoke(t, root, 0, "answer", "1", "No; keep it.", "--as", "lead")
	invoke(t, root, 0, "answer", "2", "Use the lab one.", "--as", "reviewer")
	var qs []Question
	json.Unmarshal(invoke(t, root, 0, "questions", "--all").Data, &qs)
	if len(qs) != 2 || qs[0].Technical || !qs[1].Technical || len(qs[0].Answers) != 1 || qs[0].Answers[0].Actor != "lead" {
		t.Fatal(qs)
	}
	// Without coordinators anyone may answer.
	writeTasks(t, root, `{"tasks":[{"id":"a","title":"A"},{"id":"b","title":"B","needs":["a"]}]}`)
	invoke(t, root, 0, "ask", "a", "Open question?", "--as", "sam")
	invoke(t, root, 0, "answer", "3", "Fine.", "--as", "reviewer")
}

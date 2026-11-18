package bw

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMetadataValidation(t *testing.T) {
	for _, task := range []string{
		`{"id":"a","title":"A","size":"huge"}`, `{"id":"a","title":"A","effort":"max"}`,
		`{"id":"a","title":"A","review_effort":"max"}`, `{"id":"a","title":"A","steps":[]}`,
		`{"id":"a","title":"A","steps":[""]}`, `{"id":"a","title":"A","steps":["same","same"]}`,
		`{"id":"a","title":"A","steps":[{"id":"bad/id","text":"x"}]}`,
		`{"id":"a","title":"A","steps":[{"id":"x","text":"x","unknown":1}]}`,
		`{"id":"a","title":"A","steps":["` + strings.Repeat("界", 121) + `"]}`,
	} {
		if _, e := parseCatalog([]byte(`{"tasks":[` + task + `]}`)); e == nil {
			t.Errorf("accepted %s", task)
		}
	}
	if _, e := parseCatalog([]byte(`{"forge":"github","repo_url":"https://github.com/a/b","tasks":[{"id":"a","title":"A","size":"s","effort":"high","steps":["one",{"id":"two","text":"Two"}]}]}`)); e != nil {
		t.Fatal(e)
	}
}
func TestEffortAndStepsProcesses(t *testing.T) {
	root := fixture(t, `{"tasks":[{"id":"hard","title":"Hard","priority":99,"effort":"high"},{"id":"easy","title":"Easy","size":"s","effort":"low","steps":["First",{"id":"stable","text":"Second"}]}]}`)
	if got := view(t, invoke(t, root, 0, "crack", "--as", "low-worker", "--effort", "low")); got.ID != "easy" {
		t.Fatal(got)
	}
	invoke(t, root, 2, "step", "easy", "1", "--as", "intruder")
	invoke(t, root, 1, "step", "easy", "0", "--as", "low-worker")
	invoke(t, root, 0, "step", "easy", "1", "--as", "low-worker")
	got := view(t, invoke(t, root, 0, "step", "easy", "2", "--as", "low-worker"))
	if got.Progress != "2/2" {
		t.Fatal(got)
	}
	writeTasks(t, root, `{"tasks":[{"id":"hard","title":"Hard","effort":"high"},{"id":"easy","title":"Easy","steps":[{"id":"stable","text":"Reworded"},"First","New"]}]}`)
	got = view(t, invoke(t, root, 0, "show", "easy"))
	if got.Progress != "2/3" || got.StepChanges == nil || len(got.StepChanges.Added) != 1 {
		t.Fatal(got)
	}
	got = view(t, invoke(t, root, 0, "step", "easy", "1", "--undo", "--as", "low-worker"))
	if got.Progress != "1/3" {
		t.Fatal(got)
	}
	got = view(t, invoke(t, root, 0, "done", "easy", "--as", "low-worker"))
	if got.State != "done" || len(got.UntickedSteps) != 2 {
		t.Fatal(got)
	}
	r := invoke(t, root, 3, "idle", "--as", "low-worker", "--effort", "low")
	if !strings.Contains(string(r.Data), "high") {
		t.Fatal(string(r.Data))
	}
	if rows := views(t, invoke(t, root, 0, "queue", "--effort", "low")); len(rows) != 0 {
		t.Fatal(rows)
	}
	invoke(t, root, 0, "claim", "hard", "--as", "low-worker", "--effort", "low")
}
func TestStepReconciliationAndStats(t *testing.T) {
	now := time.Now().UTC()
	c, _ := parseCatalog([]byte(`{"tasks":[{"id":"a","title":"A","size":"m","steps":["new",{"id":"keep","text":"reword"}]}]}`))
	state := State{Tasks: map[string]Mark{"a": {Holder: "w", Since: now.Add(-time.Minute), StepTicks: map[string]time.Time{"old": now.Add(-40 * time.Second), "keep": now.Add(-20 * time.Second)}, StepCatalog: []Step{{Text: "old"}, {ID: "keep", Text: "before"}}}}, Files: map[string][]byte{}}
	v, _ := findView(c, state, "a")
	if v.Progress != "1/2" || len(v.StepChanges.Removed) != 1 || len(v.StepChanges.Reopened) != 1 {
		t.Fatal(v)
	}
	reconcileSteps(state, c)
	if _, ok := state.Tasks["a"].StepTicks["old"]; ok {
		t.Fatal("removed tick retained")
	}
	report, e := statistics(options{}, snapshot{state: state, catalog: c}, []TaskEvent{{At: now.Add(-time.Minute), Task: "a", Kind: "claimed", Holder: "w"}, {At: now.Add(-20 * time.Second), Task: "a", Kind: "steps", Holder: "w", StepTicks: state.Tasks["a"].StepTicks, StepCatalog: c.Tasks[0].Steps}}, now)
	if e != nil || len(report.Tasks[0].Steps) != 1 || report.Tasks[0].Steps[0].Known || report.Sizes[0].Size != "m" {
		t.Fatalf("%+v %v", report, e)
	}
}
func TestReviewEffortSelection(t *testing.T) {
	now := time.Now().UTC()
	files := map[string][]byte{}
	appendEvent(files, reviewPath("hard"), Event{TS: now, Actor: "author", Event: "requested", Task: "hard", Branch: "hard", Head: "abc"})
	appendEvent(files, reviewPath("easy"), Event{TS: now.Add(time.Second), Actor: "author", Event: "requested", Task: "easy", Branch: "easy", Head: "def"})
	snap := snapshot{state: State{Files: files}, catalog: Catalog{Tasks: []Task{{ID: "hard", Effort: "high"}, {ID: "easy", Effort: "high", ReviewEffort: "low"}}}}
	result, _, e := boardOperation(options{command: "review", effort: "low"}, "take", "reviewer", snap, now)
	if e != nil || result.(Review).ID != "easy" {
		t.Fatalf("%+v %v", result, e)
	}
	result, _, e = boardOperation(options{command: "review", effort: "low", args: []string{"hard"}}, "take", "reviewer", snap, now)
	if e != nil || result.(Review).ID != "hard" {
		t.Fatalf("%+v %v", result, e)
	}
}
func TestForgeLinks(t *testing.T) {
	f := forgeLinks{"https://forge.test/group/repo", "main"}
	v := f.task(View{Task: Task{Ref: "docs/hello world.md#some-heading"}})
	if !strings.Contains(v.Links["ref"], "/blob/main/docs/hello%20world.md#some-heading") {
		t.Fatal(v)
	}
	r := f.review(Review{Branch: "feature/a b", Head: "abc", Events: []Event{{Event: "merged", Head: "def"}}}, Catalog{})
	if !strings.Contains(r.Links["branch"], "/tree/feature%2Fa%20b") || !strings.HasSuffix(r.Links["head"], "/commit/abc") || !strings.HasSuffix(r.Links["merged"], "/commit/def") || !strings.Contains(r.Links["search"], "/pulls?") {
		t.Fatal(r)
	}
	if f.task(View{Task: Task{Ref: "../secret"}}).Links["ref"] != "" {
		t.Fatal("escaped repository")
	}
	if _, e := parseCatalog([]byte(`{"forge":"gitlab","tasks":[{"id":"a","title":"A"}]}`)); e == nil || !strings.Contains(e.Error(), "forge must be github") {
		t.Fatalf("expected gitlab forge to be rejected, got %v", e)
	}
	b, _ := json.Marshal(Step{ID: "one", Text: "One"})
	var s Step
	if e := json.Unmarshal(b, &s); e != nil || s.ID != "one" {
		t.Fatal(s, e)
	}
}

func TestForgeDefaultsToGitHub(t *testing.T) {
	f := forgeFor(Catalog{RepoURL: "https://example.com/org/repo"}, t.TempDir())
	if got := f.commit("abc"); got != "https://example.com/org/repo/commit/abc" {
		t.Fatal(got)
	}
}

func TestConcurrentStepTicks(t *testing.T) {
	root := fixture(t, `{"tasks":[{"id":"a","title":"A","steps":["one","two"]}]}`)
	invoke(t, root, 0, "claim", "a", "--as", "holder")
	// Separate processes fetch the same state and must preserve both ticks after CAS.
	results := raceCommands(t, root, [][]string{{"step", "a", "1", "--as", "holder"}, {"step", "a", "2", "--as", "holder"}})
	for _, r := range results {
		if !r.OK {
			t.Fatalf("tick failed: %+v", r)
		}
	}
	got := view(t, invoke(t, root, 0, "show", "a"))
	if got.Progress != "2/2" {
		t.Fatal(got)
	}
}

func TestWorkerTimeIntervals(t *testing.T) {
	base := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	log := []TaskEvent{{At: base, Task: "a", Kind: "claimed", Holder: "w"}, {At: base.Add(20 * time.Second), Task: "a", Kind: "activity", Holder: "w"}, {At: base.Add(50 * time.Second), Task: "a", Kind: "released", Holder: "w"}, {At: base.Add(70 * time.Second), Task: "b", Kind: "claimed", Holder: "w"}, {At: base.Add(80 * time.Second), Task: "b", Kind: "done", Holder: "w"}}
	workers := map[string]*WorkerMetrics{"w": {Worker: "w"}}
	workerIntervals(log, workers, base.Add(100*time.Second), 30*time.Second, "")
	w := workers["w"]
	if w.WorkingSeconds != 60 || w.IdleSeconds != 40 || w.StaleSeconds != 0 {
		t.Fatal(w)
	}
	workers = map[string]*WorkerMetrics{"w": {Worker: "w"}}
	workerIntervals(log, workers, base.Add(100*time.Second), 10*time.Second, "")
	if workers["w"].StaleSeconds != 30 {
		t.Fatal(workers["w"])
	}
}

func TestMergedReviewListLinks(t *testing.T) {
	files := map[string][]byte{}
	now := time.Now().UTC()
	appendEvent(files, reviewPath("feature--a"), Event{TS: now, Actor: "author", Event: "requested", Task: "a", Branch: "feature/a", Head: "head-sha"})
	appendEvent(files, reviewPath("feature--a"), Event{TS: now, Actor: "author", Event: "merged", Head: "merge-sha"})
	result, _, e := boardOperation(options{command: "review"}, "list", "", snapshot{state: State{Files: files}}, now)
	if e != nil {
		t.Fatal(e)
	}
	review := result.([]Review)[0]
	if review.Events != nil {
		t.Fatal("list contains history")
	}
	linked := (forgeLinks{base: "https://forge.test/repo", branch: "HEAD"}).review(review, Catalog{})
	if !strings.HasSuffix(linked.Links["merged"], "/merge-sha") || !strings.HasSuffix(linked.Links["head"], "/head-sha") {
		t.Fatal(linked)
	}
}

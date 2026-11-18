package bw

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bubblelist "charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func press(m tuiModel, k string) (tuiModel, tea.Cmd) {
	code := rune(0)
	text := ""
	if len(k) == 1 {
		code, text = rune(k[0]), k
	} else {
		switch k {
		case "tab":
			code = tea.KeyTab
		case "enter":
			code = tea.KeyEnter
		case "esc":
			code = tea.KeyEscape
		case "down":
			code = tea.KeyDown
		case "left":
			code = tea.KeyLeft
		case "right":
			code = tea.KeyRight
		}
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: code, Text: text})
	return next.(tuiModel), cmd
}

func TestFilterOwnsShortcutLetters(t *testing.T) {
	m := fixtureTUI()
	m, _ = press(m, "/")
	if m.items.FilterState() != bubblelist.Filtering {
		t.Fatal("filter did not open")
	}
	for _, k := range []string{"q", "j", "?", "r", "1", "2", "3"} {
		var cmd tea.Cmd
		m, cmd = press(m, k)
		if cmd != nil {
			if _, quit := cmd().(tea.QuitMsg); quit {
				t.Fatalf("typing %q quit the app", k)
			}
		}
	}
	if m.items.FilterInput.Value() != "qj?r123" || m.help.ShowAll {
		t.Fatal("shortcut intercepted text")
	}
}

func TestNarrowDetailPreservesSelectionAndScroll(t *testing.T) {
	m := fixtureTUI()
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	m = next.(tuiModel)
	m, _ = press(m, "enter")
	m, _ = press(m, "down")
	offset := m.preview.YOffset()
	if offset == 0 {
		t.Fatal("detail did not scroll")
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "enter")
	if m.selectedID != "task:queue" || m.preview.YOffset() != offset {
		t.Fatal("drill-down lost context")
	}
	if !strings.Contains(m.View().Content, "DETAIL FOCUSED") {
		t.Fatal("focused view lacks status")
	}
}

func TestFramesFitAcrossSizesAndHelpModes(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {100, 30}, {80, 24}, {60, 24}, {24, 8}, {12, 4}, {1, 1}} {
		for _, detail := range []bool{false, true} {
			for _, expanded := range []bool{false, true} {
				m := fixtureTUI()
				m.detail, m.help.ShowAll = detail, expanded
				next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m = next.(tuiModel)
				content := m.View().Content
				if lipgloss.Width(content) > size[0] || lipgloss.Height(content) > size[1] {
					t.Fatalf("frame overflows %vx%v detail=%v expanded=%v", size[0], size[1], detail, expanded)
				}
				if size[0] >= 60 && size[1] >= 24 && !strings.Contains(content, "FOCUSED") {
					t.Fatal("status clipped at a supported working size")
				}
			}
		}
	}
}

func fixtureTUI() tuiModel {
	m := newTUI("/synthetic/project", func() tuiResult { return tuiResult{} })
	m.loading = false
	m.data = monitorData{Source: "origin@123456", Tasks: TaskList{{View: View{Task: Task{ID: "queue", Title: "Queue overview", Ref: strings.Repeat("long plan reference ", 80)}, State: "ready"}}, {View: View{Task: Task{ID: "second", Title: "Second task"}, State: "waiting"}}}, Summary: Summary{Counts: map[string]int{"ready": 1, "waiting": 1}}}
	m.rebuild()
	return m
}

func TestTUIRefreshPreservesSelectionAndErrors(t *testing.T) {
	m := fixtureTUI()
	m, _ = press(m, "down")
	selected := m.selectedID
	data := m.data
	data.Tasks = TaskList{data.Tasks[1], data.Tasks[0]}
	next, _ := m.Update(tuiResult{data: data})
	m = next.(tuiModel)
	if m.selectedID != selected {
		t.Fatal("refresh changed selection", m.selectedID, selected)
	}
	last := m.last
	next, _ = m.Update(tuiResult{err: fmt.Errorf("offline")})
	m = next.(tuiModel)
	if len(m.data.Tasks) != 2 || m.err == nil || m.last != last {
		t.Fatal("lost good snapshot")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(tuiModel)
	if !strings.Contains(m.View().Content, "REFRESH FAILED") {
		t.Fatal("failure hidden")
	}
	next, cmd := m.Update(tuiTick(m.timer - 1))
	m = next.(tuiModel)
	if cmd != nil || m.loading {
		t.Fatal("old timer triggered a refresh")
	}
	next, cmd = m.Update(tuiTick(m.timer))
	m = next.(tuiModel)
	if cmd == nil || !m.loading {
		t.Fatal("retry not started")
	}
	_, cmd = m.Update(tuiTick(m.timer))
	if cmd != nil {
		t.Fatal("overlapping reads")
	}
}

func TestTUIFilteredRefreshAndOldResults(t *testing.T) {
	m := fixtureTUI()
	m, _ = press(m, "/")
	m, cmd := press(m, "q")
	// Process the asynchronous filter/cursor batch, but not recurring cursor ticks.
	var deliver func(tea.Cmd)
	deliver = func(c tea.Cmd) {
		if c == nil {
			return
		}
		msg := c()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				deliver(c)
			}
			return
		}
		next, _ := m.Update(msg)
		m = next.(tuiModel)
	}
	deliver(cmd)
	m, _ = press(m, "enter")
	selected := m.selectedID
	next, cmd := m.Update(tuiResult{data: m.data})
	m = next.(tuiModel)
	deliver(cmd) // also executes the bounded 5s tick; no I/O is executed
	if m.selectedID != selected {
		t.Fatal("filtered refresh lost selection", m.selectedID, selected)
	}
	epoch := m.listEpoch
	next, _ = m.Update(tuiListMsg{epoch - 1, m.items.FilterValue(), bubblelist.FilterMatchesMsg{}})
	m = next.(tuiModel)
	if m.selectedID != selected {
		t.Fatal("stale filter result replaced selection")
	}
}

func TestMonitorReadOnlyAndUpdates(t *testing.T) {
	root := fixture(t, `{"tasks":[{"id":"a","title":"A"},{"id":"b","title":"B","needs":["a"]}]}`)
	publishCatalog(t, root)
	invoke(t, root, 0, "claim", "a", "--as", "worker")
	invoke(t, root, 0, "ask", "a", "Which path?", "--as", "worker")
	invoke(t, root, 0, "note", "a", "Waiting for input", "--as", "worker")
	before := mustGit(t, root, "ls-remote", "origin", stateRef)
	refs := mustGit(t, root, "show-ref")
	data, err := readMonitor(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Summary.Questions) != 1 || len(data.Notes) != 1 || data.Summary.Counts["claimed"] != 1 || data.Tasks[1].BlockedBy[0].ID != "a" {
		t.Fatal(data)
	}
	if before != mustGit(t, root, "ls-remote", "origin", stateRef) || refs != mustGit(t, root, "show-ref") {
		t.Fatal("monitor changed refs")
	}
	invoke(t, root, 0, "done", "a", "--as", "worker")
	data = readData[monitorData](t, invoke(t, root, 0, "tui"))
	if data.Summary.Counts["done"] != 1 || data.Summary.Counts["ready"] != 1 {
		t.Fatal(data)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readMonitor(ctx, root); err == nil {
		t.Fatal("cancelled read succeeded")
	}
}

func TestMonitorStalenessAndTerminalText(t *testing.T) {
	now := time.Now().UTC()
	snap := snapshot{catalog: Catalog{Tasks: []Task{{ID: "a", Title: "A"}}}, state: State{Tasks: map[string]Mark{"a": {Holder: "worker", Since: now.Add(-5 * time.Hour)}}, Files: map[string][]byte{}}}
	data, err := monitorSnapshot(snap, now)
	if err != nil || data.Stale != 1 {
		t.Fatal(data, err)
	}
	if got := tuiText("hello\x1b[31m red\x1b[0m\nnext\x1b]52;c;bad\a"); got != "hello red next" {
		t.Fatal(got)
	}
}

func TestTUIEmptyErrorAndUnicodeFrames(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, page := range []int{0, 1, 2} {
			m := fixtureTUI()
			m.setTheme(dark)
			m.page = page
			m.data = monitorData{}
			m.rebuild()
			m.err = fmt.Errorf("Cannot fetch: %s", strings.Repeat("界é", 100))
			next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
			m = next.(tuiModel)
			content := m.View().Content
			if lipgloss.Width(content) > 60 || lipgloss.Height(content) > 24 || !strings.Contains(ansi.Strip(content), "q quit") {
				t.Fatal("frame/quit visibility", content)
			}
		}
	}
}

func TestTUISectionArrows(t *testing.T) {
	m := fixtureTUI()
	for _, step := range []struct {
		key  string
		page int
	}{
		{"right", 1}, {"right", 2}, {"right", 3}, {"right", 4}, {"right", 5}, {"right", 0}, {"left", 5}, {"left", 4}, {"left", 3}, {"left", 2}, {"left", 1}, {"left", 0},
	} {
		m.detail = true
		m, _ = press(m, step.key)
		if m.page != step.page || m.detail {
			t.Fatalf("%s: page=%d detail=%v", step.key, m.page, m.detail)
		}
	}
	m, _ = press(m, "/")
	m, _ = press(m, "a")
	m, _ = press(m, "b")
	m, _ = press(m, "left")
	m, _ = press(m, "x")
	if m.page != 0 || m.items.FilterInput.Value() != "axb" {
		t.Fatal("left arrow did not edit filter", m.page, m.items.FilterInput.Value())
	}
	m, _ = press(m, "right")
	m, _ = press(m, "y")
	if m.page != 0 || m.items.FilterInput.Value() != "axby" {
		t.Fatal("right arrow did not edit filter", m.page, m.items.FilterInput.Value())
	}
}

func questionTUI() tuiModel {
	m := fixtureTUI()
	m.page = 1
	m.answerAs = "lead"
	m.data.Questions = []Question{{Event: Event{ID: 1, Task: "queue", Note: "Which option?", Actor: "worker"}}}
	m.rebuild()
	return m
}
func TestTUIAnswerDraftAndSubmission(t *testing.T) {
	m := questionTUI()
	calls := 0
	m.saveAnswer = func(id int, text string) error {
		calls++
		if id != 1 || text != "q1a" {
			t.Fatalf("wrong answer %d %q", id, text)
		}
		return fmt.Errorf("coordinator required")
	}
	m, _ = press(m, "a")
	for _, k := range []string{"q", "1", "a", "left", "right"} {
		m, _ = press(m, k)
	}
	if m.page != 1 || m.answering != 1 || m.editor.Value() != "q1a" {
		t.Fatal("editor lost key ownership")
	}
	submit := tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	next, cmd := m.Update(submit)
	m = next.(tuiModel)
	if !m.saving || cmd == nil {
		t.Fatal("not submitting")
	}
	_, duplicate := m.Update(submit)
	if duplicate != nil {
		t.Fatal("duplicate submit")
	}
	next, _ = m.Update(cmd())
	m = next.(tuiModel)
	if calls != 1 || m.answerErr == nil || m.editor.Value() != "q1a" || m.answering != 1 {
		t.Fatal("failed draft lost")
	}
	m.saveAnswer = func(int, string) error { return nil }
	next, cmd = m.Update(submit)
	m = next.(tuiModel)
	next, _ = m.Update(cmd())
	m = next.(tuiModel)
	if m.answering != 0 || !m.history || !strings.Contains(m.notice, "saved") {
		t.Fatal("success not reflected")
	}
	m = questionTUI()
	m, _ = press(m, "a")
	m, _ = press(m, "esc")
	if m.answering != 0 {
		t.Fatal("cancel failed")
	}
}
func TestTUIBoardHistoryAndAnswerAuthorization(t *testing.T) {
	root := fixture(t, `{"coordinators":["lead"],"tasks":[{"id":"a","title":"A"}]}`)
	publishCatalog(t, root)
	invoke(t, root, 0, "claim", "a", "--as", "worker")
	invoke(t, root, 0, "ask", "a", "Which option?", "--as", "worker")
	if err := saveMonitorAnswer(root, "other", 1, "A"); err == nil {
		t.Fatal("unauthorized answer succeeded")
	}
	if err := saveMonitorAnswer(root, "lead", 1, "A\nBecause it fits."); err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, root, "ls-remote", "origin", stateRef)
	if err := saveMonitorAnswer(root, "lead", 1, "A\nBecause it fits."); err != nil {
		t.Fatal(err)
	}
	if before != mustGit(t, root, "ls-remote", "origin", stateRef) {
		t.Fatal("retry rewrote state")
	}
	invoke(t, root, 0, "review", "request", "a", "--branch", "feature/a", "--as", "worker")
	invoke(t, root, 0, "review", "take", "feature--a", "--as", "reviewer")
	notes := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(notes, []byte("Fix edge case.\nAdd coverage."), 0600); err != nil {
		t.Fatal(err)
	}
	invoke(t, root, 0, "review", "changes", "feature--a", "--notes", notes, "--as", "reviewer")
	data, err := readMonitor(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Questions) != 1 || len(data.Questions[0].Answers) != 1 || len(data.Reviews) != 1 || data.Reviews[0].Findings == "" {
		t.Fatal(data)
	}
	m := fixtureTUI()
	m.page = 1
	m.data = data
	m.rebuild()
	if len(m.items.Items()) != 1 {
		t.Fatal("answered question visible by default")
	}
	if !strings.Contains(m.items.Items()[0].(tuiEntry).body, "Fix edge case.") {
		t.Fatal("findings missing")
	}
	m, _ = press(m, "h")
	if len(m.items.Items()) != 2 || !strings.Contains(m.items.Items()[0].(tuiEntry).body, "Because it fits.") {
		t.Fatal("history missing")
	}
}
func TestTUIAnswerFrames(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {100, 30}, {80, 24}, {60, 24}, {24, 12}, {12, 4}} {
		m := questionTUI()
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(tuiModel)
		m, _ = press(m, "a")
		content := m.View().Content
		if lipgloss.Width(content) > size[0] || lipgloss.Height(content) > size[1] {
			t.Fatal("answer overflow", size)
		}
		if size[0] >= 60 && !strings.Contains(content, "ctrl+s submit") {
			t.Fatal("submit hint hidden")
		}
	}
}

func TestTUIFilteredRefreshDuringAnswer(t *testing.T) {
	m := questionTUI()
	m.items.SetFilterText("Question")
	cmd := m.rebuild()
	next, _ := m.Update(cmd())
	m = next.(tuiModel)
	m, _ = press(m, "a")
	m.editor.SetValue("Keep this draft")
	cmd = m.rebuild()
	next, _ = m.Update(cmd())
	m = next.(tuiModel)
	if len(m.items.VisibleItems()) != 1 || m.answering != 1 || m.editor.Value() != "Keep this draft" {
		t.Fatal("background filter interrupted answer or lost selection")
	}
}

func TestAgentsTaskDrilldown(t *testing.T) {
	m := fixtureTUI()
	m.data.Workers = WorkerList{{Worker: "synthetic", Activity: "working", Tasks: []WorkerTask{{ID: "api", Title: "API", ClaimPeriod: ClaimPeriod{Outcome: "still held"}, Steps: []StepMetric{{Text: "Build", Known: true, Seconds: 120}}}}}}
	m, _ = press(m, "6")
	m, _ = press(m, "enter")
	if m.workerName != "synthetic" || m.detail || m.items.Title != "Agents / synthetic" {
		t.Fatal("agent did not open task list")
	}
	m, _ = press(m, "down")
	m, _ = press(m, "enter")
	if !m.detail || !strings.Contains(m.preview.GetContent(), "Build") {
		t.Fatal("task did not expand steps")
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "esc")
	if m.workerName != "" || m.items.Title != "Agents" {
		t.Fatal("back did not return to agents")
	}
}

func TestPlanDescriptionRefreshKeepsFooterVisible(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {100, 30}, {80, 24}, {60, 24}} {
		m := fixtureTUI()
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(tuiModel)
		data := m.data
		data.Description = "A synthetic plan description added after the first snapshot."
		next, _ = m.Update(tuiResult{data: data})
		m = next.(tuiModel)
		for page := 0; page < 6; page++ {
			m.page = page
			m.rebuild()
			content := m.View().Content
			if lipgloss.Width(content) > size[0] || lipgloss.Height(content) > size[1] || !strings.Contains(ansi.Strip(content), "q quit") {
				t.Fatalf("description refresh clipped footer at %v page %d", size, page)
			}
		}
	}
}

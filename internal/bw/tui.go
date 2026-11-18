package bw

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	bubblelist "charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The monitor uses a filterable list and a stable, scrollable detail pane.
type tuiEntry struct{ id, title, description, body string }

func (e tuiEntry) Title() string       { return e.title }
func (e tuiEntry) Description() string { return e.description }
func (e tuiEntry) FilterValue() string { return e.title + " " + e.description }

type bindings struct{ quit, interrupt, next, open, back, filter, more, pages, refresh, answer, history key.Binding }

func newBindings() bindings {
	bind := func(keys []string, label, description string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(label, description))
	}
	return bindings{
		quit: bind([]string{"q"}, "q", "quit"), interrupt: bind([]string{"ctrl+c"}, "ctrl+c", "quit"),
		next: bind([]string{"tab", "shift+tab"}, "tab", "focus"), open: bind([]string{"enter"}, "enter", "detail"),
		back: bind([]string{"esc"}, "esc", "back"), filter: bind([]string{"/"}, "/", "filter"),
		more:    bind([]string{"?"}, "?", "help"),
		pages:   bind([]string{"1", "2", "3", "4", "5", "6", "left", "right"}, "←/→, 1–6", "sections"),
		refresh: bind([]string{"r"}, "r", "refresh"),
		answer:  bind([]string{"a"}, "a", "answer question"),
		history: bind([]string{"h"}, "h", "board history"),
	}
}
func (k bindings) ShortHelp() []key.Binding {
	return []key.Binding{k.quit, k.filter, k.next, k.open, k.back, k.more}
}
func (k bindings) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.next, k.open, k.back}, {k.filter, k.more}, {k.pages, k.refresh}, {k.answer, k.history}, {k.quit, k.interrupt}}
}

type palette struct{ title, muted, focus, success, warning, ready, surface, badge lipgloss.Style }
type tuiModel struct {
	width, height int
	detail        bool
	selectedID    string
	workerName    string
	items         bubblelist.Model
	preview       viewport.Model
	help          help.Model
	keys          bindings
	styles        palette
	root          string
	load          func() tuiResult
	data          monitorData
	loading       bool
	last          time.Time
	err           error
	page          int
	body          string
	timer         int
	listEpoch     int
	pendingID     string
	history       bool
	editor        textarea.Model
	answering     int
	answerTitle   string
	answerAs      string
	saveAnswer    func(int, string) error
	saving        bool
	answerErr     error
	notice        string
}

func newTUI(root string, load func() tuiResult) tuiModel {
	items := []bubblelist.Item{}
	l := bubblelist.New(items, bubblelist.NewDefaultDelegate(), 0, 0)
	l.Title = "Tasks"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	p := viewport.New(viewport.WithWidth(1), viewport.WithHeight(1))
	p.SoftWrap = true
	p.MouseWheelEnabled = false // Preserve native terminal text selection.
	editor := textarea.New()
	editor.Placeholder = "Write your answer…"
	editor.ShowLineNumbers = false
	editor.CharLimit = 16000
	m := tuiModel{editor: editor, root: root, load: load, loading: true, items: l, preview: p, help: help.New(), keys: newBindings()}
	m.setTheme(true) // Usable even when the terminal does not answer the query.
	m.syncDetail()
	return m
}

func (m *tuiModel) setTheme(dark bool) {
	choose := lipgloss.LightDark(dark)
	accent := choose(lipgloss.Color("#9B4D00"), lipgloss.Color("#EBC06B"))
	muted := choose(lipgloss.Color("#555555"), lipgloss.Color("#AAAAAA"))
	m.styles = palette{
		title:   lipgloss.NewStyle().Bold(true),
		muted:   lipgloss.NewStyle().Foreground(muted),
		focus:   lipgloss.NewStyle().Foreground(accent).Bold(true),
		success: lipgloss.NewStyle().Foreground(choose(lipgloss.Color("#237A57"), lipgloss.Color("#83C9A5"))),
		warning: lipgloss.NewStyle().Foreground(choose(lipgloss.Color("#B34437"), lipgloss.Color("#EF9585"))),
		ready:   lipgloss.NewStyle().Foreground(choose(lipgloss.Color("#286E9A"), lipgloss.Color("#91BDD7"))),
		surface: lipgloss.NewStyle().Background(choose(lipgloss.Color("#EEEAE1"), lipgloss.Color("#292822"))),
		badge:   lipgloss.NewStyle().Foreground(choose(lipgloss.Color("#FFFFFF"), lipgloss.Color("#201D17"))).Background(accent).Bold(true).Padding(0, 1),
	}
	d := bubblelist.NewDefaultDelegate()
	d.Styles = bubblelist.NewDefaultItemStyles(dark)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(accent).BorderForeground(accent)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(accent).BorderForeground(accent)
	m.items.SetDelegate(tuiDelegate{d, m.styles})
	m.items.Styles.Title = m.styles.focus
	m.help.Styles.ShortKey = m.styles.focus
	m.help.Styles.ShortDesc = m.styles.muted
}

func (m tuiModel) Init() tea.Cmd { return tea.Batch(tea.RequestBackgroundColor, m.refresh()) }

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tagged, ok := msg.(tuiListMsg); ok {
		if tagged.epoch != m.listEpoch || tagged.query != m.items.FilterValue() {
			return m, nil
		}
		msg = tagged.msg
	}
	switch msg := msg.(type) {
	case tuiAnswerResult:
		m.saving = false
		m.answerErr = msg.err
		if msg.err != nil {
			return m, nil
		}
		m.answering = 0
		m.editor.Blur()
		m.history = true
		m.notice = fmt.Sprintf("Answer to question #%d saved", msg.id)
		// Keep the answered question available after refresh.
		if !m.loading {
			m.loading = true
			return m, m.refresh()
		}
		return m, nil
	case tuiTick:
		if int(msg) != m.timer {
			return m, nil
		}
		if !m.loading {
			m.loading = true
			return m, m.refresh()
		}
		return m, nil
	case tuiResult:
		m.loading, m.err = false, msg.err
		var cmd tea.Cmd
		if msg.err == nil {
			m.data, m.last = msg.data, time.Now()
			m.resize()
			cmd = m.rebuild()
		}
		m.timer++
		timer := m.timer
		return m, tea.Batch(cmd, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return tuiTick(timer) }))
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.resize()
		return m, nil
	case tea.BackgroundColorMsg:
		m.setTheme(msg.IsDark())
		m.body = ""
		m.syncDetail()
		return m, nil
	case tea.KeyPressMsg:
		if m.answering != 0 {
			if m.saving {
				return m, nil
			}
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.answering = 0
				m.editor.Blur()
				return m, nil
			case "ctrl+s":
				text := strings.TrimSpace(m.editor.Value())
				if text == "" {
					m.answerErr = fmt.Errorf("Write an answer before submitting")
					return m, nil
				}
				if m.saveAnswer == nil {
					m.answerErr = fmt.Errorf("Answering is unavailable")
					return m, nil
				}
				id, save := m.answering, m.saveAnswer
				m.saving = true
				m.answerErr = nil
				return m, func() tea.Msg { return tuiAnswerResult{id, save(id, text)} }
			}
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			return m, cmd
		}
		if key.Matches(msg, m.keys.interrupt) {
			return m, tea.Quit
		}
		// Let the editor own printable shortcut letters and its Enter/Esc.
		if !m.detail && m.items.FilterState() == bubblelist.Filtering {
			var cmd tea.Cmd
			m.items, cmd = m.items.Update(msg)
			m.syncDetail()
			return m, tagListCmd(cmd, m.listEpoch, m.items.FilterValue())
		}
		switch {
		case key.Matches(msg, m.keys.answer):
			if strings.HasPrefix(m.selectedID, "question:") {
				id, err := strconv.Atoi(strings.TrimPrefix(m.selectedID, "question:"))
				if err != nil {
					return m, nil
				}
				m.answering = id
				m.answerErr = nil
				m.editor.Reset()
				if item, ok := m.items.SelectedItem().(tuiEntry); ok {
					m.answerTitle = item.description
				}
				m.resize()
				return m, m.editor.Focus()
			}
			return m, nil
		case key.Matches(msg, m.keys.history) && m.page == 1:
			m.history = !m.history
			return m, m.rebuild()
		case key.Matches(msg, m.keys.refresh):
			if !m.loading {
				m.loading = true
				return m, m.refresh()
			}
			return m, nil
		case key.Matches(msg, m.keys.pages):
			m.workerName = ""
			switch msg.String() {
			case "left":
				m.page = (m.page + 5) % 6
			case "right":
				m.page = (m.page + 1) % 6
			default:
				m.page = int(msg.String()[0] - '1')
			}
			m.detail = false
			m.items.ResetFilter()
			return m, m.rebuild()
		case key.Matches(msg, m.keys.quit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.more):
			m.help.ShowAll = !m.help.ShowAll
			m.resize()
			return m, nil
		case key.Matches(msg, m.keys.next):
			m.detail = !m.detail
			return m, nil
		case key.Matches(msg, m.keys.open):
			if m.page == 5 && m.workerName == "" && strings.HasPrefix(m.selectedID, "worker:") {
				m.workerName = strings.TrimPrefix(m.selectedID, "worker:")
				m.selectedID = ""
				m.detail = false
				return m, m.rebuild()
			}
			m.detail = true
			return m, nil
		case key.Matches(msg, m.keys.back) && m.detail:
			m.detail = false
			return m, nil
		case key.Matches(msg, m.keys.back) && m.page == 5 && m.workerName != "":
			m.workerName = ""
			m.selectedID = ""
			return m, m.rebuild()
		case key.Matches(msg, m.keys.filter):
			m.detail = false
		}
		if m.detail {
			var cmd tea.Cmd
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		}
	}
	if m.answering != 0 {
		// Background list filtering must finish even while the editor owns input.
		if _, filtered := msg.(bubblelist.FilterMatchesMsg); !filtered {
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			return m, cmd
		}
	}
	// Forward filter-result and cursor messages even when the list is hidden.
	var cmd tea.Cmd
	m.items, cmd = m.items.Update(msg)
	if _, ok := msg.(bubblelist.FilterMatchesMsg); ok && m.pendingID != "" {
		m.restoreSelection(m.pendingID)
		m.pendingID = ""
	}
	m.syncDetail()
	return m, tagListCmd(cmd, m.listEpoch, m.items.FilterValue())
}

func (m *tuiModel) syncDetail() {
	e, ok := m.items.SelectedItem().(tuiEntry)
	if !ok {
		{
			text := "No matching items.\n\nEsc clears the filter."
			if len(m.items.Items()) == 0 {
				text = []string{"No tasks in this catalog.", "No unanswered questions, stale/removed claims or unmerged reviews. Press h for history.", "No notes recorded.", "Nothing left.", "No plan shape.", "No agents recorded."}[m.page]
				if m.last.IsZero() {
					text = "Waiting for the first snapshot from origin."
				}
			}
			m.preview.SetContent(text)
			m.preview.GotoTop()
			m.selectedID = "no-match"
		}
		return
	}
	if e.id != m.selectedID || e.body != m.body {
		changed := e.id != m.selectedID
		m.body = e.body
		m.preview.SetContent(m.detailContent(e.body))
		if changed {
			m.preview.GotoTop()
		}
		m.selectedID = e.id
	}
}

func (m tuiModel) regions() (left, right, body int) {
	extra := 0
	if m.data.Description != "" {
		extra = 1
	}
	body = max(1, m.height-7-extra-lipgloss.Height(m.help.View(m.keys)))
	if m.width < 100 {
		return m.width, m.width, body
	}
	left = (m.width - 3) * 2 / 5
	return left, m.width - left - 3, body
}

func (m *tuiModel) resize() {
	m.editor.SetWidth(max(1, m.width-4))
	m.editor.SetHeight(max(1, m.height-9))
	m.help.SetWidth(m.width)
	l, r, h := m.regions()
	m.items.SetSize(max(1, l), h)
	m.preview.SetWidth(max(1, r))
	m.preview.SetHeight(max(1, h-1))
}

func (m tuiModel) View() tea.View {
	v := tea.NewView("")
	v.AltScreen, v.WindowTitle = true, "Bullwhip"
	if m.width == 0 {
		v.SetContent("Loading Bullwhip...")
		return v
	}
	fit := func(s string) string {
		return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(s)
	}
	if m.width < 24 || m.height < 12 {
		v.SetContent(fit("Small terminal\nq / ctrl+c: quit"))
		return v
	}
	if m.answering != 0 {
		return m.answerView()
	}
	l, r, h := m.regions()
	title := "DETAIL  /  preview"
	if m.detail {
		title = "DETAIL  /  focused"
	}
	left := lipgloss.NewStyle().Width(l).Height(h).Render(m.items.View())
	right := lipgloss.NewStyle().Width(r).Height(h).Render(m.styles.focus.Render(title) + "\n" + m.preview.View())
	body := left
	if m.width >= 100 {
		divider := m.styles.muted.Render(strings.Repeat(" │ \n", max(0, h-1)) + " │ ")
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
	} else if m.detail {
		body = right
	}
	status := "LIST FOCUSED | ↑/↓ select · enter details · r refresh"
	if m.detail {
		status = "DETAIL FOCUSED | arrows / pgup / pgdown scroll | esc back"
	} else if m.items.FilterState() == bubblelist.Filtering {
		status = "FILTERING | type query | enter apply | esc cancel | ctrl+c quit"
	}
	freshness := "Connecting to origin..."
	if !m.last.IsZero() {
		freshness = "Updated " + m.last.Format("15:04:05") + " | catalog: " + m.data.Source
	}
	if m.err != nil {
		freshness = "REFRESH FAILED: " + tuiText(m.err.Error())
		if !m.last.IsZero() {
			freshness = "REFRESH FAILED | last good " + m.last.Format("15:04:05") + " | " + tuiText(m.err.Error())
		}
	}
	line := func(s string) string { return ansi.Truncate(s, m.width, "…") }
	header := m.styles.badge.Render("BW") + " " + m.styles.focus.Render("BULLWHIP") + m.styles.muted.Render("  /  ") + m.styles.title.Render(tuiText(filepath.Base(m.root)))
	mode := m.styles.muted.Render("MONITOR")
	gap := max(1, m.width-lipgloss.Width(header)-lipgloss.Width(mode))
	header += strings.Repeat(" ", gap) + mode
	counts := m.data.Summary.Counts
	total := counts["done"] + counts["claimed"] + counts["ready"] + counts["waiting"]
	progress := "Waiting for the first snapshot — counts unavailable"
	if !m.last.IsZero() {
		filled := 0
		if total > 0 {
			filled = 12 * counts["done"] / total
		}
		progress = m.styles.success.Render(strings.Repeat("━", filled)) + m.styles.muted.Render(strings.Repeat("─", 12-filled)) + fmt.Sprintf("  %d/%d done", counts["done"], total)
		progress += "   " + m.styles.focus.Render(fmt.Sprintf("%d claimed", counts["claimed"])) + "  " + m.styles.ready.Render(fmt.Sprintf("%d ready", counts["ready"])) + "  " + m.styles.muted.Render(fmt.Sprintf("%d waiting", counts["waiting"]))
		if m.data.Summary.AllDone {
			progress = m.styles.success.Render("✓ ALL TASKS DONE") + fmt.Sprintf("  %d/%d", counts["done"], total)
		}
	}
	tabs := []string{}
	for i, name := range []string{"Tasks", "Attention", "Notes", "Left", "Shape", "Agents"} {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if i == m.page {
			label = m.styles.badge.Render(label)
		} else {
			label = m.styles.muted.Render(label)
		}
		tabs = append(tabs, label)
	}
	description := ""
	if m.data.Description != "" {
		description = line(m.styles.muted.Render(tuiText(m.data.Description))) + "\n"
	}
	attention := fmt.Sprintf("STALE %d  ·  QUESTIONS %d  ·  REVIEWS %d", m.data.Stale, len(m.data.Summary.Questions), len(m.data.Summary.Reviews))
	if m.data.Resources.Status == "low_resources" || m.data.Resources.Status == "critical_resources" {
		attention = strings.ToUpper(m.data.Resources.Status) + " · " + m.data.Resources.Reason
	}
	if m.data.Resources.Status == "low_resources" || m.data.Resources.Status == "critical_resources" || m.data.Stale > 0 || len(m.data.Summary.Questions) > 0 {
		attention = m.styles.warning.Render(attention)
	} else {
		attention = m.styles.muted.Render(attention)
	}
	if m.last.IsZero() {
		attention = m.styles.muted.Render("Connecting to origin…")
	}
	if m.notice != "" && m.err == nil {
		freshness = m.notice + " | " + freshness
	}
	if m.page == 1 && m.items.FilterState() != bubblelist.Filtering {
		status = "LIST FOCUSED | enter details · a answer · h history"
		if m.detail {
			status = "DETAIL FOCUSED | a answer · h history · esc back"
		}
	}
	freshStyle := m.styles.muted
	if m.err != nil {
		freshStyle = m.styles.warning
	}
	footer := m.help.View(m.keys)
	if m.items.FilterState() == bubblelist.Filtering && !m.detail {
		footer = m.styles.muted.Render("enter apply  ·  esc cancel  ·  ctrl+c quit")
	}
	v.SetContent(fit(line(header) + "\n" + description + line(progress) + "\n" + line(attention) + "\n" + strings.Join(tabs, " ") + "\n" + m.styles.muted.Render(strings.Repeat("─", m.width)) + "\n" + body + "\n" + line(freshStyle.Render(freshness)) + "\n" + line(m.styles.muted.Render(status)) + "\n" + footer))
	return v
}

// Board content is untrusted terminal text, never terminal control sequences.
func tuiText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, ansi.Strip(s))
}

type tuiTick int
type tuiResult struct {
	data monitorData
	err  error
}

func (m tuiModel) refresh() tea.Cmd { return func() tea.Msg { return m.load() } }

func (m *tuiModel) rebuild() tea.Cmd {
	id := m.selectedID
	items := []bubblelist.Item{}
	add := func(id, title, desc, body string) {
		items = append(items, tuiEntry{id, tuiText(title), tuiText(desc), body})
	}
	switch m.page {
	case 0:
		m.items.Title = "Tasks"
		for _, r := range m.data.Tasks {
			desc := strings.ToUpper(activityLabel(r.View))
			if r.Progress != "" {
				desc += " · " + r.Progress
			}
			if r.Holder != "" {
				desc += " · " + r.Holder
			}
			if r.Stale {
				desc += " · STALE"
			}
			body := tuiText(r.ID+" — "+r.Title) + "\n\n" + tuiText(desc) + "\nPriority: " + fmt.Sprint(r.Priority) + metadataText(r.View)
			if r.Holder != "" {
				body += "\nHolder: " + tuiText(r.Holder) + "\nLast activity: " + tuiText(r.Idle) + " ago · " + tuiText(r.LastWrite) + "\n" + tuiText(r.ReviewStatus)
			}
			if r.Stale {
				body += "\n\nSTALE: activity exceeded the configured threshold.\nClaims never expire; coordinate before releasing."
			}
			for _, b := range r.BlockedBy {
				body += "\nWaiting on: " + tuiText(b.ID+" ("+b.State+") "+b.Holder)
			}
			if r.Ref != "" {
				body += "\n\nPlan reference: " + tuiText(r.Ref)
			}
			body += "\n\nUnlocks: " + tuiText(strings.Join(r.Unlocks, ", "))
			add("task:"+r.ID, r.ID+" · "+r.Title+metadataText(r.View), desc, body)
		}
	case 1:
		m.items.Title = "Attention"
		if m.history {
			m.items.Title = "Attention / all history"
		}
		for _, q := range m.data.Questions {
			if len(q.Answers) > 0 && !m.history {
				continue
			}
			audience := "question for project team"
			if len(m.data.Coordinators) > 0 && !q.Technical {
				audience = "coordinator answer required"
			}
			if q.Technical {
				audience = "technical question"
			}
			state := "UNANSWERED"
			if len(q.Answers) > 0 {
				state = "ANSWERED"
			}
			body := fmt.Sprintf("QUESTION #%d · %s\n\n%s\n\nTask: %s\nAsked by: %s\n%s", q.ID, state, tuiMultiline(q.Note), tuiText(q.Task), tuiText(q.Actor), audience)
			for _, a := range q.Answers {
				body += "\n\nANSWER · " + tuiText(a.Actor) + " · " + a.TS.Format(time.RFC3339) + "\n" + tuiMultiline(a.Note)
			}
			body += "\n\nPress a to write an answer."
			add(fmt.Sprintf("question:%d", q.ID), fmt.Sprintf("Question #%d · %s · %s", q.ID, q.Task, state), q.Note, body)
		}
		for _, r := range m.data.Summary.Claims {
			if !r.Stale && r.Title != "(removed from tasks.json)" {
				continue
			}
			add("stale:"+r.ID, "Claim needs attention · "+r.ID, r.Holder+" · inactive "+r.Idle,
				tuiText(r.Title)+"\n\nHolder: "+tuiText(r.Holder)+"\nLast activity: "+tuiText(r.Idle)+" ago\n\nStaleness flags inactivity, not a confirmed failure.\nClaims never expire. Coordinate with the holder before release.")
		}
		for _, r := range m.data.Reviews {
			if r.State == "merged" && !m.history {
				continue
			}
			reviewer := r.Reviewer
			if reviewer == "" {
				reviewer = "awaiting reviewer"
			}
			body := "REVIEW\n\n" + tuiText(r.ID) + "\nState: " + tuiText(r.State) + "\nAuthor: " + tuiText(r.Author) + "\nReviewer: " + tuiText(reviewer) + "\nBranch: " + tuiText(r.Branch) + "\nHead: " + tuiText(r.Head)
			if r.Findings != "" {
				body += "\n\nFINDINGS\n" + tuiMultiline(r.Findings)
			}
			body += "\n\nHISTORY"
			for _, e := range r.Events {
				body += "\n" + e.TS.Format(time.RFC3339) + " · " + tuiText(e.Actor) + " · " + tuiText(e.Event)
				if e.Note != "" {
					body += "\n" + tuiMultiline(e.Note)
				}
			}
			add("review:"+r.ID, "Review · "+r.Task, r.State+" · "+reviewer, body)
		}
	case 3:
		m.items.Title = "What's left"
		for _, r := range m.data.Todo.Tasks {
			body := renderView(r.View)
			for _, b := range r.BlockedBy {
				body += "\nWaiting on: " + tuiText(b.ID+" "+b.State+" "+b.Holder)
			}
			add("todo:"+r.ID, r.ID+" · "+r.Title+metadataText(r.View), strings.ToUpper(activityLabel(r.View))+" · "+r.Holder, body)
		}
	case 4:
		m.items.Title = "Plan & resources"
		add("shape", "Plan shape", fmt.Sprintf("Suggested crew %d · useful now %d", m.data.Shape.SuggestedCrew, m.data.Summary.UsefulWorkers), tuiText(m.data.Description)+"\n\n"+shapeText(m.data.Shape))
		add("resources", "Resources", m.data.Resources.Status, resourceText(m.data.Resources))
	case 5:
		m.items.Title = "Agents"
		for _, w := range m.data.Workers {
			if m.workerName == "" {
				add("worker:"+w.Worker, w.Worker+" · "+w.Activity, fmt.Sprintf("%d done · held %s · last %s ago", w.Completed, durationText(w.HeldSeconds), w.Idle), workersText(WorkerList{w})+"\n\nEnter to browse tasks; Enter a task to expand steps.")
				continue
			}
			if w.Worker != m.workerName {
				continue
			}
			m.items.Title = "Agents / " + tuiText(w.Worker)
			summary := w
			summary.Tasks = nil
			add("worker-summary:"+w.Worker, w.Worker+" · "+w.Activity, "Agent summary · Esc returns to Agents", workersText(WorkerList{summary}))
			for i, t := range w.Tasks {
				copy := summary
				copy.Tasks = []WorkerTask{t}
				add(fmt.Sprintf("worker-task:%s:%d", w.Worker, i), t.ID+" · "+t.Title, t.Outcome+" · "+durationText(t.Seconds)+" · Enter for steps", workersText(WorkerList{copy}))
			}
		}

	case 2:
		m.items.Title = "Notes"
		for i := len(m.data.Notes) - 1; i >= 0; i-- {
			n := m.data.Notes[i]
			add(fmt.Sprintf("note:%d", i), n.Task+" · "+n.Actor, n.Note, tuiText(n.Note)+"\n\n"+tuiText(n.Actor)+" · "+n.TS.Format(time.RFC3339))
		}
	}
	// Filtering belongs to the user. SetItems refreshes the active filter through
	// its returned command; selection is restored after the filtered result too.
	m.listEpoch++
	cmd := m.items.SetItems(items)
	if cmd != nil {
		m.pendingID = id
		return tagListCmd(cmd, m.listEpoch, m.items.FilterValue())
	}
	m.restoreSelection(id)
	m.syncDetail()
	return nil
}

func runTUI(stdout io.Writer, o options) error {
	if os.Getenv("BW_DEBUG") != "" {
		return fail(1, "Unset BW_DEBUG before opening the TUI; debug output uses the terminal.")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Finish cleanup before the CLI exits, even when quitting during a fetch.
	var mu sync.Mutex
	stopped := false
	defer func() { cancel(); mu.Lock(); stopped = true; mu.Unlock() }()
	m := newTUI(root, func() tuiResult {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return tuiResult{err: context.Canceled}
		}
		data, err := readMonitor(ctx, root)
		return tuiResult{data, err}
	})
	who, identityErr := identity(o, root)
	m.answerAs = who
	if identityErr != nil {
		m.answerAs = "unavailable — restart with --as NAME"
	}
	m.saveAnswer = func(id int, text string) error {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return context.Canceled
		}
		if identityErr != nil {
			return identityErr
		}
		return saveMonitorAnswer(root, who, id, text)
	}
	_, err = tea.NewProgram(m, tea.WithOutput(stdout)).Run()
	return err
}

func (m *tuiModel) restoreSelection(id string) {
	for i, item := range m.items.VisibleItems() {
		if item.(tuiEntry).id == id {
			m.items.Select(i)
			return
		}
	}
	m.items.Select(0)
}

type tuiListMsg struct {
	epoch int
	query string
	msg   tea.Msg
}

func tagListCmd(cmd tea.Cmd, epoch int, query string) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			wrapped := make([]tea.Cmd, len(batch))
			for i, c := range batch {
				wrapped[i] = tagListCmd(c, epoch, query)
			}
			return tea.BatchMsg(wrapped)
		}
		return tuiListMsg{epoch, query, msg}
	}
}

// Retain Bubbles' filtering/highlight behavior while giving task states their
// own semantic color. Selection still uses the same amber focus treatment.
type tuiDelegate struct {
	bubblelist.DefaultDelegate
	styles palette
}

func (d tuiDelegate) Render(w io.Writer, m bubblelist.Model, index int, item bubblelist.Item) {
	e, ok := item.(tuiEntry)
	if !ok {
		return
	}
	style := d.styles.muted
	switch {
	case strings.Contains(e.description, "STALE"):
		style = d.styles.warning
	case strings.HasPrefix(e.description, "DONE"):
		style = d.styles.success
	case strings.HasPrefix(e.description, "READY"):
		style = d.styles.ready
	case strings.HasPrefix(e.description, "IN PROGRESS"), strings.HasPrefix(e.description, "IN REVIEW"):
		style = d.styles.focus
	}
	d.Styles.NormalDesc = d.Styles.NormalDesc.Inherit(style)
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(style.GetForeground())
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Inherit(d.styles.surface)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Inherit(d.styles.surface)
	d.DefaultDelegate.Render(w, m, index, item)
}

func (m tuiModel) detailContent(body string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if i == 0 {
			lines[i] = m.styles.title.Render(line)
			continue
		}
		if strings.HasPrefix(line, "STALE:") {
			lines[i] = m.styles.warning.Render(line)
			continue
		}
		if label, value, ok := strings.Cut(line, ": "); ok {
			lines[i] = m.styles.muted.Render(label+": ") + value
		}
	}
	return strings.Join(lines, "\n")
}

type tuiAnswerResult struct {
	id  int
	err error
}

func saveMonitorAnswer(root, who string, id int, text string) error {
	_, err := executeBoard(options{command: "answer", as: who, args: []string{strconv.Itoa(id), text}}, root)
	return err
}
func tuiMultiline(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = tuiText(line)
	}
	return strings.Join(lines, "\n")
}
func (m tuiModel) answerView() tea.View {
	line := func(s string) string { return ansi.Truncate(s, m.width, "…") }
	status := "ctrl+s submit to origin · esc cancel · ctrl+c quit"
	if m.saving {
		status = "Saving answer to origin… waiting for confirmation"
	}
	note := "Answers are shared in Git history. Do not include secrets."
	if m.answerErr != nil {
		note = m.styles.warning.Render(tuiText(m.answerErr.Error()))
	}
	content := line(m.styles.badge.Render(fmt.Sprintf("ANSWER #%d", m.answering))) + "\n" + line("As: "+tuiText(m.answerAs)) + "\n" + line(tuiText(m.answerTitle)) + "\n\n" + m.editor.View() + "\n" + line(note) + "\n" + line(status)
	v := tea.NewView(lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(content))
	v.AltScreen = true
	v.WindowTitle = "Bullwhip / answer"
	return v
}

package bw

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Events are immutable. Ownership and status are folded from each review log.
type Event struct {
	Roles  []string  `json:"roles,omitempty"`
	Effort string    `json:"effort,omitempty"`
	TS     time.Time `json:"ts"`
	Actor  string    `json:"actor"`
	Event  string    `json:"event"`
	Head   string    `json:"head,omitempty"`
	Model  string    `json:"model,omitempty"`
	Note   string    `json:"note,omitempty"`
	Task   string    `json:"task,omitempty"`
	Branch string    `json:"branch,omitempty"`
	ID     int       `json:"id,omitempty"`
	// Technical questions may be answered by anyone; others need a coordinator.
	Technical bool `json:"technical,omitempty"`
}
type Review struct {
	Roles     []string          `json:"roles,omitempty"`
	MergedSHA string            `json:"merged_sha,omitempty"`
	Effort    string            `json:"effort,omitempty"`
	Links     map[string]string `json:"links,omitempty"`
	ID        string            `json:"id"`
	Task      string            `json:"task"`
	Branch    string            `json:"branch"`
	Author    string            `json:"author"`
	Reviewer  string            `json:"reviewer,omitempty"`
	Head      string            `json:"head"`
	State     string            `json:"state"`
	Self      bool              `json:"self"`
	Requested time.Time         `json:"requested"`
	Events    []Event           `json:"events,omitempty"`
	Findings  string            `json:"findings,omitempty"`
	Unchanged bool              `json:"unchanged,omitempty"`
}
type Question struct {
	Event
	Answers   []Event `json:"answers"`
	Unchanged bool    `json:"unchanged,omitempty"`
}

func boardCommand(cmd string) bool {
	return slices.Contains([]string{"review", "ask", "answer", "questions", "note", "notes"}, cmd)
}
func parseBoard(o options) (options, error) {
	if o.help {
		return o, nil
	}
	cmd := o.command
	n := len(o.args)
	if cmd == "review" {
		if n == 0 {
			return o, fail(1, "Run bw review --help.")
		}
		cmd = o.args[0]
		n--
		want := 1
		switch cmd {
		case "list":
			want = 0
		case "take":
			if n <= 1 {
				want = n
			}
		case "request", "release", "approve", "changes", "merged", "show":
		default:
			return o, fail(1, "Unknown review command %q. Run bw review --help.", cmd)
		}
		if cmd == "take" && o.self && n == 0 {
			return o, fail(1, "Self-review requires an explicit review ID")
		}
		if n != want {
			return o, fail(1, "Wrong arguments. Run bw review --help.")
		}
	} else {
		switch cmd {
		case "ask", "answer", "note":
			if n < 2 {
				return o, fail(1, "Use bw %s <task-or-number> TEXT.", cmd)
			}
		case "questions", "notes":
			if n != 0 {
				return o, fail(1, "Run bw %s --help.", cmd)
			}
		}
	}
	if o.worker != "" || len(o.statuses) > 0 || o.next || o.markdown || o.all && cmd != "questions" || o.force && !(o.command == "review" && cmd == "release") || o.open && cmd != "list" || o.mine && cmd != "list" || o.self && cmd != "take" || o.branch != "" && cmd != "request" || o.head != "" && cmd != "request" || o.note != "" && cmd != "request" || o.notes != "" && cmd != "approve" && cmd != "changes" || o.model != "" && cmd != "approve" && cmd != "changes" || o.sha != "" && cmd != "merged" || o.task != "" && cmd != "notes" || o.technical && cmd != "ask" {
		return o, fail(1, "Flag is not supported here. Run bw %s --help.", o.command)
	}
	if cmd == "changes" && o.notes == "" {
		return o, fail(1, "changes requires --notes FILE")
	}
	return o, nil
}
func events(data []byte) ([]Event, error) {
	result := []Event{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e Event
		if err := decode([]byte(line), &e); err != nil {
			return nil, err
		}
		if e.TS.IsZero() || e.Actor == "" || e.Event == "" {
			return nil, fmt.Errorf("incomplete board event")
		}
		result = append(result, e)
	}
	return result, nil
}
func appendEvent(files map[string][]byte, path string, e Event) {
	b, _ := json.Marshal(e)
	files[path] = append(files[path], append(b, '\n')...)
}
func reviewPath(id string) string { return "reviews/" + id + "/events.jsonl" }
func readReview(id string, data []byte) (Review, error) {
	r := Review{ID: id}
	log, err := events(data)
	if err != nil {
		return r, err
	}
	for _, e := range log {
		switch e.Event {
		case "requested":
			if e.Task != "" {
				r.Task, r.Branch, r.Author = e.Task, e.Branch, e.Actor
			}
			r.Head, r.Requested = e.Head, e.TS
		case "taken":
			r.Reviewer = e.Actor
			r.Self = e.Actor == r.Author
		case "released":
			r.Reviewer = ""
			r.Self = false
		case "merged":
			r.MergedSHA = e.Head
		case "approved", "changes_requested", "self-approved", "self-changes":
		default:
			return r, fmt.Errorf("unknown review event %q", e.Event)
		}
		r.State = e.Event
	}
	if r.Author == "" || r.Head == "" || strings.ReplaceAll(r.Branch, "/", "--") != id {
		return r, fmt.Errorf("invalid review %q", id)
	}
	r.Events = log
	return r, nil
}
func reviews(files map[string][]byte) ([]Review, error) {
	rows := []Review{}
	for path, data := range files {
		if !strings.HasPrefix(path, "reviews/") || !strings.HasSuffix(path, "/events.jsonl") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(path, "reviews/"), "/events.jsonl")
		if strings.Contains(id, "/") || id == "" || id == "." || id == ".." {
			return nil, fmt.Errorf("invalid review ID")
		}
		r, err := readReview(id, data)
		if err != nil {
			return nil, err
		}
		r.Findings = string(files["reviews/"+id+"/findings.md"])
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}
func questionList(files map[string][]byte) ([]Question, error) {
	log, err := events(files["questions.jsonl"])
	if err != nil {
		return nil, err
	}
	rows := []Question{}
	indexes := map[int]int{}
	for _, e := range log {
		switch e.Event {
		case "asked":
			if e.ID <= 0 {
				return nil, fmt.Errorf("invalid question number")
			}
			if _, ok := indexes[e.ID]; ok {
				return nil, fmt.Errorf("duplicate question number")
			}
			indexes[e.ID] = len(rows)
			rows = append(rows, Question{Event: e, Answers: []Event{}})
		case "answered":
			i, ok := indexes[e.ID]
			if !ok {
				return nil, fmt.Errorf("answer to unknown question")
			}
			rows[i].Answers = append(rows[i].Answers, e)
		default:
			return nil, fmt.Errorf("unknown question event")
		}
	}
	return rows, nil
}
func resolveCommit(root, rev string) (string, error) {
	if rev == "" {
		rev = "HEAD"
	}
	out, err := git(root, nil, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", fail(1, "Cannot resolve commit %q. Check the revision and fetch it if needed.", rev)
	}
	sha := strings.TrimSpace(string(out))
	if _, err = git(root, nil, "cat-file", "-e", sha+"^{commit}"); err != nil {
		return "", fail(1, "Commit is unavailable: %s", sha)
	}
	return sha, nil
}
func executeBoard(o options, root string) (outcome, error) {
	return executeBoardContext(context.Background(), o, root)
}
func executeBoardContext(ctx context.Context, o options, root string) (outcome, error) {
	cmd := o.command
	if cmd == "review" {
		cmd = o.args[0]
		o.args = o.args[1:]
	}
	read := slices.Contains([]string{"list", "show", "questions", "notes"}, cmd)
	who := ""
	var err error
	if !read || o.mine {
		who, err = identity(o, root)
		if err != nil {
			return outcome{}, err
		}
	}
	if cmd == "request" {
		if o.branch == "" {
			out, e := git(root, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
			if e != nil {
				return outcome{}, fail(1, "Detached HEAD: pass --branch B.")
			}
			o.branch = strings.TrimSpace(string(out))
		}
		if _, e := git(root, nil, "check-ref-format", "--branch", o.branch); e != nil || strings.ContainsAny(o.branch, "\r\n\x00") {
			return outcome{}, fail(1, "Invalid review branch")
		}
		o.head, err = resolveCommit(root, o.head)
		if err != nil {
			return outcome{}, err
		}
	}
	if cmd == "merged" {
		o.sha, err = resolveCommit(root, o.sha)
		if err != nil {
			return outcome{}, err
		}
	}
	if o.notes != "" {
		b, e := os.ReadFile(o.notes)
		if e != nil {
			return outcome{}, fail(1, "Cannot read notes: %v", e)
		}
		if strings.TrimSpace(string(b)) == "" {
			return outcome{}, fail(1, "Notes file is empty")
		}
		o.note = string(b)
	}
	s, err := openStoreContext(ctx, root)
	if err != nil {
		return outcome{}, err
	}
	defer os.RemoveAll(s.dir)
	source := ""
	var catalog Catalog
	apply := func(snap snapshot) (any, bool, error) {
		source = snap.source
		catalog = snap.catalog
		if snap.state.Files == nil {
			snap.state.Files = map[string][]byte{}
		}
		if cmd == "merged" {
			// A shallow catalog fetch cannot prove ancestry. Fetch only the default
			// branch's complete history before checking; never move a working ref.
			if _, e := gitContext(s.ctx, s.dir, nil, "fetch", "--quiet", "--no-tags", "--depth=2147483647", "--", s.remote, "+HEAD:refs/bw/default-history"); e != nil {
				return nil, false, fail(4, "Cannot fetch default branch history%s", detail(e))
			}
			if _, e := gitContext(s.ctx, s.dir, nil, "merge-base", "--is-ancestor", o.sha, "refs/bw/default-history"); e != nil {
				return nil, false, fail(2, "Merge SHA must be an ancestor of origin's default branch")
			}
		}
		now := time.Now().UTC()
		v, changed, e := boardOperation(o, cmd, who, snap, now)
		// Board activity is a sign of life for the worker's claims.
		if e == nil && changed && who != "" {
			snap.state.touch(who, now)
		}
		return v, changed, e
	}
	if read {
		snap, e := s.load()
		if e != nil {
			return outcome{}, e
		}
		if snap.tip == "" {
			return outcome{}, fail(1, "State is not initialized. Run bw init.")
		}
		v, _, e := apply(snap)
		return outcome{result: addLinks(v, catalog, root), source: source}, e
	}
	v, e := s.update(false, apply)
	return outcome{result: addLinks(v, catalog, root), source: source}, e
}
func boardOperation(o options, cmd, who string, snap snapshot, now time.Time) (any, bool, error) {
	files := snap.state.Files
	if o.command != "review" {
		return messageOperation(o, cmd, who, files, snap.catalog, now)
	}
	rows, err := reviews(files)
	if err != nil {
		return nil, false, fail(1, "Invalid review state: %v", err)
	}
	if cmd == "list" {
		result := []Review{}
		for _, r := range rows {
			if o.open && r.State == "merged" || o.mine && r.Author != who && r.Reviewer != who {
				continue
			}
			r.Events = nil
			r.Findings = ""
			result = append(result, r)
		}
		return result, false, nil
	}
	id := ""
	if len(o.args) > 0 {
		id = o.args[0]
	}
	if cmd == "request" {
		id = strings.ReplaceAll(o.branch, "/", "--")
	}
	if cmd == "take" && id == "" {
		for _, r := range rows {
			if r.heldBy(who) && r.Author != who {
				r.Unchanged = true
				return r, false, nil
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Requested.Before(rows[j].Requested) })
		for _, r := range rows {
			if r.State == "requested" && r.Reviewer == "" && r.Author != who && reviewAllowed(o, snap.catalog, r.Task) {
				id = r.ID
				break
			}
		}
		if id == "" {
			return nil, false, fail(3, "No review waiting for another reviewer.")
		}
	}
	r := Review{ID: id}
	found := false
	for _, row := range rows {
		if row.ID == id {
			r = row
			found = true
			break
		}
	}
	if !found && cmd != "request" {
		return nil, false, fail(1, "Unknown review %q. Run bw review list.", id)
	}
	if cmd == "show" {
		return r, false, nil
	}
	unchanged := func() (any, bool, error) { r.Unchanged = true; return r, false, nil }
	event := Event{TS: now, Actor: who, Head: r.Head}
	switch cmd {
	case "request":
		task, ok := findView(snap.catalog, snap.state, o.args[0])
		if !ok {
			return nil, false, fail(1, "Unknown task %q", o.args[0])
		}
		if task.State != "claimed" || task.Holder != who {
			return nil, false, fail(2, "Review requests require your own claimed task")
		}
		if found && (r.Branch != o.branch || r.Task != task.ID || r.Author != who) {
			return nil, false, fail(2, "Review ID belongs to a different branch, task or author")
		}
		if found {
			lastNote := ""
			for _, e := range r.Events {
				if e.Event == "requested" {
					lastNote = e.Note
				}
			}
			if r.Head == o.head && lastNote == o.note && slices.Contains([]string{"requested", "taken", "approved", "self-approved", "merged"}, r.State) {
				return unchanged()
			}
			if r.State == "merged" {
				return nil, false, fail(2, "Review already merged; use a new branch")
			}
		}
		event.Event, event.Head, event.Task, event.Branch, event.Note = "requested", o.head, task.ID, o.branch, o.note
	case "take":
		if r.heldBy(who) {
			return unchanged()
		}
		if r.Reviewer != "" && r.Reviewer != who {
			return nil, false, fail(2, "Review is held by %s; only release or coordinated release --force frees it", r.Reviewer)
		}
		if r.State != "requested" {
			return nil, false, fail(2, "Review is not waiting for a reviewer")
		}
		if r.Author == who {
			delay := 30 * time.Minute
			if snap.catalog.SelfReviewAfter != "" {
				delay, _ = time.ParseDuration(snap.catalog.SelfReviewAfter)
			}
			if !o.self || now.Sub(r.Requested) < delay {
				return nil, false, fail(2, "Self-review requires --self and a wait of %s after the request", delay)
			}
		} else if o.self {
			return nil, false, fail(1, "--self is for the review author")
		}
		event.Event = "taken"
		event.Roles, _ = workerRoles(o.role)
		event.Effort = o.effort
	case "release":
		if r.State == "approved" || r.State == "self-approved" || r.State == "merged" || r.Reviewer == "" {
			return unchanged()
		}
		if r.Reviewer != who && !o.force {
			return nil, false, fail(2, "Not your review; coordinate before release --force")
		}
		event.Event = "released"
	case "approve", "changes":
		if r.Reviewer != who {
			return nil, false, fail(2, "Take the review before recording a verdict")
		}
		event.Event = "approved"
		if cmd == "changes" {
			event.Event = "changes_requested"
		}
		if r.Self {
			event.Event = "self-approved"
			if cmd == "changes" {
				event.Event = "self-changes"
			}
		}
		last := r.Events[len(r.Events)-1]
		if r.State == event.Event && last.Note == o.note && last.Model == o.model {
			return unchanged()
		}
		if r.State != "taken" && r.State != "requested" {
			return nil, false, fail(2, "Request review again before another verdict")
		}
		event.Note, event.Model = o.note, o.model
		if o.note != "" {
			path := "reviews/" + id + "/findings.md"
			files[path] = append(files[path], []byte(fmt.Sprintf("\n## %s %s %s head=%s\n\n%s\n", now.Format(time.RFC3339), who, event.Event, r.Head, o.note))...)
		}
	case "merged":
		if r.Author != who {
			return nil, false, fail(2, "Only the author can record the merge")
		}
		if r.State == "merged" && r.Events[len(r.Events)-1].Head == o.sha {
			return unchanged()
		}
		if r.State != "approved" && r.State != "self-approved" {
			return nil, false, fail(2, "Review needs approval before merged")
		}
		event.Event, event.Head = "merged", o.sha
	}
	appendEvent(files, reviewPath(id), event)
	if cmd == "release" && r.State != "changes_requested" && r.State != "self-changes" {
		appendEvent(files, reviewPath(id), Event{TS: now, Actor: who, Event: "requested", Head: r.Head, Note: "re-queued after release"})
	}
	result, e := readReview(id, files[reviewPath(id)])
	result.Findings = string(files["reviews/"+id+"/findings.md"])
	return result, true, e
}
func messageOperation(o options, cmd, who string, files map[string][]byte, catalog Catalog, now time.Time) (any, bool, error) {
	if cmd == "notes" || cmd == "note" {
		log, e := events(files["notes.jsonl"])
		if e != nil {
			return nil, false, e
		}
		if cmd == "notes" {
			result := []Event{}
			for _, v := range log {
				if o.task == "" || v.Task == o.task {
					result = append(result, v)
				}
			}
			return result, false, nil
		}
		text := strings.Join(o.args[1:], " ")
		if strings.TrimSpace(text) == "" {
			return nil, false, fail(1, "Note text is required")
		}
		for _, v := range log {
			if v.Actor == who && v.Task == o.args[0] && v.Note == text {
				return map[string]any{"note": v, "unchanged": true}, false, nil
			}
		}
		v := Event{TS: now, Actor: who, Event: "noted", Task: o.args[0], Note: text}
		appendEvent(files, "notes.jsonl", v)
		return v, true, nil
	}
	rows, e := questionList(files)
	if e != nil {
		return nil, false, e
	}
	if cmd == "questions" {
		result := []Question{}
		for _, q := range rows {
			if o.all || len(q.Answers) == 0 {
				result = append(result, q)
			}
		}
		return result, false, nil
	}
	text := strings.Join(o.args[1:], " ")
	if strings.TrimSpace(text) == "" {
		return nil, false, fail(1, "Question or answer text is required")
	}
	v := Event{TS: now, Actor: who, Note: text}
	if cmd == "ask" {
		v.Event, v.Task, v.Technical = "asked", o.args[0], o.technical
		v.ID = 1
		for _, q := range rows {
			v.ID = max(v.ID, q.ID+1)
			if q.Actor == who && q.Task == v.Task && q.Note == text && q.Technical == v.Technical {
				q.Unchanged = true
				return q, false, nil
			}
		}
	} else {
		v.Event = "answered"
		v.ID, e = strconv.Atoi(o.args[0])
		if e != nil || v.ID <= 0 {
			return nil, false, fail(1, "Answer needs a positive question number")
		}
		found := false
		for _, q := range rows {
			if q.ID == v.ID {
				found = true
				// Only coordinators settle non-technical questions (scope, done bars,
				// product behaviour) when the task file lists any.
				if !q.Technical && len(catalog.Coordinators) > 0 && !catalog.coordinator(who) {
					return nil, false, fail(2, "Question #%d is for a coordinator (%s). Only questions asked with --technical take other answers.", v.ID, strings.Join(catalog.Coordinators, ", "))
				}
				for _, a := range q.Answers {
					if a.Actor == who && a.Note == text {
						return map[string]any{"answer": a, "unchanged": true}, false, nil
					}
				}
			}
		}
		if !found {
			return nil, false, fail(1, "Unknown question #%d", v.ID)
		}
	}
	appendEvent(files, "questions.jsonl", v)
	return v, true, nil
}

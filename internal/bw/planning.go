package bw

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

func workerRoles(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	roles := strings.Split(raw, ",")
	for i, r := range roles {
		if !taskID.MatchString(r) || slices.Contains(roles[:i], r) {
			return nil, fail(1, "roles must be distinct task-ID-style names, separated by commas")
		}
	}
	return roles, nil
}
func allowsRoles(worker, required []string) bool {
	return len(worker) == 0 || len(required) == 0 || slices.ContainsFunc(required, func(r string) bool { return slices.Contains(worker, r) })
}
func taskAllowed(o options, t Task) bool {
	roles, _ := workerRoles(o.role)
	return allowsEffort(o.effort, t.Effort) && allowsRoles(roles, t.Roles)
}
func reviewAllowed(o options, c Catalog, id string) bool {
	roles, _ := workerRoles(o.role)
	for _, t := range c.Tasks {
		if t.ID == id {
			return allowsEffort(o.effort, reviewEffort(c, id)) && allowsRoles(roles, t.ReviewRoles)
		}
	}
	return true
}
func validatePlanning(c Catalog) error {
	if utf8.RuneCountInString(c.Description) > 200 || strings.ContainsAny(c.Description, "\r\n\x00") {
		return fmt.Errorf("description must be at most 200 characters on one line")
	}
	if c.IdleMinutes < 0 {
		return fmt.Errorf("idle_minutes must be positive or omitted")
	}
	validateRoles := func(rs []string) error {
		for i, r := range rs {
			if !taskID.MatchString(r) || slices.Contains(rs[:i], r) {
				return fmt.Errorf("roles must be distinct task-ID-style names")
			}
		}
		return nil
	}
	if e := validateRoles(c.Roles); e != nil {
		return e
	}
	for _, t := range c.Tasks {
		for _, rs := range [][]string{t.Roles, t.ReviewRoles} {
			if e := validateRoles(rs); e != nil {
				return e
			}
			for _, r := range rs {
				if c.Roles != nil && !slices.Contains(c.Roles, r) {
					return fmt.Errorf("task %s uses unknown role %s", t.ID, r)
				}
			}
		}
	}
	return c.Resources.validate()
}

type Shape struct {
	Remaining     int      `json:"remaining"`
	LongestChain  []string `json:"longest_chain"`
	ChainLength   int      `json:"chain_length"`
	MostReady     int      `json:"most_ready"`
	SuggestedCrew int      `json:"suggested_crew"`
	FanIn         []string `json:"fan_in"`
	Bottlenecks   []string `json:"bottlenecks"`
	Warnings      []string `json:"warnings"`
}

// Simulate equal-duration waves; completed prerequisites are removed. Catalog
// order breaks ties deterministically and is already priority then ID.
func planShape(c Catalog, state State) Shape {
	out := Shape{LongestChain: []string{}, FanIn: []string{}, Bottlenecks: []string{}, Warnings: []string{}}
	remaining := map[string]Task{}
	for _, t := range c.Tasks {
		if !state.Tasks[t.ID].Done {
			remaining[t.ID] = t
		}
	}
	out.Remaining = len(remaining)
	chains := map[string][]string{}
	var chain func(string) []string
	chain = func(id string) []string {
		if v, ok := chains[id]; ok {
			return v
		}
		best := []string{}
		for _, n := range remaining[id].Needs {
			if _, ok := remaining[n]; ok {
				v := chain(n)
				if len(v) > len(best) {
					best = v
				}
			}
		}
		v := append(append([]string{}, best...), id)
		chains[id] = v
		return v
	}
	for _, t := range c.Tasks {
		if _, ok := remaining[t.ID]; !ok {
			continue
		}
		v := chain(t.ID)
		if len(v) > out.ChainLength {
			out.LongestChain = v
			out.ChainLength = len(v)
		}
		n := 0
		for _, id := range t.Needs {
			if _, ok := remaining[id]; ok {
				n++
			}
		}
		if n >= 4 {
			out.FanIn = append(out.FanIn, t.ID)
		}
		if n > 0 && n*2 > out.Remaining-1 {
			out.Warnings = append(out.Warnings, t.ID+" waits on most other remaining tasks; see bw guide planning.")
		}
	}
	done := map[string]bool{}
	for len(done) < len(remaining) {
		wave := []string{}
		for _, t := range c.Tasks {
			if _, ok := remaining[t.ID]; !ok || done[t.ID] {
				continue
			}
			ready := true
			for _, n := range t.Needs {
				if _, ok := remaining[n]; ok && !done[n] {
					ready = false
				}
			}
			if ready {
				wave = append(wave, t.ID)
			}
		}
		if len(wave) == 0 {
			break
		}
		out.MostReady = max(out.MostReady, len(wave))
		for _, id := range wave {
			done[id] = true
		}
	}
	for _, t := range c.Tasks {
		if _, ok := remaining[t.ID]; !ok {
			continue
		}
		n := 0
		for _, other := range remaining {
			if slices.Contains(other.Needs, t.ID) {
				n++
			}
		}
		if n >= 4 {
			out.Bottlenecks = append(out.Bottlenecks, t.ID)
		}
	}
	out.SuggestedCrew = out.MostReady
	if out.ChainLength*2 > out.Remaining {
		out.Warnings = append(out.Warnings, "Longest chain exceeds half the remaining tasks; see bw guide planning.")
	}
	return out
}
func shapeText(s Shape) string {
	return fmt.Sprintf("%d remaining; longest chain %d: %s\nMost ready at once: %d; suggested crew: %d\nTasks waiting on four or more: %s\nTasks blocking four or more: %s\n%s", s.Remaining, s.ChainLength, strings.Join(s.LongestChain, " -> "), s.MostReady, s.SuggestedCrew, strings.Join(s.FanIn, ", "), strings.Join(s.Bottlenecks, ", "), strings.Join(s.Warnings, "\n"))
}

type Todo struct {
	Description   string         `json:"description,omitempty"`
	Counts        map[string]int `json:"counts"`
	UsefulWorkers int            `json:"useful_workers"`
	Tasks         TaskList       `json:"tasks"`
}

func todoRows(o options, snap snapshot, now time.Time) Todo {
	result := Todo{Description: snap.catalog.Description, Counts: map[string]int{"ready": 0, "claimed": 0, "waiting": 0}, Tasks: TaskList{}}
	rows := taskRows(snap, now)
	included := map[string]bool{}
	// Kahn ordering with priority as tie breaker at every choice.
	pending := TaskList{}
	for _, r := range rows {
		if r.State != "done" {
			pending = append(pending, r)
		}
	}
	slices.SortStableFunc(pending, func(a, b TaskRow) int {
		if a.Priority != b.Priority {
			if a.Priority > b.Priority {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	for len(included) < len(pending) {
		found := false
		for _, r := range pending {
			if included[r.ID] {
				continue
			}
			ready := true
			for _, n := range r.Needs {
				if !snap.state.Tasks[n].Done && !included[n] {
					ready = false
				}
			}
			if !ready {
				continue
			}
			included[r.ID] = true
			found = true
			if !o.mine || r.Holder == o.as || r.Holder == "" && taskAllowed(o, r.Task) {
				result.Tasks = append(result.Tasks, r)
				result.Counts[r.State]++
			}
			break
		}
		if !found {
			break
		}
	}
	result.UsefulWorkers = result.Counts["ready"] + result.Counts["claimed"]
	return result
}
func printTodo(w io.Writer, t Todo) {
	if t.Description != "" {
		fmt.Fprintln(w, t.Description)
	}
	fmt.Fprintf(w, "Left: %d claimed, %d ready, %d waiting; %d useful workers now\n", t.Counts["claimed"], t.Counts["ready"], t.Counts["waiting"], t.UsefulWorkers)
	printTasks(w, t.Tasks)
}

func activityLabel(v View) string {
	if v.Activity != "" {
		return v.Activity
	}
	return v.State
}
func decorateActivity(v *View, m Mark, c Catalog, board activityBoard, now time.Time) {
	if v.State != "claimed" {
		return
	}
	v.Activity = "claimed"
	v.LastWrite = "claim"
	if m.Seen.After(m.Since) {
		v.Activity = "in progress"
		v.LastWrite = "heartbeat"
	}
	latest := m.active()
	for _, at := range m.StepTicks {
		if at.Equal(latest) {
			v.LastWrite = "step"
		}
	}
	// Board timestamps reveal the write type without new state fields.
	rs := board.reviews
	var latestReview *Review
	for _, r := range rs {
		for _, e := range r.Events {
			if e.Actor == m.Holder && e.TS.Equal(latest) {
				v.LastWrite = "review " + e.Event
			}
		}
		if r.Task == v.ID && r.Author == m.Holder && !r.Requested.Before(m.Since) && (latestReview == nil || r.Requested.After(latestReview.Requested)) {
			copy := r
			latestReview = &copy
		}
	}
	if r := latestReview; r != nil && slices.Contains([]string{"requested", "taken", "changes_requested", "self-changes"}, r.State) {
		v.Activity = "in review"
		v.ReviewStatus = "waiting for a reviewer"
		if r.Reviewer != "" {
			v.ReviewStatus = "under review by " + r.Reviewer
		}
		if strings.Contains(r.State, "changes") {
			v.ReviewStatus = "changes requested"
		}
	}

	if write, ok := board.writes[m.Holder]; ok && write.at.Equal(latest) {
		v.LastWrite = write.kind
	}

	idle := c.IdleMinutes
	if idle == 0 {
		idle = 10
	}
	if v.Activity != "in review" && now.Sub(latest) > time.Duration(idle)*time.Minute {
		v.Activity = "idle"
	}
	if v.Stale {
		v.Activity = "stale"
	}
}

type activityWrite struct {
	at   time.Time
	kind string
}
type activityBoard struct {
	reviews []Review
	writes  map[string]activityWrite
}

func readActivityBoard(s State) activityBoard {
	board := activityBoard{writes: map[string]activityWrite{}}
	board.reviews, _ = reviews(s.Files)
	for _, file := range []string{"notes.jsonl", "questions.jsonl"} {
		es, _ := events(s.Files[file])
		for _, e := range es {
			if old, ok := board.writes[e.Actor]; !ok || !old.at.After(e.TS) {
				board.writes[e.Actor] = activityWrite{e.TS, e.Event}
			}
		}
	}
	return board
}

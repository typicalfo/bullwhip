package bw

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

type WorkerTask struct {
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Size  string            `json:"size,omitempty"`
	Links map[string]string `json:"links,omitempty"`
	ClaimPeriod
	Steps                []StepMetric  `json:"steps"`
	Rounds               []ReviewRound `json:"rounds"`
	ReviewWaitingSeconds float64       `json:"review_waiting_seconds"`
}
type WorkerDetail struct {
	Worker      string       `json:"worker"`
	Activity    string       `json:"activity"`
	Roles       []string     `json:"roles,omitempty"`
	Effort      string       `json:"effort,omitempty"`
	First       time.Time    `json:"first_activity"`
	Last        time.Time    `json:"last_activity"`
	Idle        string       `json:"idle"`
	Completed   int          `json:"completed"`
	Released    int          `json:"released"`
	Held        int          `json:"held"`
	HeldSeconds float64      `json:"held_seconds"`
	Current     []View       `json:"current"`
	Tasks       []WorkerTask `json:"tasks"`
	Reviews     []Review     `json:"reviews"`
	Verdicts    []Verdict    `json:"verdicts"`
	Questions   int          `json:"questions"`
	Notes       int          `json:"notes"`
	declared    time.Time
}
type WorkerList []WorkerDetail

type stepRecord struct {
	ticks   map[string]time.Time
	steps   []Step
	undone  map[string]bool
	history []StepMetric
}

func (r *stepRecord) apply(e TaskEvent) {
	if r.undone == nil {
		r.undone = map[string]bool{}
	}
	for k := range r.ticks {
		if _, ok := e.StepTicks[k]; !ok {
			r.undone[k] = true
		}
	}
	// Preserve the catalog and predecessor at each tick; later reordering or
	// removing a step must not rewrite the duration of work already recorded.
	for index, step := range e.StepCatalog {
		at, ok := e.StepTicks[step.key()]
		if !ok {
			continue
		}
		if before, had := r.ticks[step.key()]; had && before.Equal(at) {
			continue
		}
		metric := StepMetric{Key: step.key(), Text: step.Text, Ticked: at, Known: true}
		for _, prior := range e.StepCatalog[:index] {
			tick, done := e.StepTicks[prior.key()]
			if !done || tick.After(at) {
				metric.Known = false
				metric.Reason = "steps ticked out of catalog order"
			}
		}
		r.history = append(r.history, metric)
	}
	r.ticks = e.StepTicks
	r.steps = e.StepCatalog
}
func (r stepRecord) metrics(claim time.Time) []StepMetric {
	if len(r.history) > 0 {
		return r.historicalMetrics(claim)
	}
	out := []StepMetric{}
	for _, s := range r.steps {
		if at, ok := r.ticks[s.key()]; ok {
			out = append(out, StepMetric{Key: s.key(), Text: s.Text, Ticked: at, Known: true})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ticked.Before(out[j].Ticked) })
	previous := claim
	lastIndex := -1
	outOfOrder := false
	for i := range out {
		index := -1
		for j, s := range r.steps {
			if s.key() == out[i].Key {
				index = j
			}
		}
		if index <= lastIndex || index != i {
			outOfOrder = true
		}
		lastIndex = index
		out[i].Seconds = seconds(previous, out[i].Ticked)
		previous = out[i].Ticked
	}
	for i := range out {
		reason := ""
		if r.undone[out[i].Key] {
			reason = "unticked or ticked again"
		}
		if outOfOrder {
			reason = "steps ticked out of catalog order"
		}
		if i > 0 && out[i].Ticked.Sub(out[i-1].Ticked) < time.Minute || i+1 < len(out) && out[i+1].Ticked.Sub(out[i].Ticked) < time.Minute {
			reason = "several steps completed within one minute"
		}
		if out[i].Ticked.Before(claim) {
			reason = "task changed hands"
		}
		if reason != "" {
			out[i].Known = false
			out[i].Reason = reason
			out[i].Seconds = 0
		}
	}
	return out
}
func (r stepRecord) historicalMetrics(claim time.Time) []StepMetric {
	records := append([]StepMetric{}, r.history...)
	sort.SliceStable(records, func(i, j int) bool { return records[i].Ticked.Before(records[j].Ticked) })
	out := []StepMetric{}
	previous := claim
	for i, m := range records {
		m.Seconds = seconds(previous, m.Ticked)
		previous = m.Ticked
		if i > 0 && m.Ticked.Sub(records[i-1].Ticked) < time.Minute || i+1 < len(records) && records[i+1].Ticked.Sub(m.Ticked) < time.Minute {
			m.Known = false
			m.Reason = "several steps completed within one minute"
		}
		if r.undone[m.Key] {
			m.Known = false
			m.Reason = "unticked or ticked again"
		}
		if m.Ticked.Before(claim) {
			m.Known = false
			m.Reason = "task changed hands"
		}
		if !m.Known {
			m.Seconds = 0
		}
		if at, ok := r.ticks[m.Key]; !ok || !at.Equal(m.Ticked) {
			continue
		}
		for _, step := range r.steps {
			if step.key() == m.Key {
				m.Text = step.Text
				out = append(out, m)
				break
			}
		}
	}
	return out
}
func latestStepMetrics(t Task, log []TaskEvent) []StepMetric {
	r := stepRecord{}
	var claim time.Time
	for _, e := range log {
		if e.Task != t.ID {
			continue
		}
		if e.Kind == "claimed" {
			r = stepRecord{}
			claim = e.At
		}
		if e.Kind == "steps" {
			r.apply(e)
		}
	}
	metrics := r.metrics(claim)
	out := []StepMetric{}
	for _, m := range metrics {
		for _, step := range t.Steps {
			if m.Key == step.key() {
				m.Text = step.Text
				out = append(out, m)
				break
			}
		}
	}
	return out
}
func stepDuration(s StepMetric) string {
	if !s.Known {
		return "unknown (" + s.Reason + ")"
	}
	return durationText(s.Seconds)
}

func workerStepMetrics(task Task, record stepRecord, claim time.Time) []StepMetric {
	metrics := record.metrics(claim)
	steps := record.steps
	if len(steps) == 0 {
		steps = task.Steps
	}
	out := []StepMetric{}
	for _, step := range steps {
		m := StepMetric{Key: step.key(), Text: step.Text, Reason: "not ticked"}
		for _, known := range metrics {
			if known.Key == step.key() {
				m = known
				break
			}
		}
		out = append(out, m)
	}
	return out
}

func workerDetails(snap snapshot, log []TaskEvent, now time.Time) (WorkerList, error) {
	ws := map[string]*WorkerDetail{}
	tasks := map[string]Task{}
	for _, t := range snap.catalog.Tasks {
		tasks[t.ID] = t
	}
	get := func(name string, at time.Time) *WorkerDetail {
		if name == "" {
			return nil
		}
		w := ws[name]
		if w == nil {
			w = &WorkerDetail{Worker: name, Activity: "idle", Current: []View{}, Tasks: []WorkerTask{}, Reviews: []Review{}, Verdicts: []Verdict{}}
			ws[name] = w
		}
		if w.First.IsZero() || at.Before(w.First) {
			w.First = at
		}
		if at.After(w.Last) {
			w.Last = at
		}
		return w
	}
	declare := func(w *WorkerDetail, at time.Time, roles []string, effort string) {
		if !at.Before(w.declared) {
			w.declared = at
			w.Roles = roles
			w.Effort = effort
		}
	}
	type held struct {
		worker string
		index  int
		steps  stepRecord
	}
	open := map[string]*held{}
	finish := func(id string, e TaskEvent) {
		h := open[id]
		if h == nil {
			return
		}
		w := ws[h.worker]
		t := &w.Tasks[h.index]
		at := e.At
		t.Ended = &at
		t.Outcome = e.Kind
		t.Seconds = seconds(t.Claimed, at)
		t.Steps = workerStepMetrics(tasks[t.ID], h.steps, t.Claimed)
		w.HeldSeconds += t.Seconds
		if e.Kind == "done" {
			w.Completed++
		} else {
			w.Released++
		}
		delete(open, id)
	}
	for _, e := range log {
		if _, ok := tasks[e.Task]; !ok {
			continue
		}
		w := get(e.Holder, e.At)
		switch e.Kind {
		case "claimed":
			if _, ok := open[e.Task]; ok {
				finish(e.Task, TaskEvent{At: e.At, Kind: "released"})
			}
			t := tasks[e.Task]
			w.Tasks = append(w.Tasks, WorkerTask{ID: t.ID, Title: t.Title, Size: t.Size, ClaimPeriod: ClaimPeriod{Worker: e.Holder, Claimed: e.At, Outcome: "still held"}, Steps: []StepMetric{}, Rounds: []ReviewRound{}})
			open[e.Task] = &held{worker: e.Holder, index: len(w.Tasks) - 1}
			declare(w, e.At, e.Roles, e.Effort)
		case "steps":
			if h := open[e.Task]; h != nil {
				h.steps.apply(e)
			}
		case "done", "released":
			finish(e.Task, e)
		}
	}
	for _, h := range open {
		w := ws[h.worker]
		t := &w.Tasks[h.index]
		t.Seconds = seconds(t.Claimed, now)
		t.Steps = workerStepMetrics(tasks[t.ID], h.steps, t.Claimed)
		w.HeldSeconds += t.Seconds
		w.Held++
	}
	rs, e := reviews(snap.state.Files)
	if e != nil {
		return nil, e
	}
	for _, r := range rs {
		if _, ok := tasks[r.Task]; !ok {
			continue
		}
		for _, event := range r.Events {
			w := get(event.Actor, event.TS)
			if w == nil {
				continue
			}
			if event.Event == "taken" {
				declare(w, event.TS, event.Roles, event.Effort)
				copy := r
				copy.Events = nil
				w.Reviews = append(w.Reviews, copy)
			}
			if slices.Contains([]string{"approved", "changes_requested", "self-approved", "self-changes"}, event.Event) {
				w.Verdicts = append(w.Verdicts, Verdict{r.ID, r.Task, event.TS, event.Event, r.Author, event.Actor})
			}
			if r.heldBy(event.Actor) {
				w.Activity = "reviewing"
			}
		}
		// Attach review rounds to the claim containing the request, including verdict delay.
		for _, event := range r.Events {
			if event.Event != "requested" || event.Task == "" {
				continue
			}
			w := ws[r.Author]
			if w == nil {
				continue
			}
			for i := range w.Tasks {
				t := &w.Tasks[i]
				if t.ID != r.Task || event.TS.Before(t.Claimed) || t.Ended != nil && event.TS.After(*t.Ended) {
					continue
				}
				round := ReviewRound{Branch: r.Branch, Head: event.Head, Review: r.ID, Requested: event.TS, Author: r.Author}
				until := now
				for _, v := range r.Events {
					if v.TS.Before(event.TS) {
						continue
					}
					if v.Event == "taken" {
						round.Reviewer = v.Actor
					}
					if slices.Contains([]string{"approved", "changes_requested", "self-approved", "self-changes"}, v.Event) {
						round.Verdict = v.Event
						at := v.TS
						round.VerdictAt = &at
						round.SelfApproved = v.Event == "self-approved"
						until = v.TS
						break
					}
					if v.Event == "requested" && v.TS.After(event.TS) {
						until = v.TS
						break
					}
				}
				round.Seconds = seconds(event.TS, until)
				t.Rounds = append(t.Rounds, round)
				t.ReviewWaitingSeconds += seconds(event.TS, until)
			}
		}
	}
	qs, e := questionList(snap.state.Files)
	if e != nil {
		return nil, e
	}
	for _, q := range qs {
		if _, ok := tasks[q.Task]; !ok {
			continue
		}
		if w := get(q.Actor, q.TS); w != nil {
			w.Questions++
		}
		for _, a := range q.Answers {
			get(a.Actor, a.TS)
		}
	}
	ns, e := events(snap.state.Files["notes.jsonl"])
	if e != nil {
		return nil, e
	}
	for _, n := range ns {
		if _, ok := tasks[n.Task]; ok {
			if w := get(n.Actor, n.TS); w != nil {
				w.Notes++
			}
		}
	}
	for _, v := range snap.catalog.views(snap.state, now) {
		if v.State == "claimed" {
			w := get(v.Holder, snap.state.Tasks[v.ID].active())
			w.Current = append(w.Current, v)
			w.Activity = "working"
		}
	}
	out := WorkerList{}
	for _, w := range ws {
		w.Idle = age(now.Sub(w.Last))
		sort.SliceStable(w.Tasks, func(i, j int) bool { return w.Tasks[i].Claimed.Before(w.Tasks[j].Claimed) })
		out = append(out, *w)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Activity != "idle") != (b.Activity != "idle") {
			return a.Activity != "idle"
		}
		if !a.Last.Equal(b.Last) {
			return a.Last.After(b.Last)
		}
		return a.Worker < b.Worker
	})
	return out, nil
}
func workersText(ws WorkerList) string {
	var b strings.Builder
	for _, w := range ws {
		fmt.Fprintf(&b, "%s · %s · roles %s · effort %s\n%d done; %d released; %d held; %s holding claims; last %s ago\nFirst: %s; last: %s; %d reviews taken; %d questions; %d notes\n", clean(w.Worker), w.Activity, strings.Join(w.Roles, ","), w.Effort, w.Completed, w.Released, w.Held, durationText(w.HeldSeconds), w.Idle, w.First.Format(time.RFC3339), w.Last.Format(time.RFC3339), len(w.Reviews), w.Questions, w.Notes)
		for _, t := range w.Tasks {
			end := "now"
			if t.Ended != nil {
				end = t.Ended.Format(time.RFC3339)
			}
			fmt.Fprintf(&b, "  %s · %s · %s · %s; %s -> %s; %s; review %s\n", t.ID, clean(t.Title), t.Size, t.Outcome, t.Claimed.Format(time.RFC3339), end, durationText(t.Seconds), durationText(t.ReviewWaitingSeconds))
			for _, s := range t.Steps {
				fmt.Fprintf(&b, "    %s: %s\n", clean(s.Text), stepDuration(s))
			}
			for _, r := range t.Rounds {
				fmt.Fprintf(&b, "    Review %s: %s by %s; %s\n", r.Review, r.Verdict, clean(r.Reviewer), durationText(r.Seconds))
			}
		}
		for _, v := range w.Verdicts {
			fmt.Fprintf(&b, "  Review %s: %s\n", v.Review, v.Verdict)
		}
	}
	return strings.TrimSpace(b.String())
}

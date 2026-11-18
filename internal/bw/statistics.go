package bw

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"
)

type RunMetrics struct {
	Started        *time.Time `json:"started,omitempty"`
	Ended          *time.Time `json:"ended,omitempty"`
	Finished       bool       `json:"finished"`
	ElapsedSeconds float64    `json:"elapsed_seconds"`
	Done           int        `json:"done"`
	Total          int        `json:"total"`
	DonePerHour    float64    `json:"done_per_hour"`
}
type ClaimPeriod struct {
	Worker  string     `json:"worker"`
	Claimed time.Time  `json:"claimed"`
	Ended   *time.Time `json:"ended,omitempty"`
	Outcome string     `json:"outcome"`
	Seconds float64    `json:"seconds"`
}
type ReviewRound struct {
	Branch       string            `json:"branch,omitempty"`
	Head         string            `json:"head,omitempty"`
	Links        map[string]string `json:"links,omitempty"`
	VerdictAt    *time.Time        `json:"verdict_at,omitempty"`
	Seconds      float64           `json:"seconds"`
	Review       string            `json:"review"`
	Requested    time.Time         `json:"requested"`
	Author       string            `json:"author"`
	Reviewer     string            `json:"reviewer,omitempty"`
	Verdict      string            `json:"verdict,omitempty"`
	SelfApproved bool              `json:"self_approved"`
}
type Verdict struct {
	Review   string    `json:"review"`
	Task     string    `json:"task"`
	At       time.Time `json:"at"`
	Verdict  string    `json:"verdict"`
	Author   string    `json:"author"`
	Reviewer string    `json:"reviewer"`
}
type WorkerMetrics struct {
	Worker          string      `json:"worker"`
	Completed       int         `json:"completed"`
	HeldSeconds     float64     `json:"held_seconds"`
	WorkingSeconds  float64     `json:"working_seconds"`
	IdleSeconds     float64     `json:"idle_seconds"`
	StaleSeconds    float64     `json:"stale_seconds"`
	Current         []View      `json:"current"`
	Releases        []TaskEvent `json:"releases"`
	ReviewsGiven    []Verdict   `json:"reviews_given"`
	ReviewsReceived []Verdict   `json:"reviews_received"`
	Questions       int         `json:"questions"`
	Notes           int         `json:"notes"`
}
type StepMetric struct {
	Known   bool      `json:"known"`
	Reason  string    `json:"reason,omitempty"`
	Key     string    `json:"key"`
	Text    string    `json:"text"`
	Ticked  time.Time `json:"ticked"`
	Seconds float64   `json:"seconds"`
}
type SizeMetric struct {
	Size        string  `json:"size"`
	Tasks       int     `json:"tasks"`
	HeldSeconds float64 `json:"held_seconds"`
}
type TaskMetrics struct {
	Links                map[string]string `json:"links,omitempty"`
	Size                 string            `json:"size,omitempty"`
	Steps                []StepMetric      `json:"steps"`
	ReviewWaitingSeconds float64           `json:"review_waiting_seconds"`
	ID                   string            `json:"id"`
	Title                string            `json:"title"`
	State                string            `json:"state"`
	Holders              []ClaimPeriod     `json:"holders"`
	ReadySeconds         float64           `json:"ready_seconds"`
	WaitingSeconds       float64           `json:"waiting_seconds"`
	HeldSeconds          float64           `json:"held_seconds"`
	ClaimToDoneSeconds   *float64          `json:"claim_to_done_seconds,omitempty"`
	DoneAt               *time.Time        `json:"done_at,omitempty"`
	Rounds               []ReviewRound     `json:"rounds"`
}
type Statistics struct {
	Sizes   []SizeMetric    `json:"sizes"`
	Run     RunMetrics      `json:"run"`
	Workers []WorkerMetrics `json:"workers"`
	Tasks   []TaskMetrics   `json:"tasks"`
}

func seconds(start, end time.Time) float64 { return max(0, end.Sub(start).Seconds()) }

func statistics(o options, snap snapshot, log []TaskEvent, now time.Time) (Statistics, error) {
	report := Statistics{Run: RunMetrics{Total: len(snap.catalog.Tasks)}, Workers: []WorkerMetrics{}, Tasks: []TaskMetrics{}}
	tasks := map[string]*TaskMetrics{}
	for _, v := range taskRows(snap, now) {
		tasks[v.ID] = &TaskMetrics{ID: v.ID, Title: v.Title, Size: v.Size, Steps: []StepMetric{}, State: v.State, Holders: []ClaimPeriod{}, Rounds: []ReviewRound{}}
		if v.State == "done" {
			report.Run.Done++
		}
	}
	if o.task != "" && tasks[o.task] == nil {
		return report, fail(1, "Unknown task %q. Run bw tasks.", o.task)
	}
	report.Run.Finished = report.Run.Done == report.Run.Total
	var start, end time.Time
	for _, e := range log {
		if start.IsZero() && e.Kind == "claimed" {
			start = e.At
		}
		if e.Kind == "done" {
			end = e.At
		}
	}
	if start.IsZero() {
		for _, task := range snap.catalog.Tasks {
			if (o.task == "" || task.ID == o.task) && o.worker == "" {
				report.Tasks = append(report.Tasks, *tasks[task.ID])
			}
		}
		return report, nil
	}
	report.Run.Started = &start
	if report.Run.Finished {
		report.Run.Ended = &end
	} else {
		end = now
	}
	if end.Before(start) {
		end = start
	}
	report.Run.ElapsedSeconds = seconds(start, end)
	if report.Run.ElapsedSeconds > 0 {
		report.Run.DonePerHour = float64(report.Run.Done) * 3600 / report.Run.ElapsedSeconds
	}
	workers := map[string]*WorkerMetrics{}
	involved := map[string]map[string]bool{}
	worker := func(name, task string) *WorkerMetrics {
		if o.task != "" && task != o.task {
			return nil
		}
		if involved[name] == nil {
			involved[name] = map[string]bool{}
		}
		involved[name][task] = true
		if workers[name] == nil {
			workers[name] = &WorkerMetrics{Worker: name, Current: []View{}, Releases: []TaskEvent{}, ReviewsGiven: []Verdict{}, ReviewsReceived: []Verdict{}}
		}
		return workers[name]
	}
	state := State{Tasks: map[string]Mark{}}
	last := start
	accrue := func(until time.Time) {
		d := seconds(last, until)
		for _, task := range snap.catalog.Tasks {
			row := tasks[task.ID]
			switch phase(task, state) {
			case "ready":
				row.ReadySeconds += d
			case "waiting":
				row.WaitingSeconds += d
			}
			if mark, held := state.Tasks[task.ID]; held && !mark.Done {
				row.HeldSeconds += d
				if w := worker(state.Tasks[task.ID].Holder, task.ID); w != nil {
					w.HeldSeconds += d
				}
			}
		}
		last = until
	}
	for _, e := range log {
		at := minTime(end, maxTime(last, e.At))
		accrue(at)
		row := tasks[e.Task]
		switch e.Kind {
		case "claimed":
			state.Tasks[e.Task] = Mark{Holder: e.Holder, Since: at}
			row.Holders = append(row.Holders, ClaimPeriod{Worker: e.Holder, Claimed: at, Outcome: "claimed"})
			worker(e.Holder, e.Task)
		case "done", "released":
			if len(row.Holders) > 0 {
				period := &row.Holders[len(row.Holders)-1]
				ts := at
				period.Ended = &ts
				period.Outcome = e.Kind
				period.Seconds = seconds(period.Claimed, at)
			}
			if e.Kind == "done" {
				state.Tasks[e.Task] = Mark{Holder: e.Holder, Done: true}
				ts := at
				row.DoneAt = &ts
				if w := worker(e.Holder, e.Task); w != nil {
					w.Completed++
				}
			} else {
				delete(state.Tasks, e.Task)
				if w := worker(e.Holder, e.Task); w != nil {
					w.Releases = append(w.Releases, e)
				}
				if e.Actor != "" && e.Actor != e.Holder {
					if w := worker(e.Actor, e.Task); w != nil {
						w.Releases = append(w.Releases, e)
					}
				}
			}
		case "reopened":
			delete(state.Tasks, e.Task)
			row.DoneAt = nil
		}
	}
	accrue(maxTime(last, end))
	for _, task := range snap.catalog.Tasks {
		row := tasks[task.ID]
		if len(row.Holders) == 0 {
			continue
		}
		period := &row.Holders[len(row.Holders)-1]
		if period.Ended == nil {
			period.Seconds = seconds(period.Claimed, end)
		}
		if row.DoneAt != nil {
			d := seconds(row.Holders[0].Claimed, *row.DoneAt)
			row.ClaimToDoneSeconds = &d
		}
	}
	for _, v := range snap.catalog.views(snap.state, now) {
		if v.State == "claimed" {
			if w := worker(v.Holder, v.ID); w != nil {
				w.Current = append(w.Current, v)
			}
		}
	}
	// Board logs are append-only, so the current tree supplies their timestamps.
	inRun := func(e Event) bool {
		return tasks[e.Task] != nil && !e.TS.Before(start) && (!report.Run.Finished || e.TS.Unix() <= end.Unix())
	}
	rs, err := reviews(snap.state.Files)
	if err != nil {
		return report, fail(1, "Invalid review state: %v", err)
	}
	for _, r := range rs {
		if tasks[r.Task] == nil {
			continue
		}
		reviewer := ""
		round := -1
		for _, e := range r.Events {
			if e.Event == "taken" {
				reviewer = e.Actor
			}
			if e.Event == "released" {
				reviewer = ""
			}
			explicitRequest := e.Task != ""
			e.Task = r.Task
			if !inRun(e) {
				continue
			}
			if e.Event == "requested" && explicitRequest {
				row := tasks[r.Task]
				row.Rounds = append(row.Rounds, ReviewRound{Branch: r.Branch, Head: e.Head, Review: r.ID, Requested: e.TS, Author: r.Author, Reviewer: reviewer})
				round = len(row.Rounds) - 1
				worker(r.Author, r.Task)
			}
			if e.Event == "taken" {
				worker(e.Actor, r.Task)
				if round >= 0 {
					tasks[r.Task].Rounds[round].Reviewer = reviewer
				}
			}
			if slices.Contains([]string{"approved", "changes_requested", "self-approved", "self-changes"}, e.Event) {
				verdict := Verdict{r.ID, r.Task, e.TS, e.Event, r.Author, e.Actor}
				if w := worker(e.Actor, r.Task); w != nil {
					w.ReviewsGiven = append(w.ReviewsGiven, verdict)
				}
				if w := worker(r.Author, r.Task); w != nil {
					w.ReviewsReceived = append(w.ReviewsReceived, verdict)
				}
				if round >= 0 {
					tasks[r.Task].Rounds[round].Verdict = e.Event
					at := e.TS
					tasks[r.Task].Rounds[round].VerdictAt = &at
					tasks[r.Task].Rounds[round].SelfApproved = e.Event == "self-approved"
				}
			}
		}
	}
	qs, err := questionList(snap.state.Files)
	if err != nil {
		return report, fail(1, "Invalid question state: %v", err)
	}
	for _, q := range qs {
		if inRun(q.Event) {
			if w := worker(q.Actor, q.Task); w != nil {
				w.Questions++
			}
		}
	}
	notes, err := events(snap.state.Files["notes.jsonl"])
	if err != nil {
		return report, fail(1, "Invalid note state: %v", err)
	}
	for _, e := range notes {
		if inRun(e) {
			if w := worker(e.Actor, e.Task); w != nil {
				w.Notes++
			}
		}
	}
	for _, task := range snap.catalog.Tasks {
		row := tasks[task.ID]
		row.Steps = latestStepMetrics(task, log)

		for ri, r := range row.Rounds {
			until := end
			for _, next := range row.Rounds[ri+1:] {
				if next.Review == r.Review {
					until = minTime(until, next.Requested)
					break
				}
			}
			for _, review := range rs {
				if review.ID != r.Review {
					continue
				}
				for _, event := range review.Events {
					if !event.TS.Before(r.Requested) && slices.Contains([]string{"approved", "changes_requested", "self-approved", "self-changes"}, event.Event) {
						until = minTime(until, event.TS)
						break
					}
				}
			}
			row.Rounds[ri].Seconds = seconds(r.Requested, until)
			row.ReviewWaitingSeconds += row.Rounds[ri].Seconds
		}
	}
	workerIntervals(log, workers, end, time.Duration(snap.catalog.staleHours())*time.Hour, o.task)

	sizes := map[string]*SizeMetric{}
	for _, task := range snap.catalog.Tasks {
		if o.task != "" && o.task != task.ID || o.worker != "" && !involved[o.worker][task.ID] {
			continue
		}
		size := task.Size
		if size == "" {
			size = "unsized"
		}
		if sizes[size] == nil {
			sizes[size] = &SizeMetric{Size: size}
		}
		sizes[size].Tasks++
		sizes[size].HeldSeconds += tasks[task.ID].HeldSeconds
	}
	for _, size := range []string{"xs", "s", "m", "l", "xl", "unsized"} {
		if row := sizes[size]; row != nil {
			report.Sizes = append(report.Sizes, *row)
		}
	}
	for _, w := range workers {
		if o.worker == "" || w.Worker == o.worker {
			report.Workers = append(report.Workers, *w)
		}
	}
	sort.Slice(report.Workers, func(i, j int) bool {
		a, b := report.Workers[i], report.Workers[j]
		if a.Completed != b.Completed {
			return a.Completed > b.Completed
		}
		return a.Worker < b.Worker
	})
	for _, row := range tasks {
		sort.SliceStable(row.Rounds, func(i, j int) bool { return row.Rounds[i].Requested.Before(row.Rounds[j].Requested) })
	}
	for _, task := range snap.catalog.Tasks {
		if (o.task == "" || task.ID == o.task) && (o.worker == "" || involved[o.worker][task.ID]) {
			report.Tasks = append(report.Tasks, *tasks[task.ID])
		}
	}
	return report, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
func durationText(s float64) string { return (time.Duration(s) * time.Second).String() }
func printStats(w io.Writer, s Statistics) {
	if s.Run.Started == nil {
		fmt.Fprintf(w, "Run not started; %d/%d done\n", s.Run.Done, s.Run.Total)
		return
	}
	fmt.Fprintf(w, "Run started %s; elapsed %s; %d/%d done; %.2f/h\n", s.Run.Started.Format(time.RFC3339), durationText(s.Run.ElapsedSeconds), s.Run.Done, s.Run.Total, s.Run.DonePerHour)
	if s.Run.Ended != nil {
		fmt.Fprintf(w, "Finished %s; total %s\n", s.Run.Ended.Format(time.RFC3339), durationText(s.Run.ElapsedSeconds))
	}
	cells := [][]string{}
	for _, worker := range s.Workers {
		current := []string{}
		for _, v := range worker.Current {
			current = append(current, v.ID+" "+v.Age)
		}
		if len(current) == 0 {
			current = append(current, "-")
		}
		cells = append(cells, []string{worker.Worker, fmt.Sprint(worker.Completed), durationText(worker.HeldSeconds), fmt.Sprintf("%d/%d", len(worker.ReviewsGiven), len(worker.ReviewsReceived)), fmt.Sprint(len(worker.Releases)), fmt.Sprint(worker.Questions), fmt.Sprint(worker.Notes), strings.Join(current, ", ")})
	}
	printTable(w, []string{"WORKER", "DONE", "HELD", "REVIEWS GIVEN/RECEIVED", "RELEASES", "ASKED", "NOTES", "CURRENT"}, cells)
	fmt.Fprintln(w)
	cells = [][]string{}
	for _, task := range s.Tasks {
		holders := []string{}
		for _, h := range task.Holders {
			holders = append(holders, clean(h.Worker))
		}
		if len(holders) == 0 {
			holders = append(holders, "-")
		}
		complete := "-"
		if task.ClaimToDoneSeconds != nil {
			complete = durationText(*task.ClaimToDoneSeconds)
		}
		cells = append(cells, []string{task.ID + " " + task.Size, strings.Join(holders, " -> "), durationText(task.ReadySeconds), durationText(task.WaitingSeconds), durationText(task.HeldSeconds), complete, fmt.Sprint(len(task.Rounds))})
	}
	printTable(w, []string{"TASK", "HOLDERS", "READY>CLAIM", "WAITING", "HELD", "CLAIM>DONE", "ROUNDS"}, cells)
	fmt.Fprintln(w)
	seenReleases := map[string]bool{}
	for _, worker := range s.Workers {
		for _, e := range worker.Releases {
			key := e.At.Format(time.RFC3339Nano) + "/" + e.Task + "/" + e.Holder
			if seenReleases[key] {
				continue
			}
			seenReleases[key] = true
			actor := clean(e.Actor)
			if actor == "" {
				actor = "unknown"
			}
			fmt.Fprintf(w, "Release %s: holder %s; by %s; %s; forced=%t; %s\n", e.Task, clean(e.Holder), actor, e.At.Format(time.RFC3339), e.Forced, clean(e.Reason))
		}
		for _, v := range worker.ReviewsGiven {
			fmt.Fprintf(w, "Review %s: %s by %s for %s\n", clean(v.Review), v.Verdict, clean(v.Reviewer), clean(v.Author))
		}
	}
	for _, size := range s.Sizes {
		fmt.Fprintf(w, "Size %s: %d tasks; held %s\n", size.Size, size.Tasks, durationText(size.HeldSeconds))
	}
	for _, task := range s.Tasks {
		for _, step := range task.Steps {
			fmt.Fprintf(w, "Step %s/%s: %s since previous tick or claim\n", task.ID, clean(step.Key), stepDuration(step))
		}
		for _, h := range task.Holders {
			end := "now"
			if h.Ended != nil {
				end = h.Ended.Format(time.RFC3339)
			}
			fmt.Fprintf(w, "Claim %s: %s; %s -> %s; %s (%s)\n", task.ID, clean(h.Worker), h.Claimed.Format(time.RFC3339), end, h.Outcome, durationText(h.Seconds))
		}
		for _, r := range task.Rounds {
			fmt.Fprintf(w, "Round %s/%s: requested %s; reviewer %s; %s; self-approved=%t\n", task.ID, clean(r.Review), r.Requested.Format(time.RFC3339), clean(r.Reviewer), r.Verdict, r.SelfApproved)
		}
	}
}

// Worker time starts at their first claim. Working/idle are a union of claim
// intervals; stale is the subset of working time after the activity threshold.
// HeldSeconds remains a sum, so simultaneous claims can overlap there.
func workerIntervals(log []TaskEvent, workers map[string]*WorkerMetrics, end time.Time, threshold time.Duration, taskFilter string) {
	active := map[string]Mark{}
	started := map[string]time.Time{}
	var last time.Time
	accrue := func(until time.Time) {
		if last.IsZero() {
			last = until
			return
		}
		latest := map[string]time.Time{}
		for _, m := range active {
			if old, ok := latest[m.Holder]; !ok || m.active().After(old) {
				latest[m.Holder] = m.active()
			}
		}
		for name := range started {
			w := workers[name]
			if w == nil {
				continue
			}
			seen, held := latest[name]
			if !held {
				w.IdleSeconds += seconds(last, until)
			} else {
				w.WorkingSeconds += seconds(last, until)
				w.StaleSeconds += seconds(maxTime(last, seen.Add(threshold)), until)
			}
		}
		last = until
	}
	for _, e := range log {
		if taskFilter != "" && e.Task != taskFilter {
			continue
		}
		at := minTime(end, e.At)
		if !last.IsZero() {
			at = maxTime(last, at)
		}
		accrue(at)
		switch e.Kind {
		case "claimed":
			active[e.Task] = Mark{Holder: e.Holder, Since: at}
			if _, ok := started[e.Holder]; !ok {
				started[e.Holder] = at
			}
		case "activity":
			if m, ok := active[e.Task]; ok {
				m.Seen = at
				active[e.Task] = m
			}
		case "done", "released", "reopened":
			delete(active, e.Task)
		}
	}
	if !last.IsZero() {
		accrue(maxTime(last, end))
	}
}

package bw

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

var taskStates = []string{"claimed", "ready", "waiting", "done"}

type Blocker struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Holder string `json:"holder,omitempty"`
	Idle   string `json:"idle,omitempty"`
	Stale  bool   `json:"stale"`
}
type TaskRow struct {
	View
	BlockedBy []Blocker  `json:"blocked_by"`
	DoneAt    *time.Time `json:"done_at,omitempty"`
}
type TaskList []TaskRow

type Summary struct {
	Description          string         `json:"description,omitempty"`
	UsefulWorkers        int            `json:"useful_workers"`
	Counts               map[string]int `json:"counts"`
	Claims               []View         `json:"claims"`
	Reviews              []Review       `json:"reviews"`
	ReviewWaiting        int            `json:"review_waiting"`
	ReviewTaken          int            `json:"review_taken"`
	Questions            []Question     `json:"questions"`
	CoordinatorQuestions int            `json:"coordinator_questions"`
	AllDone              bool           `json:"all_done"`
	Progress             string         `json:"progress"`
}
type IdleAdvice struct {
	Review     string `json:"review,omitempty"`
	ReasonCode string `json:"reason_code,omitempty"`
	Action     string `json:"action"`
	Task       string `json:"task,omitempty"`
	Waiting    int    `json:"waiting"`
	Claimed    int    `json:"claimed"`
	Reason     string `json:"reason"`
}

func phase(task Task, state State) string {
	if state.Tasks[task.ID].Done {
		return "done"
	}
	for _, id := range task.Needs {
		if !state.Tasks[id].Done {
			return "waiting"
		}
	}
	if _, ok := state.Tasks[task.ID]; ok {
		return "claimed"
	}
	return "ready"
}

func taskRows(snap snapshot, now time.Time) TaskList {
	rows := TaskList{}
	views := snap.catalog.views(snap.state, now)
	byID := map[string]View{}
	for _, v := range views {
		v.State = phase(v.Task, snap.state)
		byID[v.ID] = v
	}
	for _, v := range views {
		v = byID[v.ID]
		row := TaskRow{View: v, BlockedBy: []Blocker{}}
		for _, id := range v.Needs {
			need := byID[id]
			if need.State != "done" {
				row.BlockedBy = append(row.BlockedBy, Blocker{id, need.State, need.Holder, need.Idle, need.Stale})
			}
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return slices.Index(taskStates, rows[i].State) < slices.Index(taskStates, rows[j].State)
	})
	return rows
}

func summarize(snap snapshot, now time.Time) (Summary, error) {
	s := Summary{Description: snap.catalog.Description, Counts: map[string]int{}, Claims: []View{}, Reviews: []Review{}, Questions: []Question{}}
	for _, state := range taskStates {
		s.Counts[state] = 0
	}
	for _, row := range taskRows(snap, now) {
		s.Counts[row.State]++
	}
	claims, err := list(options{command: "status"}, snap)
	if err != nil {
		return s, err
	}
	s.Claims = claims.([]View)
	sort.SliceStable(s.Claims, func(i, j int) bool {
		if s.Claims[i].Priority != s.Claims[j].Priority {
			return s.Claims[i].Priority > s.Claims[j].Priority
		}
		return s.Claims[i].ID < s.Claims[j].ID
	})
	rs, err := reviews(snap.state.Files)
	if err != nil {
		return s, fail(1, "Invalid review state: %v", err)
	}
	for _, r := range rs {
		r = reviewMetadata(snap.catalog, r)
		if r.State != "requested" && r.State != "taken" {
			continue
		}
		if r.Reviewer == "" {
			s.ReviewWaiting++
		} else {
			s.ReviewTaken++
		}
		r.Events, r.Findings = nil, ""
		s.Reviews = append(s.Reviews, r)
	}
	qs, err := questionList(snap.state.Files)
	if err != nil {
		return s, fail(1, "Invalid question state: %v", err)
	}
	for _, q := range qs {
		if len(q.Answers) > 0 {
			continue
		}
		s.Questions = append(s.Questions, q)
		if !q.Technical && len(snap.catalog.Coordinators) > 0 {
			s.CoordinatorQuestions++
		}
	}
	s.UsefulWorkers = s.Counts["ready"] + s.Counts["claimed"]
	s.AllDone = s.Counts["done"] == len(snap.catalog.Tasks)
	ready := fmt.Sprintf("%d ready", s.Counts["ready"])
	if s.Counts["ready"] == 0 {
		ready = "nothing ready"
	}
	s.Progress = fmt.Sprintf("%d of %d done; %d claimed; %d waiting; %s", s.Counts["done"], len(snap.catalog.Tasks), s.Counts["claimed"], s.Counts["waiting"], ready)
	if s.AllDone {
		s.Progress += "; all tasks done"
	}
	return s, nil
}

func workerAdvice(o options, who string, snap snapshot, now time.Time, resources ResourceReport) IdleAdvice {
	if resources.Status == "critical_resources" {
		return IdleAdvice{Action: "stop", ReasonCode: "critical_resources", Reason: resources.Reason}
	}
	// Own claims take precedence over minimums, roles and effort.
	claims, _ := list(options{command: "status"}, snap)
	for _, v := range claims.([]View) {
		if v.Holder == who {
			return IdleAdvice{Action: "work", Task: v.ID, Reason: "You hold " + v.ID + "; finish or release it before taking more work."}
		}
	}
	a := IdleAdvice{Action: "stop"}
	eligibleWaiting := false
	needed := []string{}
	for _, v := range snap.catalog.views(snap.state, now) {
		if v.State == "waiting" {
			a.Waiting++
			if taskAllowed(o, v.Task) {
				eligibleWaiting = true
			}
		}
		if v.State == "claimed" {
			a.Claimed++
			if taskAllowed(o, v.Task) {
				eligibleWaiting = true
			}
		}
		if v.State != "done" && !taskAllowed(o, v.Task) {
			needed = append(needed, v.ID+" (roles "+strings.Join(v.Roles, ",")+"; effort "+v.Effort+")")
		}
		if v.State != "ready" {
			continue
		}
		if taskAllowed(o, v.Task) {
			if resources.Status == "low_resources" {
				a.Action, a.ReasonCode, a.Reason = "wait", "low_resources", resources.Reason
				return a
			}
			a.Action, a.Reason = "claim", "Free ready work exists; run bw crack before starting."
			return a
		}
	}
	if snap.catalog.IdleReview == nil || *snap.catalog.IdleReview {
		rs, _ := reviews(snap.state.Files)
		for _, r := range rs {
			if r.Author != who && reviewAllowed(o, snap.catalog, r.Task) && (r.State == "requested" && r.Reviewer == "" || r.heldBy(who)) {
				a.Action, a.Review, a.Reason = "review", r.ID, "Take review "+r.ID+" with bw review take "+r.ID+"; follow the project's review process, then run bw idle."
				return a
			}
		}
	}
	if eligibleWaiting {
		a.Action, a.Reason = "wait", "Work matching your roles and effort remains blocked or held; run bw wait."
		return a
	}
	a.Reason = "No work remains for your roles and effort; stop."
	if len(needed) > 0 {
		a.Reason += " Remaining ready work requires: " + strings.Join(needed, "; ")
	}
	return a
}

func executeRead(o options, root string) (outcome, error) {
	return executeReadContext(context.Background(), o, root)
}
func executeReadContext(ctx context.Context, o options, root string) (outcome, error) {
	who := ""
	var err error
	if o.command == "idle" || o.command == "todo" && o.mine {
		if who, err = identity(o, root); err != nil {
			return outcome{}, err
		}
	}
	s, err := openStoreContext(ctx, root)
	if err != nil {
		return outcome{}, err
	}
	defer os.RemoveAll(s.dir)
	snap, err := s.load()
	if err != nil {
		return outcome{}, err
	}
	out := snapshotOutcome(snap)
	if snap.tip == "" {
		return out, fail(1, "State is not initialized. Run bw init.")
	}
	now := time.Now().UTC()
	switch o.command {
	case "idle":
		resources, e := resourceReport(snap.catalog, snap.state, root)
		if e != nil {
			return out, e
		}
		a := workerAdvice(o, who, snap, now, resources)
		out.result = a
		if a.Action == "stop" {
			return out, fail(3, "%s", a.Reason)
		}
	case "doctor":
		out.result, err = resourceReport(snap.catalog, snap.state, root)
	case "shape":
		out.result = planShape(snap.catalog, snap.state)
	case "todo":
		o.as = who
		out.result = todoRows(o, snap, now)
	case "workers":
		log, e := s.taskHistory(snap)
		if e != nil {
			return out, e
		}
		ws, e := workerDetails(snap, log, now)
		err = e
		if len(o.args) > 0 {
			filtered := WorkerList{}
			for _, w := range ws {
				if w.Worker == o.args[0] {
					filtered = append(filtered, w)
				}
			}
			if len(filtered) == 0 {
				return out, fail(1, "Unknown worker %q", o.args[0])
			}
			ws = filtered
		}
		out.result = ws
	case "summary":
		out.result, err = summarize(snap, now)
	case "tasks":
		rows := TaskList{}
		for _, row := range taskRows(snap, now) {
			if len(o.statuses) == 0 || slices.Contains(o.statuses, row.State) {
				rows = append(rows, row)
			}
		}
		needHistory := slices.ContainsFunc(rows, func(r TaskRow) bool { return r.State == "done" })
		if needHistory {
			log, e := s.taskHistory(snap)
			if e != nil {
				return out, e
			}
			for _, e := range log {
				if e.Kind == "done" {
					for i := range rows {
						if rows[i].ID == e.Task {
							ts := e.At
							rows[i].DoneAt = &ts
						}
					}
				}
			}
		}
		out.result = rows
	case "stats":
		log, e := s.taskHistory(snap)
		if e != nil {
			return out, e
		}
		out.result, err = statistics(o, snap, log, now)
	}
	out.result = addLinks(out.result, snap.catalog, root)
	return out, err
}

func clean(s string) string { return strings.Join(strings.Fields(displayText(s)), " ") }
func printTasks(w io.Writer, rows TaskList) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "No tasks to show.")
		return
	}
	cells := [][]string{}
	for _, r := range rows {
		state, worker, detail := strings.ToUpper(activityLabel(r.View)), r.Holder, fmt.Sprintf("p%d", r.Priority)
		if worker == "" {
			worker = "-"
		}
		switch r.State {
		case "claimed":
			detail = r.LastWrite + " " + r.Idle + " ago"
			if r.ReviewStatus != "" {
				detail += "; " + r.ReviewStatus
			}
			if r.Stale {
				state += " (stale)"
			}
		case "waiting":
			parts := []string{}
			for _, b := range r.BlockedBy {
				text := b.ID + " (" + b.State
				if b.Holder != "" {
					text += " by " + b.Holder
					if b.Idle != "" {
						text += ", active " + b.Idle + " ago"
					}
					if b.Stale {
						text += ", stale"
					}
				}
				parts = append(parts, text+")")
			}
			detail = "on " + strings.Join(parts, ", ")
		case "done":
			detail = "completion time unavailable"
			if r.DoneAt != nil {
				detail = r.DoneAt.Format(time.RFC3339) + " (" + age(time.Since(*r.DoneAt)) + " ago)"
			}
		}
		cells = append(cells, []string{state, r.ID, worker, detail, r.Title + metadataText(r.View)})
	}
	printTable(w, []string{"STATE", "TASK", "WORKER", "DETAILS", "TITLE"}, cells)
}
func printSummary(w io.Writer, s Summary) {
	if s.Description != "" {
		fmt.Fprintln(w, s.Description)
	}
	fmt.Fprintf(w, "Useful workers now: %d\n", s.UsefulWorkers)
	fmt.Fprintf(w, "Tasks: %d claimed, %d ready, %d waiting, %d done\n", s.Counts["claimed"], s.Counts["ready"], s.Counts["waiting"], s.Counts["done"])
	if len(s.Claims) > 0 {
		fmt.Fprintln(w, "Active claims:")
		printViews(w, s.Claims)
	}
	fmt.Fprintf(w, "Reviews: %d waiting, %d taken\n", s.ReviewWaiting, s.ReviewTaken)
	fmt.Fprintf(w, "Questions: %d unanswered; %d need a coordinator\n", len(s.Questions), s.CoordinatorQuestions)
	fmt.Fprintln(w, s.Progress)
}

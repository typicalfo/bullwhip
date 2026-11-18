package bw

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// monitorData is one coherent state/catalog snapshot. Polls don't walk history
// or infer worker execution from ownership; staleness is only inactivity.
type monitorData struct {
	Project      string         `json:"project,omitempty"`
	Description  string         `json:"description,omitempty"`
	Todo         Todo           `json:"todo"`
	Shape        Shape          `json:"shape"`
	Resources    ResourceReport `json:"resources"`
	Workers      WorkerList     `json:"workers"`
	Questions    []Question     `json:"questions"`
	Reviews      []Review       `json:"reviews"`
	Coordinators []string       `json:"coordinators"`
	Tasks        TaskList       `json:"tasks"`
	Summary      Summary        `json:"summary"`
	Notes        []Event        `json:"notes"`
	Source       string         `json:"catalog"`
	Stale        int            `json:"stale_claims"`
}

func readMonitor(ctx context.Context, root string) (monitorData, error) {
	s, err := openStoreContext(ctx, root)
	if err != nil {
		return monitorData{}, err
	}
	defer os.RemoveAll(s.dir)
	snap, err := s.load()
	if err != nil {
		return monitorData{}, err
	}
	if snap.tip == "" {
		return monitorData{}, fail(1, "State is not initialized. Run bw init.")
	}
	data, err := monitorSnapshot(snap, time.Now().UTC())
	if err == nil {
		data.Project = filepath.Base(root)
		data.Resources, err = resourceReport(snap.catalog, snap.state, root)
		if err != nil {
			return data, err
		}
		history, e := s.taskHistory(snap)
		if e != nil {
			return data, e
		}
		data.Workers, e = workerDetails(snap, history, time.Now().UTC())
		if e != nil {
			return data, e
		}
		data.Workers = addLinks(data.Workers, snap.catalog, root).(WorkerList)
		data.Todo = addLinks(data.Todo, snap.catalog, root).(Todo)
		data.Tasks = addLinks(data.Tasks, snap.catalog, root).(TaskList)
		data.Reviews = addLinks(data.Reviews, snap.catalog, root).([]Review)
	}
	return data, err
}

func monitorSnapshot(snap snapshot, now time.Time) (monitorData, error) {
	summary, err := summarize(snap, now)
	if err != nil {
		return monitorData{}, err
	}
	notes, err := events(snap.state.Files["notes.jsonl"])
	if err != nil {
		return monitorData{}, fail(1, "Invalid note state: %v", err)
	}
	qs, err := questionList(snap.state.Files)
	if err != nil {
		return monitorData{}, err
	}
	rs, err := reviews(snap.state.Files)
	if err != nil {
		return monitorData{}, err
	}
	for i := range rs {
		rs[i] = reviewMetadata(snap.catalog, rs[i])
	}
	data := monitorData{Description: snap.catalog.Description, Todo: todoRows(options{}, snap, now), Shape: planShape(snap.catalog, snap.state), Questions: qs, Reviews: rs, Coordinators: snap.catalog.Coordinators, Tasks: taskRows(snap, now), Summary: summary, Notes: notes, Source: snap.source}
	for _, c := range summary.Claims {
		if c.Stale {
			data.Stale++
		}
	}
	return data, nil
}

package bw

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Task transitions already exist as state.json changes in Git. Heartbeats and
// retries are not new claims. Board timestamps supply forced-release attribution.
type TaskEvent struct {
	Roles       []string             `json:"roles,omitempty"`
	Effort      string               `json:"effort,omitempty"`
	StepTicks   map[string]time.Time `json:"step_ticks,omitempty"`
	StepCatalog []Step               `json:"step_catalog,omitempty"`
	At          time.Time            `json:"at"`
	Task        string               `json:"task"`
	Kind        string               `json:"kind"`
	Holder      string               `json:"holder"`
	Actor       string               `json:"actor"`
	Reason      string               `json:"reason,omitempty"`
	Forced      bool                 `json:"forced"`
}

func batchBlob(r *bufio.Reader) ([]byte, error) {
	header, err := r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(header)
	if len(fields) == 2 && fields[1] == "missing" {
		return nil, nil
	}
	if len(fields) != 3 || fields[1] != "blob" {
		return nil, fmt.Errorf("invalid history object header")
	}
	n, err := strconv.Atoi(fields[2])
	if err != nil || n < 0 || n > 4<<20 {
		return nil, fmt.Errorf("history object exceeds 4 MiB or has invalid size")
	}
	data := make([]byte, n+1)
	if _, err = io.ReadFull(r, data); err != nil {
		return nil, err
	}
	if data[n] != '\n' {
		return nil, fmt.Errorf("invalid history object boundary")
	}
	return data[:n], nil
}

func (s *store) taskHistory(snap snapshot) ([]TaskEvent, error) {
	// Fetch full ancestry into the temporary store only. Analyze the original
	// snapshot tip even if a concurrent writer publishes a newer descendant.
	if _, err := gitContext(s.ctx, s.dir, nil, "fetch", "--quiet", "--no-tags", "--depth=2147483647", "--", s.remote, "+"+stateRef+":refs/bw/state-history"); err != nil {
		return nil, fail(4, "Cannot fetch state history%s. Check Git access and retry.", detail(err))
	}
	data, err := gitContext(s.ctx, s.dir, nil, "log", "--first-parent", "--reverse", "--format=%H%x09%ct%x09%P", snap.tip)
	if err != nil {
		return nil, fail(1, "Cannot read state history%s", detail(err))
	}
	commits := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	root := strings.SplitN(commits[0], "\t", 2)[0]
	commit, err := gitContext(s.ctx, s.dir, nil, "cat-file", "commit", root)
	if err != nil {
		return nil, fail(1, "Cannot read state history root%s", detail(err))
	}
	head, _, _ := strings.Cut(string(commit), "\n\n")
	if strings.Contains(head, "\nparent ") {
		return nil, fail(1, "State history is incomplete; restore its original ancestry before running stats.")
	}
	result := []TaskEvent{}
	before := State{Tasks: map[string]Mark{}}
	notesCount := 0
	var last time.Time
	for start := 0; start < len(commits); start += 64 {
		end := min(start+64, len(commits))
		var input strings.Builder
		for _, line := range commits[start:end] {
			fields := strings.Split(line, "\t")
			if len(fields) != 3 || len(strings.Fields(fields[2])) > 1 || start == 0 && line == commits[0] && fields[2] != "" {
				return nil, fail(1, "State history is incomplete or merged; stats require the original linear history.")
			}
			input.WriteString(fields[0] + ":state.json\n" + fields[0] + ":notes.jsonl\n")
		}
		output, e := gitContext(s.ctx, s.dir, []byte(input.String()), "cat-file", "--batch")
		if e != nil {
			return nil, fail(1, "Cannot read historical state%s", detail(e))
		}
		reader := bufio.NewReader(bytes.NewReader(output))
		for _, line := range commits[start:end] {
			fields := strings.Split(line, "\t")
			sec, e := strconv.ParseInt(fields[1], 10, 64)
			if e != nil {
				return nil, fail(1, "Invalid state commit timestamp")
			}
			at := time.Unix(sec, 0).UTC()
			blob, e := batchBlob(reader)
			if e != nil || blob == nil {
				return nil, fail(1, "Cannot read historical state.json: %v", e)
			}
			var after State
			if e = decode(blob, &after); e == nil {
				e = after.validate()
			}
			if e != nil {
				return nil, fail(1, "Invalid historical state: %v", e)
			}
			blob, e = batchBlob(reader)
			if e != nil {
				return nil, fail(1, "Cannot read historical notes: %v", e)
			}
			notes, e := events(blob)
			if e != nil || len(notes) < notesCount {
				return nil, fail(1, "Invalid historical notes: %v", e)
			}
			freshNotes := notes[notesCount:]
			notesCount = len(notes)
			emit := func(e TaskEvent) {
				if e.At.Before(last) {
					e.At = last
				}
				last = e.At
				result = append(result, e)
			}
			for _, task := range snap.catalog.Tasks {
				old, had := before.Tasks[task.ID]
				mark, has := after.Tasks[task.ID]
				same := had && has && old.Holder == mark.Holder && old.Since.Equal(mark.Since)
				if had && (!same || old.Done && !mark.Done) {
					kind := "released"
					if old.Done {
						kind = "reopened"
					}
					e := TaskEvent{At: at, Task: task.ID, Kind: kind, Holder: old.Holder}
					prefix := "Released " + old.Holder + "'s claim: "
					for _, n := range freshNotes {
						if n.Task == task.ID && strings.HasPrefix(n.Note, prefix) {
							e.Actor, e.Reason, e.Forced, e.At = n.Actor, strings.TrimPrefix(n.Note, prefix), true, n.TS
						}
					}
					emit(e)
				}
				if has && (!same || old.Done && !mark.Done) {
					emit(TaskEvent{At: mark.Since, Task: task.ID, Kind: "claimed", Holder: mark.Holder, Actor: mark.Holder, Roles: mark.Roles, Effort: mark.Effort})
				}
				if has && (!maps.Equal(old.StepTicks, mark.StepTicks) || !slices.Equal(old.StepCatalog, mark.StepCatalog)) {
					emit(TaskEvent{At: mark.active(), Task: task.ID, Kind: "steps", Holder: mark.Holder, Actor: mark.Holder, StepTicks: mark.StepTicks, StepCatalog: mark.StepCatalog})
				}
				if same && !mark.Done && mark.active().After(old.active()) {
					emit(TaskEvent{At: mark.active(), Task: task.ID, Kind: "activity", Holder: mark.Holder, Actor: mark.Holder})
				}
				if has && mark.Done && (!same || !old.Done) {
					emit(TaskEvent{At: at, Task: task.ID, Kind: "done", Holder: mark.Holder, Actor: mark.Holder})
				}
			}
			before = after
		}
	}
	return result, nil
}

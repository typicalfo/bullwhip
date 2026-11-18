package bw

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

type Task struct {
	Roles        []string `json:"roles,omitempty"`
	ReviewRoles  []string `json:"review_roles,omitempty"`
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Ref          string   `json:"ref,omitempty"`
	Needs        []string `json:"needs,omitempty"`
	Priority     int      `json:"priority,omitempty"`
	Size         string   `json:"size,omitempty"`
	Effort       string   `json:"effort,omitempty"`
	ReviewEffort string   `json:"review_effort,omitempty"`
	Steps        []Step   `json:"steps,omitempty"`
}

type Catalog struct {
	Description     string         `json:"description,omitempty"`
	Roles           []string       `json:"roles,omitempty"`
	IdleReview      *bool          `json:"idle_review,omitempty"`
	IdleMinutes     int            `json:"idle_minutes,omitempty"`
	Resources       ResourceLimits `json:"resources,omitzero"`
	RepoURL         string         `json:"repo_url,omitempty"`
	Forge           string         `json:"forge,omitempty"`
	SelfReviewAfter string         `json:"self_review_after,omitempty"`
	StaleHours      int            `json:"stale_hours,omitempty"`
	// Coordinators may release any claim with --force and a reason.
	Coordinators []string `json:"coordinators,omitempty"`
	Tasks        []Task   `json:"tasks"`
}

type Mark struct {
	Roles  []string  `json:"roles,omitempty"`
	Effort string    `json:"effort,omitempty"`
	Holder string    `json:"holder,omitempty"`
	Since  time.Time `json:"since,omitempty"`
	// Seen is the holder's latest bw write (claim, heartbeat or board activity).
	Seen        time.Time            `json:"seen,omitzero"`
	Done        bool                 `json:"done,omitempty"`
	StepTicks   map[string]time.Time `json:"step_ticks,omitempty"`
	StepCatalog []Step               `json:"step_catalog,omitempty"`
}

// active is when the holder last showed signs of life.
func (m Mark) active() time.Time {
	if m.Seen.After(m.Since) {
		return m.Seen
	}
	return m.Since
}

// touch records activity for every open claim the worker holds and reports
// whether the worker holds any. A seen time ahead of this clock is kept.
func (s State) touch(who string, now time.Time) bool {
	held := false
	for id, m := range s.Tasks {
		if m.Holder == who && !m.Done {
			held = true
			if m.Seen.Before(now) {
				m.Seen = now
				s.Tasks[id] = m
			}
		}
	}
	return held
}

type State struct {
	BoardVersion int               `json:"board_version,omitempty"`
	Tasks        map[string]Mark   `json:"tasks"`
	Files        map[string][]byte `json:"-"`
	// Ops holds the IDs of the most recent updates, newest last.
	Ops []string `json:"ops,omitempty"`
}

const maxOps = 64

var opID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (s State) hasOp(op string) bool {
	for _, o := range s.Ops {
		if o == op {
			return true
		}
	}
	return false
}

type View struct {
	Activity     string `json:"activity,omitempty"`
	LastWrite    string `json:"last_write,omitempty"`
	ReviewStatus string `json:"review_status,omitempty"`
	Task
	Links          map[string]string `json:"links,omitempty"`
	Progress       string            `json:"progress,omitempty"`
	CompletedSteps []string          `json:"completed_steps,omitempty"`
	UntickedSteps  []string          `json:"unticked_steps,omitempty"`
	StepChanges    *StepChanges      `json:"step_changes,omitempty"`
	Section        string            `json:"section,omitempty"`
	Warning        string            `json:"warning,omitempty"`
	State          string            `json:"state"`
	Holder         string            `json:"holder,omitempty"`
	Since          *time.Time        `json:"since,omitempty"`
	Age            string            `json:"age,omitempty"`
	// Idle is how long ago the holder was last active.
	Idle    string   `json:"idle,omitempty"`
	Stale   bool     `json:"stale"`
	Unlocks []string `json:"unlocks"`
	// Unchanged means the requested outcome already held, so nothing was written.
	Unchanged bool `json:"unchanged,omitempty"`
}

// Reject duplicate keys as well as unknown fields: losing a task or holder silently
// would defeat the only coordination guarantee this tool provides.
func decode(data []byte, dst any) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("expected a JSON object")
	}
	if len(data) > 4<<20 {
		return fmt.Errorf("JSON exceeds 4 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	var value func() error
	value = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		for d.More() {
			if delim == '{' {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key := k.(string)
				if seen[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
			}
			if e := value(); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	if e := value(); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fmt.Errorf("expected one JSON document")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

var taskID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func (c *Catalog) validate() error {
	if err := validateMetadata(*c); err != nil {
		return err
	}
	if c.SelfReviewAfter != "" {
		d, e := time.ParseDuration(c.SelfReviewAfter)
		if e != nil || d < 0 {
			return fmt.Errorf("self_review_after must be a nonnegative duration such as 30m")
		}
	}
	if c.Tasks == nil {
		return fmt.Errorf("tasks must be an array; run bw help format")
	}
	if c.StaleHours < 0 {
		return fmt.Errorf("stale_hours must be positive or omitted")
	}
	for i, name := range c.Coordinators {
		if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) || strings.ContainsAny(name, "\r\n\x00") || slices.Contains(c.Coordinators[:i], name) {
			return fmt.Errorf("coordinators must be distinct worker names")
		}
	}
	ids := map[string]Task{}
	for _, t := range c.Tasks {
		if !taskID.MatchString(t.ID) || strings.TrimSpace(t.Title) == "" {
			return fmt.Errorf("each task needs an id and title; run bw help format")
		}
		if _, ok := ids[t.ID]; ok {
			return fmt.Errorf("duplicate task id %q", t.ID)
		}
		ids[t.ID] = t
	}
	visited := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if visited[id] == 1 {
			return fmt.Errorf("dependency cycle at %q", id)
		}
		if visited[id] == 2 {
			return nil
		}
		visited[id] = 1
		for _, n := range ids[id].Needs {
			if _, ok := ids[n]; !ok {
				return fmt.Errorf("task %q needs unknown task %q", id, n)
			}
			if e := visit(n); e != nil {
				return e
			}
		}
		visited[id] = 2
		return nil
	}
	for _, t := range c.Tasks {
		if e := visit(t.ID); e != nil {
			return e
		}
	}
	return nil
}

// parseCatalog decodes and validates a task file, then orders it by priority
// (highest first) and ID.
func parseCatalog(data []byte) (Catalog, error) {
	var c Catalog
	if e := decode(data, &c); e != nil {
		return c, e
	}
	if e := c.validate(); e != nil {
		return c, e
	}
	sort.SliceStable(c.Tasks, func(i, j int) bool {
		if c.Tasks[i].Priority != c.Tasks[j].Priority {
			return c.Tasks[i].Priority > c.Tasks[j].Priority
		}
		return c.Tasks[i].ID < c.Tasks[j].ID
	})
	return c, nil
}

func (s State) validate() error {
	if s.BoardVersion < 0 || s.BoardVersion > 1 {
		return fmt.Errorf("unsupported board version; upgrade bw")
	}
	if s.Tasks == nil {
		return fmt.Errorf("state tasks must be an object")
	}
	for id, m := range s.Tasks {
		if _, err := workerRoles(strings.Join(m.Roles, ",")); err != nil || !validEffort(m.Effort) {
			return fmt.Errorf("invalid recorded roles or effort for %s", id)
		}
		if len(m.StepTicks) > 32 || len(m.StepCatalog) > 32 {
			return fmt.Errorf("too many steps for %s", id)
		}
		for key, at := range m.StepTicks {
			if key == "" || at.IsZero() {
				return fmt.Errorf("invalid step tick for %s", id)
			}
		}
		if !taskID.MatchString(id) || m.Holder == "" || m.Since.IsZero() {
			return fmt.Errorf("invalid state mark for %q", id)
		}
	}
	if len(s.Ops) > maxOps {
		return fmt.Errorf("too many op IDs")
	}
	for _, o := range s.Ops {
		if !opID.MatchString(o) {
			return fmt.Errorf("invalid op ID")
		}
	}
	return nil
}

func (c Catalog) views(s State, now time.Time) []View {
	result := make([]View, 0, len(c.Tasks))
	board := readActivityBoard(s)
	for _, t := range c.Tasks {
		v := View{Task: t, State: "ready", Unlocks: []string{}}
		for _, n := range t.Needs {
			if !s.Tasks[n].Done {
				v.State = "waiting"
			}
		}
		m, ok := s.Tasks[t.ID]
		if ok {
			if m.Done {
				v.State = "done"
			} else {
				v.State = "claimed"
				v.Stale = c.stale(m.active(), now)
				v.Idle = age(now.Sub(m.active()))
			}
			since := m.Since
			v.Holder = m.Holder
			v.Since = &since
			v.Age = age(now.Sub(m.Since))
		}
		for _, other := range c.Tasks {
			for _, n := range other.Needs {
				if n == t.ID {
					v.Unlocks = append(v.Unlocks, other.ID)
					break
				}
			}
		}
		decorateActivity(&v, m, c, board, now)
		decorateSteps(&v, m)
		result = append(result, v)
	}
	return result
}

// DefaultStaleHours is how long a claim may go without holder activity
// before anyone may release it with --force.
const DefaultStaleHours = 4

func (c Catalog) staleHours() int {
	if c.StaleHours == 0 {
		return DefaultStaleHours
	}
	return c.StaleHours
}

// stale reports a claim whose holder has been inactive for stale_hours.
func (c Catalog) stale(active, now time.Time) bool {
	return now.Sub(active).Hours() >= float64(c.staleHours())
}

func (c Catalog) coordinator(who string) bool {
	return slices.Contains(c.Coordinators, who)
}

// age is a short, human duration: 42s, 17m, 3h, 5d.
func age(d time.Duration) string {
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
}

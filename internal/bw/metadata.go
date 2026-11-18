package bw

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Step struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text"`
}

func (s *Step) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &s.Text)
	}
	type plain Step
	return decode(b, (*plain)(s))
}
func (s Step) key() string {
	if s.ID != "" {
		return s.ID
	}
	return s.Text
}

type StepChanges struct {
	Added    []string `json:"added"`
	Removed  []string `json:"removed"`
	Reopened []string `json:"reopened"`
}

func validEffort(s string) bool { return slices.Contains([]string{"", "low", "medium", "high"}, s) }
func effortRank(s string) int   { return slices.Index([]string{"", "low", "medium", "high"}, s) }
func allowsEffort(level, required string) bool {
	return level == "" || effortRank(level) >= effortRank(required)
}
func reviewEffort(c Catalog, id string) string {
	for _, t := range c.Tasks {
		if t.ID == id {
			if t.ReviewEffort != "" {
				return t.ReviewEffort
			}
			return t.Effort
		}
	}
	return ""
}
func validateMetadata(c Catalog) error {
	if err := validatePlanning(c); err != nil {
		return err
	}
	if c.Forge != "" && c.Forge != "github" {
		return fmt.Errorf("forge must be github")
	}
	if c.RepoURL != "" {
		u, e := url.Parse(c.RepoURL)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("repo_url must be an HTTP(S) repository web URL without credentials, query or fragment")
		}
	}
	for _, t := range c.Tasks {
		if !slices.Contains([]string{"", "xs", "s", "m", "l", "xl"}, t.Size) || !validEffort(t.Effort) || !validEffort(t.ReviewEffort) {
			return fmt.Errorf("invalid size or effort for %s", t.ID)
		}
		if t.Steps != nil && (len(t.Steps) < 1 || len(t.Steps) > 32) {
			return fmt.Errorf("%s steps must have 1–32 entries", t.ID)
		}
		keys := map[string]bool{}
		for _, s := range t.Steps {
			if strings.TrimSpace(s.Text) == "" || utf8.RuneCountInString(s.Text) > 120 || s.ID != "" && !taskID.MatchString(s.ID) || keys[s.key()] {
				return fmt.Errorf("invalid or duplicate step in %s", t.ID)
			}
			keys[s.key()] = true
		}
	}
	return nil
}
func decorateSteps(v *View, m Mark) {
	if len(v.Steps) == 0 && len(m.StepCatalog) == 0 {
		return
	}
	completed := map[string]bool{}
	current := map[string]bool{}
	old := map[string]bool{}
	for _, s := range m.StepCatalog {
		old[s.key()] = true
	}
	changes := &StepChanges{Added: []string{}, Removed: []string{}, Reopened: []string{}}
	for _, s := range v.Steps {
		k := s.key()
		current[k] = true
		if _, ok := m.StepTicks[k]; ok {
			v.CompletedSteps = append(v.CompletedSteps, k)
			completed[k] = true
		} else {
			v.UntickedSteps = append(v.UntickedSteps, s.Text)
		}
		if len(m.StepCatalog) > 0 && !old[k] {
			changes.Added = append(changes.Added, s.Text)
		}
	}
	for i, s := range m.StepCatalog {
		if !current[s.key()] {
			changes.Removed = append(changes.Removed, s.Text)
			if s.ID == "" && i < len(v.Steps) && v.Steps[i].ID == "" && !old[v.Steps[i].key()] {
				changes.Reopened = append(changes.Reopened, v.Steps[i].Text)
			}
		}
	}
	v.Progress = fmt.Sprintf("%d/%d", len(completed), len(v.Steps))
	if len(changes.Added)+len(changes.Removed) > 0 {
		v.StepChanges = changes
	}
}
func reconcileSteps(state State, c Catalog) {
	for _, t := range c.Tasks {
		m, ok := state.Tasks[t.ID]
		if !ok {
			continue
		}
		keys := map[string]bool{}
		for _, s := range t.Steps {
			keys[s.key()] = true
		}
		for k := range m.StepTicks {
			if !keys[k] {
				delete(m.StepTicks, k)
			}
		}
		state.Tasks[t.ID] = m
	}
}
func tickStep(o options, who string, snap snapshot, v View, now time.Time) (any, bool, error) {
	m := snap.state.Tasks[v.ID]
	if m.Done || m.Holder != who {
		return nil, false, fail(2, "Only the holder of an unfinished claim can tick steps")
	}
	n, e := strconv.Atoi(o.args[1])
	if e != nil || n < 1 || n > len(v.Steps) {
		return nil, false, fail(1, "Step must be between 1 and %d", len(v.Steps))
	}
	reconcileSteps(snap.state, snap.catalog)
	m = snap.state.Tasks[v.ID]
	if m.StepTicks == nil {
		m.StepTicks = map[string]time.Time{}
	}
	k := v.Steps[n-1].key()
	if o.undo {
		delete(m.StepTicks, k)
	} else if _, ok := m.StepTicks[k]; !ok {
		m.StepTicks[k] = now
	}
	m.StepCatalog = append([]Step{}, v.Steps...)
	snap.state.Tasks[v.ID] = m
	snap.state.touch(who, now)
	result, _ := findView(snap.catalog, snap.state, v.ID)
	return result, true, nil
}
func metadataText(v View) string {
	parts := []string{}
	if len(v.Roles) > 0 {
		parts = append(parts, "roles "+strings.Join(v.Roles, ","))
	}
	if v.Size != "" {
		parts = append(parts, "size "+v.Size)
	}
	if v.Effort != "" {
		parts = append(parts, "effort "+v.Effort)
	}
	if v.Progress != "" {
		parts = append(parts, "steps "+v.Progress)
	}
	if len(parts) == 0 {
		return ""
	}
	return " [" + strings.Join(parts, ", ") + "]"
}

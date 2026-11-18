package bw

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// heldBy is shared by holds and review take's retry path, including self-reviews.
func (r Review) heldBy(who string) bool {
	return r.Reviewer != "" && r.Reviewer == who && (r.State == "taken" || r.State == "requested")
}

type HeldClaim struct {
	Links map[string]string `json:"links,omitempty"`
	ID    string            `json:"id"`
	Title string            `json:"title"`
	Since time.Time         `json:"since"`
	Age   string            `json:"age"`
	Idle  string            `json:"idle"`
	Stale bool              `json:"stale"`
}
type HeldReview struct {
	Links  map[string]string `json:"links,omitempty"`
	ID     string            `json:"id"`
	Task   string            `json:"task"`
	Branch string            `json:"branch"`
	State  string            `json:"state"`
	Self   bool              `json:"self"`
	Since  time.Time         `json:"since"`
	Age    string            `json:"age"`
}
type Holdings struct {
	Worker  string       `json:"worker"`
	Claims  []HeldClaim  `json:"claims"`
	Reviews []HeldReview `json:"reviews"`
}

func holdings(who string, snap snapshot, now time.Time) (Holdings, error) {
	h := Holdings{Worker: who, Claims: []HeldClaim{}, Reviews: []HeldReview{}}
	rows, err := list(options{command: "status"}, snap)
	if err != nil {
		return h, err
	}
	for _, v := range rows.([]View) {
		if v.Holder == who {
			h.Claims = append(h.Claims, HeldClaim{nil, v.ID, v.Title, *v.Since, age(now.Sub(*v.Since)), v.Idle, v.Stale})
		}
	}
	sort.Slice(h.Claims, func(i, j int) bool { return h.Claims[i].ID < h.Claims[j].ID })
	reviews, err := reviews(snap.state.Files)
	if err != nil {
		return h, fail(1, "Invalid review state: %v", err)
	}
	for _, r := range reviews {
		if !r.heldBy(who) {
			continue
		}
		// Re-requests retain ownership and its original age. A new take starts it.
		var since time.Time
		for _, e := range r.Events {
			if e.Event == "taken" {
				since = e.TS
			}
		}
		h.Reviews = append(h.Reviews, HeldReview{nil, r.ID, r.Task, r.Branch, r.State, r.Self, since, age(now.Sub(since))})
	}
	return h, nil
}

// shellArg keeps the suggested release commands usable for names containing spaces.
func shellArg(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\r\n'\"\\$`;&|<>()*?[]{}!#~") && !strings.HasPrefix(s, "-") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
func (h Holdings) text() string {
	if len(h.Claims)+len(h.Reviews) == 0 {
		return clean(h.Worker) + " holds nothing."
	}
	var b strings.Builder
	claim, review := "claim", "review"
	if len(h.Claims) != 1 {
		claim += "s"
	}
	if len(h.Reviews) != 1 {
		review += "s"
	}
	fmt.Fprintf(&b, "%s holds %d %s and %d %s:\n", clean(h.Worker), len(h.Claims), claim, len(h.Reviews), review)
	commands := []string{}
	for _, c := range h.Claims {
		stale := ""
		if c.Stale {
			stale = " STALE"
		}
		fmt.Fprintf(&b, "  claim: %s%s\n    title: %s\n    since: %s (age %s; idle %s)\n", clean(c.ID), stale, clean(c.Title), c.Since.Format(time.RFC3339), c.Age, c.Idle)
		commands = append(commands, "bw release "+shellArg(c.ID)+" --as "+shellArg(h.Worker))
	}
	for _, r := range h.Reviews {
		fmt.Fprintf(&b, "  review: %s\n    task: %s\n    state: %s\n    since: %s (%s)\n", clean(r.ID), clean(r.Task), r.State, r.Since.Format(time.RFC3339), r.Age)
		commands = append(commands, "bw review release "+shellArg(r.ID)+" --as "+shellArg(h.Worker))
	}
	return b.String() + "To release:\n  " + strings.Join(commands, "\n  ")
}
func executeHolds(o options, root string) (outcome, error) {
	if len(o.args) > 0 {
		o.as = o.args[0]
	}
	who, err := identity(o, root)
	if err != nil {
		return outcome{}, err
	}
	s, err := openStore(root)
	if err != nil {
		return outcome{}, err
	}
	defer os.RemoveAll(s.dir)
	snap, err := s.load()
	if err != nil {
		return outcome{}, err
	}
	if snap.tip == "" {
		return outcome{}, fail(1, "State is not initialized. Run bw init.")
	}
	h, err := holdings(who, snap, time.Now().UTC())
	if err != nil {
		return outcome{source: snap.source}, err
	}
	out := outcome{result: addLinks(h, snap.catalog, root), source: snap.source}
	if o.check && len(h.Claims)+len(h.Reviews) > 0 {
		return out, fail(2, "%s", h.text())
	}
	return out, nil
}

package bw

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func heldResult(t *testing.T, r response) Holdings {
	t.Helper()
	var h Holdings
	if err := json.Unmarshal(r.Data, &h); err != nil {
		t.Fatal(err, string(r.Data))
	}
	if h.Claims == nil || h.Reviews == nil {
		t.Fatal("arrays must be non-null", string(r.Data))
	}
	return h
}
func TestHoldsClaimsAndIdentity(t *testing.T) {
	root := fixture(t, graph)
	commitFixture(t, root)
	invoke(t, root, 0, "claim", "a", "--as", "worker")
	tip := mustGit(t, root, "ls-remote", "origin", stateRef)
	// Reads must work even when the remote refuses all writes.
	remote := mustGit(t, root, "remote", "get-url", "origin")
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	h := heldResult(t, invoke(t, root, 0, "holds", "worker"))
	if h.Worker != "worker" || len(h.Claims) != 1 || h.Claims[0].ID != "a" || h.Claims[0].Title != "A" || h.Claims[0].Since.IsZero() || h.Claims[0].Age == "" {
		t.Fatal(h)
	}
	r := invoke(t, root, 2, "holds", "worker", "--check", "--as", "other")
	h = heldResult(t, r)
	if h.Worker != "worker" || !strings.HasPrefix(r.Catalog, "origin@") || !strings.Contains(r.Error, "bw release a --as worker") {
		t.Fatal(r, h)
	}
	heldResult(t, invoke(t, root, 0, "holds", "unknown", "--check"))
	invoke(t, root, 1, "holds")
	h = heldResult(t, invoke(t, root, 2, "holds", "--as", "worker", "--check"))
	if h.Worker != "worker" {
		t.Fatal(h)
	}
	sub := filepath.Join(root, "sub", "dir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	heldResult(t, invoke(t, sub, 2, "holds", "worker", "--check"))
	cmd := command(root, "holds", "--check")
	cmd.Env = append(cmd.Env, "BW_AS=worker")
	out, err := cmd.Output()
	r, code, err := outputCode(out, err)
	if err != nil || code != 2 || heldResult(t, r).Worker != "worker" {
		t.Fatal(code, err, string(out))
	}
	cmd = command(root, "holds", "--as", "other")
	cmd.Env = append(cmd.Env, "BW_AS=worker")
	out, err = cmd.Output()
	r, code, err = outputCode(out, err)
	if err != nil || code != 0 || heldResult(t, r).Worker != "other" {
		t.Fatal(code, err, string(out))
	}
	cmd = exec.Command(binary, "holds")
	cmd.Dir, cmd.Env = root, append(os.Environ(), "BW_AS=")
	out, err = cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "git-fallback holds nothing.") {
		t.Fatal(err, string(out))
	}
	if got := mustGit(t, root, "ls-remote", "origin", stateRef); got != tip {
		t.Fatal("holds wrote state", got, tip)
	}
	if err := os.Remove(filepath.Join(remote, "hooks", "pre-receive")); err != nil {
		t.Fatal(err)
	}
	invoke(t, root, 0, "done", "a", "--as", "worker")
	if h = heldResult(t, invoke(t, root, 0, "holds", "worker", "--check")); len(h.Claims) != 0 {
		t.Fatal(h)
	}
	invoke(t, root, 0, "claim", "b", "--as", "worker")
	invoke(t, root, 0, "release", "b", "--as", "worker")
	if h = heldResult(t, invoke(t, root, 0, "holds", "worker", "--check")); len(h.Claims) != 0 {
		t.Fatal(h)
	}
	mustGit(t, root, "remote", "set-url", "origin", filepath.Join(root, "missing.git"))
	invoke(t, root, 4, "holds", "worker", "--check")
	for _, args := range [][]string{{"holds", "one", "two"}, {"holds", "--force"}, {"status", "--check"}, {"review", "list", "--check"}} {
		invoke(t, root, 1, args...)
	}
}

func TestHoldsReviewLifecycle(t *testing.T) {
	root, id := requestFixture(t)
	check := func(code, count int) {
		t.Helper()
		r := invoke(t, root, code, "holds", "reviewer", "--check")
		h := heldResult(t, r)
		if len(h.Reviews) != count || len(h.Claims) != 0 {
			t.Fatal(h)
		}
		if count > 0 && (!strings.Contains(r.Error, "bw review release "+id+" --as reviewer") || h.Reviews[0].Since.IsZero()) {
			t.Fatal(r, h)
		}
	}
	check(0, 0) // authored requests do not count for a different worker
	author := heldResult(t, invoke(t, root, 0, "holds", "author"))
	if len(author.Reviews) != 0 || len(author.Claims) != 1 {
		t.Fatal(author)
	}
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	check(2, 1)
	before := heldResult(t, invoke(t, root, 0, "holds", "reviewer")).Reviews[0].Since
	mustGit(t, root, "commit", "--quiet", "--allow-empty", "-m", "new head")
	invoke(t, root, 0, "review", "request", "a", "--branch", "feature/a", "--as", "author")
	check(2, 1)
	h := heldResult(t, invoke(t, root, 0, "holds", "reviewer"))
	if h.Reviews[0].State != "requested" || h.Reviews[0].Since != before {
		t.Fatal(h)
	}
	retry := reviewResult(t, invoke(t, root, 0, "review", "take", "--as", "reviewer"))
	if retry.ID != id || !retry.Unchanged {
		t.Fatal(retry)
	}
	notes := filepath.Join(root, "findings.md")
	if err := os.WriteFile(notes, []byte("Fix this edge case."), 0644); err != nil {
		t.Fatal(err)
	}
	invoke(t, root, 0, "review", "changes", id, "--notes", notes, "--as", "reviewer")
	check(0, 0)
	invoke(t, root, 0, "review", "request", "a", "--branch", "feature/a", "--as", "author")
	check(2, 1)
	invoke(t, root, 0, "review", "release", id, "--as", "reviewer")
	check(0, 0)
	invoke(t, root, 0, "review", "take", id, "--as", "reviewer")
	invoke(t, root, 0, "review", "approve", id, "--as", "reviewer")
	check(0, 0)
	mustGit(t, root, "push", "--quiet", "origin", "HEAD")
	invoke(t, root, 0, "review", "merged", id, "--as", "author")
	check(0, 0)
}

func TestHoldsSelfReview(t *testing.T) {
	root := fixture(t, `{"self_review_after":"0s","tasks":[{"id":"a","title":"A"}]}`)
	commitFixture(t, root)
	invoke(t, root, 0, "claim", "a", "--as", "author")
	r := reviewResult(t, invoke(t, root, 0, "review", "request", "a", "--branch", "self/a", "--as", "author"))
	invoke(t, root, 0, "review", "take", r.ID, "--self", "--as", "author")
	h := heldResult(t, invoke(t, root, 2, "holds", "author", "--check"))
	if len(h.Reviews) != 1 || !h.Reviews[0].Self {
		t.Fatal(h)
	}
	notes := filepath.Join(root, "findings.md")
	if err := os.WriteFile(notes, []byte("Fix this."), 0644); err != nil {
		t.Fatal(err)
	}
	invoke(t, root, 0, "review", "changes", r.ID, "--notes", notes, "--as", "author")
	h = heldResult(t, invoke(t, root, 0, "holds", "author"))
	if len(h.Reviews) != 0 {
		t.Fatal(h)
	}
	invoke(t, root, 0, "review", "request", "a", "--branch", "self/a", "--as", "author", "--note", "Updated")
	invoke(t, root, 0, "review", "approve", r.ID, "--as", "author")
	h = heldResult(t, invoke(t, root, 0, "holds", "author"))
	if len(h.Reviews) != 0 {
		t.Fatal(h)
	}
}

func TestHoldsFoldAndRemovedClaims(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	taken := now.Add(-48 * time.Hour)
	for _, state := range []string{"taken", "requested", "approved", "self-approved", "changes_requested", "self-changes", "released", "merged"} {
		t.Run(state, func(t *testing.T) {
			files := map[string][]byte{}
			appendEvent(files, reviewPath("feature--a"), Event{TS: taken.Add(-time.Hour), Actor: "author", Event: "requested", Head: "abc", Task: "a", Branch: "feature/a"})
			appendEvent(files, reviewPath("feature--a"), Event{TS: taken, Actor: "worker", Event: "taken"})
			if state != "taken" {
				appendEvent(files, reviewPath("feature--a"), Event{TS: now.Add(-time.Minute), Actor: "author", Event: state, Head: "def"})
			}
			snap := snapshot{catalog: Catalog{}, state: State{Files: files, Tasks: map[string]Mark{"removed": {Holder: "worker", Since: taken}, "done": {Holder: "worker", Since: taken, Done: true}}}}
			h, err := holdings("worker", snap, now)
			if err != nil {
				t.Fatal(err)
			}
			if len(h.Claims) != 1 || !h.Claims[0].Stale || h.Claims[0].Age != "2d" {
				t.Fatal(h)
			}
			pending := state == "taken" || state == "requested"
			if (len(h.Reviews) == 1) != pending {
				t.Fatal(h)
			}
			if pending && !h.Reviews[0].Since.Equal(taken) {
				t.Fatal(h)
			}
		})
	}
}

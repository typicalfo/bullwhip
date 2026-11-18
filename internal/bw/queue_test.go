package bw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMainHelpUsageAndDetails(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		r := invoke(t, root, 0, args...)
		var text string
		if err := json.Unmarshal(r.Data, &text); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(text, "Usage:") || !strings.Contains(text, "help-2") || strings.Contains(text, "Worker name:") {
			t.Fatalf("unexpected main help: %s", text)
		}
	}
	for _, args := range [][]string{{"help-2"}, {"help", "help-2"}, {"help-2", "--help"}} {
		r := invoke(t, root, 0, args...)
		if !strings.Contains(string(r.Data), "Worker name:") || !strings.Contains(string(r.Data), "Repository:") {
			t.Fatalf("missing overview: %s", r.Data)
		}
	}
}

func TestQueueAllAndAlias(t *testing.T) {
	root := fixture(t, `{"tasks":[{"id":"finished","title":"Finished","priority":9},{"id":"held","title":"Held","priority":8},{"id":"ready","title":"Ready","priority":7},{"id":"blocked","title":"Blocked","priority":6,"needs":["held"]}]}`)
	publishCatalog(t, root)
	invoke(t, root, 0, "claim", "finished", "--as", "worker")
	invoke(t, root, 0, "done", "finished", "--as", "worker")
	invoke(t, root, 0, "claim", "held", "--as", "worker")
	remote := mustGit(t, root, "remote", "get-url", "origin")
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	before := mustGit(t, root, "ls-remote", "origin", stateRef)
	refs := mustGit(t, root, "show-ref")
	for _, command := range []string{"queue", "q"} {
		rows := views(t, invoke(t, root, 0, command))
		if len(rows) != 1 || rows[0].ID != "ready" {
			t.Fatal(rows)
		}
		rows = views(t, invoke(t, root, 0, command, "--all"))
		if len(rows) != 4 {
			t.Fatal(rows)
		}
		for i, want := range []string{"done", "claimed", "ready", "waiting"} {
			if rows[i].State != want {
				t.Fatalf("%s: %+v", command, rows)
			}
		}
		text := textCommand(t, root, command, "--all")
		for _, id := range []string{"finished", "held", "ready", "blocked"} {
			if !strings.Contains(text, id) {
				t.Fatal(text)
			}
		}
	}
	if after := mustGit(t, root, "ls-remote", "origin", stateRef); after != before {
		t.Fatal("queue changed state")
	}
	if after := mustGit(t, root, "show-ref"); after != refs {
		t.Fatal("queue changed local refs")
	}
	for _, args := range [][]string{{"q", "--help"}, {"help", "q"}} {
		r := invoke(t, t.TempDir(), 0, args...)
		var help string
		if err := json.Unmarshal(r.Data, &help); err != nil || help != helpText["queue"] {
			t.Fatal(string(r.Data), err)
		}
	}
	invoke(t, root, 1, "q", "--state", "ready")
	invoke(t, root, 1, "q", "extra")
}

func TestQueueAllEmpty(t *testing.T) {
	root := fixture(t, `{"tasks":[]}`)
	if rows := views(t, invoke(t, root, 0, "q", "--all")); len(rows) != 0 {
		t.Fatal(rows)
	}
	if text := textCommand(t, root, "q", "--all"); !strings.Contains(text, "No tasks to show") {
		t.Fatal(text)
	}
}

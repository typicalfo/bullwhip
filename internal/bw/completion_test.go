package bw

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionInstallUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("ZDOTDIR", home)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		o := options{command: "completion", args: []string{"install"}, shell: shell}
		if _, e := completion(o); e != nil {
			t.Fatal(e)
		}
		out, e := completion(o)
		if e != nil || !strings.Contains(out.result.(string), "Nothing changed") {
			t.Fatal(out, e)
		}
		o.args = []string{"uninstall"}
		if _, e = completion(o); e != nil {
			t.Fatal(e)
		}
		if shell != "fish" {
			b, _ := os.ReadFile(filepath.Join(home, map[string]string{"bash": ".bashrc", "zsh": ".zshrc"}[shell]))
			if strings.Contains(string(b), "# bw completion") {
				t.Fatal(string(b))
			}
		}
	}
}
func TestCompletionValuesAndNoRemote(t *testing.T) {
	if s := completeWords([]string{"queue", "--effort"}); s != "low\nmedium\nhigh" {
		t.Fatal(s)
	}
	root := fixture(t, `{"tasks":[{"id":"local-task","title":"Local"}]}`)
	mustGit(t, root, "remote", "set-url", "origin", "ssh://unreachable.invalid/repo")
	cmd := exec.Command(binary, "_complete", "show")
	cmd.Dir = root
	if b, e := cmd.Output(); e != nil || !strings.Contains(string(b), "local-task") {
		t.Fatalf("%s %v", b, e)
	} // any remote access would fail
}

func TestCompletionRawFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"_complete", "queue", "--json", "--effort"}, &stdout, &stderr); code != 0 || stdout.String() != "low\nmedium\nhigh\n" {
		t.Fatalf("%d %s %s", code, stdout.String(), stderr.String())
	}
}

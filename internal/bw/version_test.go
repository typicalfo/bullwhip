package bw

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionUsesBinaryRevisionWithoutGit(t *testing.T) {
	revision := mustGit(t, "../..", "rev-parse", "HEAD")
	outside := t.TempDir()
	cmd := exec.Command(binary, "version")
	cmd.Dir = outside
	cmd.Env = append(os.Environ(), "PATH="+outside)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("version without Git: %v %s", err, out)
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text != "bw "+revision && text != "bw "+revision+" (modified)" {
		t.Fatalf("version must identify the build checkout: %q", out)
	}
	got := readData[string](t, invoke(t, outside, 0, "version"))
	if got != text {
		t.Fatalf("JSON %q, text %q", got, text)
	}
	invoke(t, outside, 1, "version", "unexpected")
}

func TestVersionWithoutBuildMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bw")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", path, "./cmd/bw")
	cmd.Dir = "../.."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	cmd = exec.Command(path, "version")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+dir)
	if out, err := cmd.Output(); err != nil || string(out) != "bw unknown (build commit unavailable)\n" {
		t.Fatalf("missing metadata: %v %s", err, out)
	}
}

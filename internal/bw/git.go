package bw

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mrand "math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	stateRef    = "refs/bullwhip/state"
	localState  = "refs/bw/state"
	localTasks  = "refs/bw/catalog"
	catalogPath = ".bullwhip/tasks.json"
	maxAttempts = 64
)

type store struct {
	dir, remote, local string
	ctx                context.Context
}

// gitError keeps Git's (redacted) stderr so callers can explain failures.
type gitError struct {
	code   int
	stderr string
}

func (e *gitError) Error() string { return e.stderr }

var userinfo = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]+@`)

// redact removes URL credentials, which Git can echo in transport errors.
func redact(s string) string { return userinfo.ReplaceAllString(s, "${1}***@") }

// detail returns the last line of Git's stderr for user-facing messages.
func detail(e error) string {
	var g *gitError
	if !errors.As(e, &g) || g.stderr == "" {
		return ""
	}
	lines := strings.Split(g.stderr, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(last) > 200 {
		last = last[:200] + "..."
	}
	return " (git: " + last + ")"
}

func git(dir string, input []byte, args ...string) ([]byte, error) {
	return gitContext(context.Background(), dir, input, args...)
}

func gitContext(parent context.Context, dir string, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	options := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "protocol.allow=never", "-c", "protocol.file.allow=always", "-c", "protocol.ssh.allow=always", "-c", "protocol.https.allow=always", "-c", "submodule.recurse=false", "-c", "commit.gpgSign=false", "-c", "push.gpgSign=false", "-c", "gc.auto=0", "-c", "fetch.writeCommitGraph=false"}
	cmd := exec.CommandContext(ctx, "git", append(options, args...)...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") && k != "GIT_SSH_COMMAND" && k != "GIT_SSH" && k != "GIT_ASKPASS" && k != "GIT_SSH_VARIANT" && k != "GIT_CONFIG_GLOBAL" && k != "GIT_CONFIG_NOSYSTEM" && k != "GIT_SSL_CAINFO" && k != "GIT_SSL_CAPATH" {
			continue
		}
		if k != "LC_ALL" {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "LC_ALL=C", "GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_AUTHOR_NAME=bw", "GIT_AUTHOR_EMAIL=bw@localhost", "GIT_COMMITTER_NAME=bw", "GIT_COMMITTER_EMAIL=bw@localhost")
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, e := cmd.Output()
	msg := redact(strings.TrimSpace(stderr.String()))
	if os.Getenv("BW_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "bw: git %s\n", redact(strings.Join(args, " ")))
		if msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
	}
	if e != nil {
		code := -1
		var x *exec.ExitError
		if errors.As(e, &x) {
			code = x.ExitCode()
		}
		if msg == "" {
			msg = e.Error()
		}
		return out, &gitError{code, msg}
	}
	return out, nil
}

func checkRemote(remote string) error {
	if remote == "" || strings.HasPrefix(remote, "-") || strings.Contains(remote, "::") {
		return fail(1, "Invalid origin remote. Use an ssh, https or file URL with git remote set-url origin <url>.")
	}
	if scheme, _, ok := strings.Cut(remote, "://"); ok {
		switch strings.ToLower(scheme) {
		case "ssh", "git+ssh", "ssh+git", "https", "file":
		default:
			return fail(1, "Origin uses %s://, which bw does not use. Switch origin to an ssh, https or file URL with git remote set-url origin <url>.", scheme)
		}
	}
	return nil
}

func openStore(root string) (*store, error) {
	return openStoreContext(context.Background(), root)
}

func openStoreContext(ctx context.Context, root string) (*store, error) {
	out, e := gitContext(ctx, root, nil, "remote", "get-url", "origin")
	if e != nil {
		return nil, fail(1, "No origin remote. Add one with git remote add origin <url>.")
	}
	remote := strings.TrimSpace(string(out))
	if e = checkRemote(remote); e != nil {
		return nil, e
	}
	if !strings.Contains(remote, ":") && !filepath.IsAbs(remote) {
		remote = filepath.Join(root, remote)
	}
	dir, e := os.MkdirTemp("", "bw-state-*")
	if e != nil {
		return nil, e
	}
	if _, e = gitContext(ctx, root, nil, "init", "--bare", "--quiet", "--template=", dir); e != nil {
		os.RemoveAll(dir)
		return nil, fail(1, "Cannot create temporary Git store. Check Git and your temporary directory.")
	}
	// Borrow the working repository's objects so fetching the default branch only
	// transfers what this clone lacks. The temporary store never writes there.
	if common, e := gitContext(ctx, root, nil, "rev-parse", "--path-format=absolute", "--git-common-dir"); e == nil {
		objects := filepath.Join(strings.TrimSpace(string(common)), "objects")
		if info, e := os.Stat(objects); e == nil && info.IsDir() {
			_ = os.MkdirAll(filepath.Join(dir, "objects", "info"), 0755)
			_ = os.WriteFile(filepath.Join(dir, "objects", "info", "alternates"), []byte(objects+"\n"), 0644)
		}
	}
	return &store{dir: dir, remote: remote, local: filepath.Join(root, catalogPath), ctx: ctx}, nil
}

type snapshot struct {
	state   State
	tip     string
	catalog Catalog
	source  string
}

// fetch takes the state ref and origin's default branch in one shallow fetch.
// A missing ref is reported by name; it is dropped and the fetch repeated.
func (s *store) fetch() (state, catalog bool, err error) {
	specs := map[string]string{stateRef: "+" + stateRef + ":" + localState, "HEAD": "+HEAD:" + localTasks}
	for len(specs) > 0 {
		args := []string{"fetch", "--quiet", "--no-tags", "--no-recurse-submodules", "--depth=1", "--", s.remote}
		for _, name := range []string{stateRef, "HEAD"} {
			if spec, ok := specs[name]; ok {
				args = append(args, spec)
			}
		}
		_, e := gitContext(s.ctx, s.dir, nil, args...)
		if e == nil {
			break
		}
		missing := ""
		for name := range specs {
			if strings.Contains(e.Error(), "couldn't find remote ref "+name) {
				missing = name
			}
		}
		if missing == "" {
			return false, false, fail(4, "Cannot fetch state from origin%s. Check Git access and retry; after a failed update, run bw show <task> before doing work.", detail(e))
		}
		delete(specs, missing)
	}
	_, state = specs[stateRef]
	_, catalog = specs["HEAD"]
	return state, catalog, nil
}

func (s *store) load() (snapshot, error) {
	snap := snapshot{state: State{Tasks: map[string]Mark{}, Files: map[string][]byte{}}}
	hasState, hasCatalog, e := s.fetch()
	if e != nil {
		return snap, e
	}
	if hasState {
		out, e := gitContext(s.ctx, s.dir, nil, "rev-parse", "--verify", localState+"^{commit}")
		if e != nil {
			return snap, fail(1, "State ref must point to a commit. Ask your repository maintainer.")
		}
		snap.tip = strings.TrimSpace(string(out))
		out, e = gitContext(s.ctx, s.dir, nil, "show", snap.tip+":state.json")
		if e != nil {
			return snap, fail(1, "State ref has no state.json. Ask your repository maintainer.")
		}
		snap.state = State{}
		if e = decode(out, &snap.state); e == nil {
			e = snap.state.validate()
		}
		if e != nil {
			return snap, fail(1, "Invalid state: %v. Ask your repository maintainer.", e)
		}
	}
	if hasState {
		data, err := gitContext(s.ctx, s.dir, nil, "archive", "--format=tar", snap.tip)
		if err != nil {
			return snap, fail(1, "Cannot read board state: %v", err)
		}
		snap.state.Files = map[string][]byte{}
		r := tar.NewReader(bytes.NewReader(data))
		for {
			h, err := r.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return snap, err
			}
			if h.Typeflag != tar.TypeReg || h.Name == "state.json" {
				continue
			}
			if h.Size > 4<<20 {
				return snap, fail(1, "Board file exceeds 4 MiB")
			}
			b, err := io.ReadAll(r)
			if err != nil {
				return snap, err
			}
			snap.state.Files[h.Name] = b
		}
	}
	// The shared catalog is the one on origin's default branch. Until it is
	// merged there, the local working-tree file is used.
	var data []byte
	snap.source = "local"
	if hasCatalog {
		if out, e := gitContext(s.ctx, s.dir, nil, "rev-parse", "--verify", localTasks+"^{commit}"); e == nil {
			commit := strings.TrimSpace(string(out))
			if out, e := gitContext(s.ctx, s.dir, nil, "show", commit+":"+catalogPath); e == nil {
				data, snap.source = out, "origin@"+commit[:12]
			}
		}
	}
	if data == nil {
		if data, e = os.ReadFile(s.local); e != nil {
			return snap, fail(1, "Cannot read %s (not on origin's default branch or locally). Run bw init, or check file permissions.", catalogPath)
		}
	}
	if snap.catalog, e = parseCatalog(data); e != nil {
		where := "local " + catalogPath
		if snap.source != "local" {
			where = catalogPath + " on origin's default branch"
		}
		return snap, fail(1, "Invalid %s: %v. Fix it, then run bw check.", where, e)
	}
	return snap, nil
}

func newOp() (string, error) {
	nonce := make([]byte, 16)
	if _, e := rand.Read(nonce); e != nil {
		return "", e
	}
	return hex.EncodeToString(nonce), nil
}

func (s *store) commit(state State, parent, op string) (string, error) {
	// Identify the board schema when the state tree contains board files.
	if len(state.Files) > 0 {
		state.BoardVersion = 1
	}
	state.Ops = append(append([]string{}, state.Ops...), op)
	if len(state.Ops) > maxOps {
		state.Ops = state.Ops[len(state.Ops)-maxOps:]
	}
	data, err := marshal(state)
	if err != nil {
		return "", err
	}
	if len(data) > 4<<20 {
		return "", fail(1, "State exceeds 4 MiB. Ask your repository maintainer before changing it.")
	}
	files := map[string][]byte{"state.json": data}
	for name, b := range state.Files {
		if name != "state.json" {
			if len(b) > 4<<20 {
				return "", fail(1, "Board file %s exceeds 4 MiB; shorten the input before retrying.", name)
			}
			files[name] = b
		}
	}
	tree, e := s.tree(files)
	if e != nil {
		return "", e
	}
	args := []string{"commit-tree", tree, "-m", "bw " + op}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	out, e := gitContext(s.ctx, s.dir, nil, args...)
	return strings.TrimSpace(string(out)), e
}

// change inspects a fresh snapshot and reports whether it modified the state.
type change func(snapshot) (result any, changed bool, err error)

// Each candidate is a child of the freshly fetched tip, so a normal push is a
// compare-and-swap. Never merge/rebase a candidate: re-evaluate the operation on
// the latest state instead. Every candidate records a unique op ID in the state,
// which proves publication when an accepted push's reply is lost.
func (s *store) update(init bool, apply change) (any, error) {
	snap, e := s.load()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if e != nil {
			return nil, e
		}
		if !init && snap.tip == "" {
			return nil, fail(1, "State is not initialized. Run bw init.")
		}
		result, changed, e := apply(snap)
		if e != nil || !changed {
			return result, e
		}
		reconcileSteps(snap.state, snap.catalog)
		op, e := newOp()
		if e != nil {
			return nil, e
		}
		candidate, e := s.commit(snap.state, snap.tip, op)
		if e != nil {
			var f *failure
			if errors.As(e, &f) {
				return nil, e
			}
			return nil, fail(1, "Cannot write Git objects. Check disk space and Git installation.")
		}
		_, pushErr := gitContext(s.ctx, s.dir, nil, "push", "--porcelain", "--no-verify", "--no-recurse-submodules", "--", s.remote, candidate+":"+stateRef)
		if pushErr == nil {
			return result, nil
		}
		next, e := s.load()
		if e != nil {
			return nil, e
		}
		if next.tip == candidate || next.state.hasOp(op) {
			return result, nil
		}
		if next.tip == snap.tip {
			return nil, fail(4, "Origin rejected the state update%s. Check push permissions or hooks, then run bw show <task> before retrying.", detail(pushErr))
		}
		backoff := 5 * time.Millisecond << min(attempt, 5)
		time.Sleep(backoff/2 + mrand.N(backoff))
		snap = next
	}
	return nil, fail(4, "State is still busy after %d concurrent update attempts; nothing was changed. Retry the command.", maxAttempts)
}

func repoRoot() (string, error) {
	out, e := git("", nil, "rev-parse", "--show-toplevel")
	if e != nil {
		return "", fail(1, "Not in a Git working tree. Change into your repository first.")
	}
	return strings.TrimSpace(string(out)), nil
}

// Build each directory deterministically; every file shares the same CAS commit.
func (s *store) tree(files map[string][]byte) (string, error) {
	entries := map[string]string{}
	dirs := map[string]map[string][]byte{}
	for name, data := range files {
		first, rest, nested := strings.Cut(name, "/")
		if nested {
			if dirs[first] == nil {
				dirs[first] = map[string][]byte{}
			}
			dirs[first][rest] = data
			continue
		}
		blob, e := gitContext(s.ctx, s.dir, data, "hash-object", "-w", "--stdin")
		if e != nil {
			return "", e
		}
		entries[first] = "100644 blob " + strings.TrimSpace(string(blob))
	}
	for name, children := range dirs {
		hash, e := s.tree(children)
		if e != nil {
			return "", e
		}
		entries[name] = "040000 tree " + hash
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var input strings.Builder
	for _, name := range names {
		input.WriteString(entries[name] + "\t" + name + "\x00")
	}
	out, e := gitContext(s.ctx, s.dir, []byte(input.String()), "mktree", "-z")
	return strings.TrimSpace(string(out)), e
}

package bw

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

func completeWords(args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()
	words := append([]string{}, commandOrder...)
	words = append(words, "--role", "--timeout", "--interval", "operating", "help", "setup", "planning", "format", "agents", "--json", "--as", "--effort", "--state", "--worker", "--task", "--undo", "--open", "--port", "--shell", "--help", "--all", "--next", "--force", "--reason", "--mine", "--self", "--branch", "--head", "--note", "--notes", "--model", "--sha", "--technical", "--check", "--markdown", "xs", "s", "m", "l", "xl", "low", "medium", "high")
	// Shells pass only words preceding the current prefix. Keep values contextual.
	prev := ""
	if len(args) > 0 {
		prev = args[len(args)-1]
	}
	switch prev {
	case "--effort":
		return "low\nmedium\nhigh"
	case "--state":
		return strings.Join(taskStates, "\n")
	case "--shell":
		return "bash\nzsh\nfish"
	}
	if len(args) > 0 && args[0] == "guide" {
		return "planning\nagents\noperating"
	}
	if len(args) > 0 && args[0] == "completion" {
		return "bash\nzsh\nfish\ninstall\nuninstall"
	}
	if len(args) > 0 && args[0] == "review" && len(args) == 1 {
		return "list\nshow\nrequest\ntake\nrelease\napprove\nchanges\nmerged"
	}
	rootData, e := gitContext(ctx, "", nil, "rev-parse", "--show-toplevel")
	if e != nil {
		return strings.Join(words, "\n")
	}
	root := strings.TrimSpace(string(rootData))
	if b, e := os.ReadFile(filepath.Join(root, catalogPath)); e == nil {
		if c, e := parseCatalog(b); e == nil {
			words = append(words, c.Roles...)
			for _, t := range c.Tasks {
				words = append(words, t.ID)
			}
		}
	}
	// These refs are read only. Completion never opens a store or fetches.
	for _, ref := range []string{localState, stateRef} {
		b, e := gitContext(ctx, root, nil, "ls-tree", "-r", "--name-only", ref)
		if e != nil {
			continue
		}
		for _, p := range strings.Fields(string(b)) {
			if strings.HasPrefix(p, "reviews/") && strings.HasSuffix(p, "/events.jsonl") {
				words = append(words, strings.TrimSuffix(strings.TrimPrefix(p, "reviews/"), "/events.jsonl"))
			}
		}
		b, e = gitContext(ctx, root, nil, "show", ref+":questions.jsonl")
		if e == nil {
			if qs, e := events(b); e == nil {
				for _, q := range qs {
					words = append(words, strconv.Itoa(q.ID))
				}
			}
		}
		break
	}
	sort.Strings(words)
	words = slices.Compact(words)
	return strings.Join(words, "\n")
}
func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return `# bw managed completion
_bw_complete() { local IFS=$'\n'; COMPREPLY=( $(compgen -W "$(bw _complete "${COMP_WORDS[@]:1:COMP_CWORD-1}")" -- "${COMP_WORDS[COMP_CWORD]}") ); }
complete -F _bw_complete bw
`, nil
	case "zsh":
		return `#compdef bw
# bw managed completion
_bw_complete() { local -a suggestions; suggestions=("${(@f)$(bw _complete "${words[@]:1:$CURRENT-2}")}"); compadd -- "${suggestions[@]}"; }
compdef _bw_complete bw
`, nil
	case "fish":
		return `# bw managed completion
complete -c bw -f -a '(bw _complete (commandline -opc)[2..-1])'
`, nil
	}
	return "", fail(1, "Choose bash, zsh or fish")
}
func completion(o options) (outcome, error) {
	if o.help {
		return outcome{result: helpText["completion"]}, nil
	}
	if len(o.args) != 1 {
		return outcome{}, fail(1, "Run bw completion bash|zsh|fish or install|uninstall")
	}
	action := o.args[0]
	if action != "install" && action != "uninstall" {
		script, e := completionScript(action)
		return outcome{result: script}, e
	}
	shell := o.shell
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	script, e := completionScript(shell)
	if e != nil {
		return outcome{}, e
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return outcome{}, e
	}
	var target, rc, line string
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	switch shell {
	case "fish":
		target = filepath.Join(config, "fish/completions/bw.fish")
	case "bash":
		target = filepath.Join(home, ".local/share/bash-completion/completions/bw")
		rc = filepath.Join(home, ".bashrc")
		line = "source '" + strings.ReplaceAll(target, "'", "'\\''") + "' # bw completion\n"
	case "zsh":
		target = filepath.Join(config, "bw/completion.zsh")
		zd := os.Getenv("ZDOTDIR")
		if zd == "" {
			zd = home
		}
		rc = filepath.Join(zd, ".zshrc")
		line = "autoload -Uz compinit; compinit; source '" + strings.ReplaceAll(target, "'", "'\\''") + "' # bw completion\n"
	}
	changes := []string{}
	old, readErr := os.ReadFile(target)
	if readErr != nil && !os.IsNotExist(readErr) {
		return outcome{}, readErr
	}
	if len(old) > 0 && !strings.Contains(string(old), "# bw managed completion") {
		return outcome{}, fail(1, "Refusing to overwrite unmanaged completion %s", target)
	}
	if action == "install" {
		if string(old) != script {
			if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
				return outcome{}, e
			}
			if e = os.WriteFile(target, []byte(script), 0644); e != nil {
				return outcome{}, e
			}
			changes = append(changes, "Wrote "+target)
		}
	} else if readErr == nil {
		if e = os.Remove(target); e != nil {
			return outcome{}, e
		}
		changes = append(changes, "Removed "+target)
	}
	if rc != "" {
		b, e := os.ReadFile(rc)
		if e != nil && !os.IsNotExist(e) {
			return outcome{}, e
		}
		text := string(b)
		next := text
		if action == "install" {
			if !strings.Contains(text, line) {
				if next != "" && !strings.HasSuffix(next, "\n") {
					next += "\n"
				}
				next += line
			}
		} else {
			next = strings.ReplaceAll(text, line, "")
		}
		if next != text {
			if e = os.WriteFile(rc, []byte(next), 0644); e != nil {
				return outcome{}, e
			}
			changes = append(changes, "Updated "+rc+": "+strings.TrimSpace(line))
		}
	}
	if len(changes) == 0 {
		changes = append(changes, "Nothing changed")
	}
	changes = append(changes, fmt.Sprintf("Undo: bw completion %s --shell %s", map[bool]string{true: "uninstall", false: "install"}[action == "install"], shell))
	return outcome{result: strings.Join(changes, "\n")}, nil
}

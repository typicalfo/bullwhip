# Bullwhip implementation specification

`bw` is a small, deterministic CLI for repository-local tasks, exclusive
claims, dependency unlocking, and a shared review, question and note board.
The generated README and command help document the complete command interface.
`bw help` shows usage; `bw help-2` provides the longer overview.

## Repository and identity

Run from any subdirectory of a Git working tree. The project is that
repository; the remote is `origin`. Tasks come from `.bullwhip/tasks.json`
on origin's default branch, falling back to the local file until shared.
`bw check` validates the local task file.

Worker identity comes from `--as`, then `BW_AS`, then Git's `user.name`.
JSON callers performing identity-dependent operations must supply `--as`
or `BW_AS`; each concurrent worker needs a distinct identity.

## Tasks and claims

Tasks have a stable `id`, `title`, optional `ref`, `needs` and `priority`.
Validate unique IDs, known dependencies and acyclic dependencies. Ready tasks
sort by descending priority, then ID. A task is waiting, ready, claimed or
done. Only done marks for catalog tasks satisfy prerequisites.
`bw queue` lists only free, ready tasks in priority/ID order. `bw queue --all` includes
all catalog task states in the same priority/ID order. `bw q` aliases `bw queue`,
including its flags and help. Use `bw tasks` for the grouped task board and blockers.

## Task views and idle workers

`bw tasks [--state S]...` groups tasks as claimed, ready, waiting and done,
retaining priority/ID order within each group. Repeat `--state` to select
any combination without duplicate rows. Waiting rows include each unfinished
need's state, holder, inactivity and stale flag. Claimed rows show their
holder and activity; done rows show the completing worker and completion
time recovered from the existing state-ref history. Task-file format help
is `bw help format`; `bw help tasks` describes the task-view command.
Reopening a prerequisite displays its unfinished descendants as waiting even
when they retain a claim. Holders remain visible; the read never revokes
ownership. Existing done marks still display as done.

`bw summary` reports all four counts, every active claim (including removed
tasks), pending reviews classified as waiting or held, and unanswered
questions with a count requiring a coordinator. Plain text keeps review and
question details as counts; JSON includes the individual records. It ends with task progress
and whether all catalog tasks are done. Approved/changes/merged reviews are
excluded from the pending review counts.

`bw idle --as NAME` decides in order: work if the caller holds a claim
(including a removed task); claim if free ready work exists; wait if waiting
catalog tasks outnumber catalog tasks claimed by other workers; stop
otherwise. Work/claim/wait exit 0; stop exits 3 with populated JSON data
(`action`, optional `task`, `waiting`, `claimed`, `reason`). This heuristic
reserves no capacity: simultaneous idle workers may all receive wait.

All four new commands read state only. `tasks`, `summary` and `stats` need
no identity; `idle` uses the normal identity rules. JSON retains the common
envelope and catalog source. `questions --all` remains available.

## Run statistics

`bw stats [--worker NAME] [--task ID]` replays existing state-ref commits and
board events for task IDs in the current catalog (origin's default branch,
with the normal local fallback). It never adds events or changes state.
The run begins at its first claim; it ends at the last completion when all
catalog tasks are done, otherwise elapsed time goes to now. No claims means
not started, with zero durations. An empty catalog is complete but unstarted.

State transitions are read from complete linear Git history pinned to the
same snapshot as the board. Claims use their recorded `since`; completion,
release and reopen use commit timestamps, with second precision. Forced
releases use the timestamp, actor and reason of the note written in the same
commit. Unannotated release actors are unknown; the released holder is known. Heartbeats and safe retries do not create claim periods. Missing,
invalid or merged history fails clearly; unavailable history is never zeroed.
Clock regressions clamp at the preceding transition to avoid negative times.

Replay the current catalog's dependencies from run start, accumulating ready,
waiting and held wall times. Time before the first claim is outside the run.
Holders are shown in order, with their claim/end times and outcomes; total
claim-to-done spans from first claim through the latest completion, including
release gaps. Reopening clears the current completion and resumes the run.
Worker completion counts are successful done transitions, so reopening and
finishing again can count twice. Run done count is the current done-task count.
A worker's held time sums claim durations, including overlapping claims; it
is elapsed claim time, not effort. A retained claim blocked by a reopened
prerequisite accrues both held time and dependency waiting time.

Worker rows also include current claims, releases (both as holder and releaser,
with forced reasons), verdicts given/received, questions asked and notes written.
Include board events for current task IDs between run start and end (completion
second inclusive); report notes after a completed run in the board, not its stats.
Review rounds count explicit requests, not automatic requeue events; preserve
verdicts and self-approval. Filters intersect task/worker detail rows, recompute
worker counters for a selected task, and retain global run totals. Unknown
workers yield empty detail; unknown task IDs fail with usage guidance.


## Claim ownership

The work loop is queue, claim, work, done. `crack` aliases `claim --next`.
Claim retries return an existing held task. Done requires the caller's
claim. Release requires stopping work first. Another worker's claim needs
`--force --reason`, recorded as a note, and is allowed only when the holder
has made no bw write for `stale_hours` (default 4), or when the caller is one
of the task file's `coordinators`. Every write by a holder (claim, heartbeat,
note, question, review action) refreshes their claims' `seen` time; agents
run `bw heartbeat` at least hourly during long stretches of work. Claims
never expire on their own. Identity is self-asserted (`--as`, `BW_AS`, Git
`user.name`), so coordinators and staleness are guardrails for cooperating
workers, not access control; the recorded reason is the audit trail. State
and catalog decoding rejects unknown fields, so a schema change needs every
worker on the new binary at once.
Reopen clears a done mark without changing descendant marks.

Removed catalog tasks retain claims until explicitly released. Local ref
fragments resolve to Markdown sections within the repository; missing
sections warn without preventing a claim.

## Reviews, questions and notes

Reviews are requested for the caller's claimed task. IDs derive from branch
names by replacing `/` with `--`. Branch and head default to the current
branch and HEAD; commits must resolve locally. Reviews record requests,
exclusive takes, releases, verdicts and merges as append-only events.

Automatic take chooses the oldest unheld request by another author, or
returns the caller's held pending review on retry. Ownership never expires.
Re-requests retain the reviewer and invalidate the previous verdict.
Mid-review releases re-queue the request; releases after changes wait for a
new request. A coordinated forced release can free another reviewer.

Self-review requires an explicit ID, `--self`, and `self_review_after`
(default 30m). Self-verdicts are labeled. Changes require findings; verdicts
may record a model name. Only the author records a merge after approval,
and the merge SHA must be an ancestor of origin's default branch. A merge
does not mark the task done. Project policy decides reviewers and checks.

Questions have stable increasing IDs and append-only answers. When the task
file lists coordinators, only they may answer a question unless it was asked
with `--technical`; this keeps scope and product decisions with them. Notes carry
a task context label. Identical writes by the same actor are safe to retry.
The board is native to `bw`; projects start with an empty board.

## Holds

`bw holds [<name>] [--check] [--json] [--as <name>]` reads state without
writing. An explicit name takes precedence over caller identity. Unknown
workers hold nothing. Without a name, the normal identity rules apply.

Report all claimed tasks held by the worker, including removed catalog
tasks, with title, ownership timestamp, age and stale flag. Report pending
reviews currently held by the worker, including self-reviews: taken
or requested while held. Verdicts, release and merge exclude a
review. Authored but unheld reviews, questions and notes do not count.
Use one pending-review predicate shared with take's retry paths.

Text output includes release commands. JSON includes worker, claims and
reviews arrays, even when empty. Without `--check`, successful reads exit 0.
With `--check`, holdings produce exit 2, `ok: false`, populated data and an
error explaining release commands; empty holdings exit 0.

## Storage and reliability

Claims, done marks and board files live on origin's `refs/bullwhip/state`.
Use normal compare-and-swap Git pushes. On contention, fetch and recompute;
never merge or rebase stale candidates. Record bounded operation IDs to
recognize accepted pushes whose acknowledgement was lost. Preserve all
unrelated state files. Board data uses `board_version: 1` in `state.json`.

Git write access can bypass this cooperative tool. Preserve SSH/TLS and
credential handling. Never store secrets, prompts, customer data, personal
identifiers or command output. No background server, daemon, launcher, project registry,
leases, verification ledger or shared-resource locks.

## Interface and qualification

`bw version` prints the full build commit from Go build metadata and marks
uncommitted source builds as `(modified)`. It runs without Git or repository
access; builds without VCS metadata report `unknown` explicitly.

Human output wraps to the terminal width, defaulting to 100 columns for pipes
and files. Tables wrap individual cells and fall back to labeled records when
columns cannot fit. Fields are never truncated; terminal controls are stripped
from human text. JSON preserves complete original values and is the agent interface.
Markdown help and the interactive monitor retain their own layouts.

Every command supports `--help` without repository access and `--json` with
one envelope. Exit codes: 0 success, 1 usage/setup, 2 conflict, 3 no ready
task or waiting review, 4 remote failure/busy. Errors explain the next action.
Keep help in `internal/bw/help.go`; regenerate README with `scripts/docs.sh`.
Agent help teaches claim ownership, review policy, safe release, and running
`bw holds --check --json` before stopping.

Use real competing processes and disposable Git remotes in tests. Run
`scripts/acceptance.sh`: standard and race tests, vet, govulncheck, and the
two-worker demo. Record results and platform limits in `docs/acceptance.md`.
Do not activate real queues or workers as part of CLI qualification.

## Human monitor

`bw tui` is a foreground Charm v2 terminal monitor. It polls a
coherent state/catalog snapshot after a five-second delay between completed
refreshes; only one request runs at once. It shows task progress, holders,
dependency blockers, stale claims, unanswered questions, pending reviews and
notes. Stale means no recorded activity past the catalog threshold, not a
confirmed stalled process. Claims never expire. All-done leaves the monitor open.

Filtering, list/detail focus and narrow-terminal drill-down are local UI state.
Refresh errors retain the last good data with a visible failure label and retry;
initial errors also retry. Quit cancels in-flight reads. Browsing never writes state. In Attention,
Left/Right or 1/2/3 navigate sections; Enter shows question answers and review
findings/history, and h includes answered questions and merged reviews. Press a
on a question to open a draft, Esc to cancel, and Ctrl+S to submit through the
existing answer operation. The editor shows the resolved identity and preserves
drafts on failure. Submission waits for a result and cannot be submitted twice
concurrently. Coordinator authorization and safe retries remain the CLI's rules.
No worker execution or automatic recovery actions occur. `bw tui --json` returns a single
snapshot in the standard envelope without starting terminal UI. It deliberately
does not fetch completion history on every poll; `bw tasks` and `bw stats` provide
historical timings. The interactive mode requires BW_DEBUG to be unset.

## Task metadata, forge links and dashboard

The optional task fields are `size` (`xs`, `s`, `m`, `l`, `xl`), `effort`
and `review_effort` (`low`, `medium`, `high`), and `steps` (1–32 strings
of at most 120 Unicode characters or objects with `id` and `text`). Step
IDs follow task ID rules. Keys (ID, otherwise exact text) are unique within
a task. Review effort defaults to task effort. Missing effort has no limit.
Worker effort comes from `--effort`, then `BW_EFFORT`; omission is unlimited.
Automatic claims and review takes skip work above this level; explicit IDs
ignore the limit. Queue filters with the same rule. Idle stops, naming the
minimum required effort, when ready work exists but none matches.

`bw step <task> <n> [--undo]` records a keyed tick for the current catalog's
1-based step, only for the holder of an unfinished claim. It refreshes all
that holder's claim heartbeats. `step_ticks` (key to UTC timestamp) and
`step_catalog` (the checklist at the last tick/undo) are optional mark fields
in `state.json`. Reordering and ID-based rewording preserve ticks; removed
keys are discarded on subsequent state writes. Plain-text rewording creates
an unticked key. Views report progress and changes since the last tick;
plain-string replacements at the same position are labeled reopened.
Done succeeds with unticked steps listed in text warnings and JSON.
Statistics group held wall time by size and report intervals from the claim
or previous surviving tick to each surviving tick. These are wall time,
not measured effort; undo removes the timestamp. Review wait is request to
verdict, or now while pending. Worker working/idle time starts at the first claim and uses the union of
held intervals. Idle means no held tasks; stale is held time past the catalog
activity threshold, replaying historical heartbeat timestamps.

Optional catalog `repo_url` (HTTP(S) web URL) overrides the origin-derived
web base. SSH and HTTPS origins are converted to a credential-free web base.
`forge`, if set, must be `github`; GitHub is the default and the only
supported link format. JSON adds `links` for repository-path task refs, review
branches, source-branch pull request searches, heads and merged commits. File
links use origin's locally known default branch, falling back to `HEAD`.
Human CLI output does not print these URLs.

`bw web` / `bw dashboard` is a foreground localhost dashboard, bound only
to IPv4 127.0.0.1, with `--port` (0/omitted chooses a port) and `--open`.
It prints the URL, refreshes coherent catalog/state/history snapshots every
five seconds and streams them with SSE. It embeds pinned Tabler dist assets
and their MIT licence: no CDN or Node build is required. Board, progress,
time, workers and questions/notes views use the CLI read models. Answers
call the existing answer operation with the process identity, including its
coordinator checks and compare-and-swap retry rules. Cross-origin requests
are rejected; no accounts or tokens are introduced. Ctrl-C cancels polling,
closes streams and removes temporary stores. `--json` returns one snapshot.
The deferred game view is outside this implementation.

`bw completion bash|zsh|fish` prints scripts. `install` and `uninstall`
accept `--shell`, defaulting to `$SHELL`, print changed paths and the reverse
command, and preserve unmanaged files. Bash installs under
`~/.local/share/bash-completion/completions/bw` with a marked `.bashrc`
source line. Zsh uses `$XDG_CONFIG_HOME/bw/completion.zsh` and a marked
`$ZDOTDIR/.zshrc` line (home when unset). Fish uses
`$XDG_CONFIG_HOME/fish/completions/bw.fish`. Config defaults to `~/.config`.
The internal `_complete` protocol reads the local catalog and locally
available state refs only, with a 180ms context deadline for Git reads;
it never fetches or changes any repository. Static words work anywhere.

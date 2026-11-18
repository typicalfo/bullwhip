# Proposal: task metadata, forge links, shell completion and a web dashboard

Direction from the project owner, for the bw coding agent to build. Everything below is optional for task authors: a catalog without the new fields behaves exactly as today. bw has no backward-compatibility rule, so all workers upgrade together when a catalog starts using a new field (strict decoding rejects unknown fields).

Order of work: 0 first and throughout, then 1 and 2, then 3, then 4 and 5. The game view (6) is deferred.

## 0. Working safely while workers use bw

Other agents are using the installed `bw` on live repositories right now, coordinated through those repositories' `refs/bullwhip/state`. A development build that writes a field older binaries don't know (a step tick, a new task field) into a live state ref makes every other worker's `bw` fail.

- Work only in your own clone of the bullwhip repository, on a branch. Never edit, build or switch branches in a checkout someone else uses.
- Run your build from the clone (`go run ./cmd/bw` or `go build -o ./.work/bw ./cmd/bw`). Never `go install` it or copy it onto `PATH`; the owner installs after merge.
- Test only against throwaway projects: a temporary directory with its own local bare repository as `origin` (`git init --bare`, then `bw init`). Never point a development build at a real project's remote or its state ref, not even read-only commands, since some commands write heartbeats.
- `scripts/acceptance.sh` and the Go tests must create and delete their own temporary repositories, as they do today.
- Delete your temporary repositories and build outputs when you finish.
- When the work merges, say in the MR description which new fields reach the state ref and the task file, so the owner knows every worker must upgrade before a catalog uses them.

## 1. Task fields

| Field | Type | Meaning |
| --- | --- | --- |
| `size` | `xs`, `s`, `m`, `l`, `xl` | How much work the task is. Shown in `queue`, `tasks`, `show`, the TUI and the dashboard. `bw stats` reports wall time by size, so estimates can be compared with actuals |
| `effort` | `low`, `medium`, `high` | The reasoning level an agent needs. Different from `size`: a small task can need high effort |
| `review_effort` | `low`, `medium`, `high` | The level a reviewer needs. When absent, the task's `effort` applies |
| `steps` | array of 1–32 entries, each a string (at most 120 characters) or `{"id": "...", "text": "..."}` | A checklist inside the task. Not separately claimable |

### `effort` matching

- A worker declares its level with `--effort <level>` or `BW_EFFORT`, like `--as`. Undeclared means no limit.
- `bw crack` skips tasks whose `effort` is above the worker's level; among the rest it picks by priority as now. `bw claim <task>` ignores `effort` (choosing a task explicitly is a human decision).
- `bw idle`: if the only ready tasks are above the worker's level, the action is `stop` with a reason naming the level needed, so an agent never waits for work it will not take.
- `queue` and `tasks` show `effort`; `bw queue --effort <level>` filters.
- `bw review take` without an ID skips reviews whose `review_effort` (or `effort`) is above the reviewer's declared level; taking a review by ID ignores it. `review list` shows the level.

### `steps` progress

- `bw step <task> <n>` ticks step `n` (1-based, in the current catalog order); `--undo` unticks. Only the holder can tick. Ticks live in the state ref with the claim and count as a heartbeat.
- A tick is stored against the step's key, not its position: the `id` when given, otherwise the step's text. Step IDs follow task ID rules and are unique within the task.
- `show`, `tasks`, the TUI and the dashboard show progress (`3/5`).
- `bw done` with unticked steps succeeds but warns and lists them (`--json` returns them). Steps are progress, not a gate.
- Editing a task's `steps` keeps completed work:
  - adding or reordering steps keeps every existing tick (progress goes from `3/5` to `3/6`, never down for work already done);
  - removing a step drops only its tick;
  - changing a plain-string step's text makes it a new, unticked step, because the change means it needs more work; to reword a step without reopening it, give it an `id` and keep the `id`;
  - `show` lists which steps were added, removed or reopened since the last tick.
- `bw stats` reports time per step where ticks exist.

## 2. Forge links

- Catalog fields `repo_url` (web base URL) and `forge`, both optional. When `repo_url` is absent, bw derives it from `origin`: `git@host:group/repo.git` and `https://host/group/repo.git` become `https://host/group/repo`. `forge`, if set, must be `github`; GitHub is the default and the only supported link format.
- Links bw can build:
  - task `ref` that is a repository path: the file (and heading anchor) on the default branch;
  - review branch: the branch view, and GitHub pull request search for that source branch (bw does not know PR numbers);
  - review head and merged SHA: the commit view.
- `--json` outputs gain a `links` object wherever these apply. Human output stays unchanged.

## 3. `bw web`: a local web dashboard

- `bw web` (alias `bw dashboard`) starts the dashboard and prints its URL. Listens on `127.0.0.1` only (`--port`, default chosen and printed; `--open` opens the browser). No accounts, tokens or other web security: it is localhost-only, and that can be revisited later if needed.
- Runs as one identity, from `--as` or `BW_AS` like every other command (normally a coordinator). Every write from the page is recorded as that identity, exactly as if run from the CLI.
- Data: the same JSON the `--json` commands produce, refreshed by fetching the state ref every few seconds and pushed to the page with server-sent events.
- Look and feel: build on [Tabler](https://tabler.io) ([source](https://github.com/tabler/tabler)), a free, MIT-licensed admin dashboard template. Vendor its prebuilt CSS and JavaScript from a pinned release (the `dist` files of its npm package), keep its licence file beside them, and embed everything with `go:embed`. No Node build step at bw build time and no CDN at run time. Any chart library is vendored the same way with its licence.
- Views:
  - board: tasks by state with size, effort, step progress, holder, idle time and blockers;
  - progress: burn-down of the current catalog and throughput over time;
  - time: wall time per task by size (estimate against actual), time waiting on review, changes-requested rate;
  - workers: working, idle and stale time per worker;
  - questions and notes, with an answer box on each unanswered question. Answering calls the same code as `bw answer` and shows the same errors (for example, a non-coordinator answering a question that needs a coordinator).
- Every task, review, branch and commit links to the forge (section 2).
- Stopping `bw web` (Ctrl-C) leaves nothing running and nothing on disk.

## 4. Shell completion

- `bw completion bash|zsh|fish` prints a completion script for that shell.
- `bw completion install [--shell bash|zsh|fish]` sets it up for the current user: it detects the shell from `$SHELL` when `--shell` is absent, writes the script to that shell's standard per-user completion location (or adds one sourcing line to the shell's rc file when the shell has none), is safe to run again (never adds a second line), and prints exactly what it changed and how to undo it. `bw completion uninstall` reverses it.
- Completes commands, subcommands, flags, help topics and fixed values (`--state`, `--effort`, sizes). Inside a repository it also completes task IDs, review IDs and question numbers.
- Dynamic completion reads only the local catalog file and the locally fetched state ref: it never contacts the remote, never writes, and returns within 200 ms. Outside a repository it completes only static words.
- `bw help setup` and the README describe installing completion.

## 5. Help and docs

- `bw help`, `bw help agents` and the README cover every new command, flag and field, including `bw web`, `bw completion` and `bw step`.
- The `bw help agents --markdown` block tells agents to tick steps as they finish them and to declare `--effort` when their operator gives them a level.

## 6. Deferred: game view

A view on the same dashboard: workers as characters, claimed tasks as monsters sized by `size` with HP from `steps`, reviews as attacks (changes requested heals the monster), merged tasks on a slain pile, waiting tasks behind gates, idle workers resting, questions as speech bubbles. Waiting on the owner's choice of asset pack and tooling.

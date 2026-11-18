# Proposal: waiting, roles, resource guards and a planning guide

Direction from the project owner, for the bw coding agent to build. It comes from running several concurrent agents on real plans: agents burned tokens polling while blocked, plans with long chains or one large final task left most agents idle, and labs started by agents filled a small machine's disk and memory. Everything below is optional for task authors and workers: without the new fields and flags, bw behaves as today. Until bw's first public release there is no backward-compatibility rule: it keeps iteration fast and the code small, and all workers upgrade together when a catalog starts using a new field. After release, changes will keep older catalogs and state readable; don't build for that yet.

Order of work: 0 throughout; then 1, 2, 3 and 4; then 5 and 6; then 7, 8 and 9; the README (10) last, so it describes everything that shipped.

## 0. Working safely while workers use bw

Other agents use the installed `bw` on live repositories while you work.

- Work only in your own clone of the bullwhip repository, on a branch.
- Run your build from the clone (`go run ./cmd/bw` or `go build -o ./.work/bw ./cmd/bw`). Never `go install` it or put it on `PATH`; the owner installs after merge.
- Test only against throwaway projects with a local bare repository as `origin`. Never point a development build at a real project's remote or state ref, not even for read-only commands.
- Delete temporary repositories and build outputs when you finish.
- In the MR description, list every new field that reaches the task file or the state ref.

## 1. `bw wait`: blocking without tokens

A blocked agent that polls spends tokens on every check. A shell command that sleeps spends none, so bw does the waiting.

- `bw wait --as <name> [--timeout 9m] [--json]` blocks until the worker's `bw idle` action is something other than `wait`, then prints that action exactly as `bw idle` would. It also returns, with action `wait`, when the timeout passes. Default timeout 9 minutes, maximum 60, so it fits under the common 10-minute limit agent tools put on one shell command.
- While waiting it fetches the state ref every 30 seconds (`--interval`, minimum 10 seconds) and writes nothing. It does not heartbeat, because a waiting worker holds no claim.
- Exit codes match `bw idle`. Ctrl-C or a signal exits cleanly.
- `bw help agents` tells agents: when `idle` says `wait`, run `bw wait` and act on what it returns; never poll in a loop of your own.

## 2. Idle agents review

- `bw idle` gains a `review` action, checked after `work` and `claim` and before `wait`: an open review is waiting, the worker is not its author, and the reviewer level and roles (sections 5 and 6) allow it. The JSON names the review ID.
- `bw wait` returns on `review` too.
- `bw help agents` explains that `review` means: take that review with `bw review take <id>`, follow the project's review process, then run `bw idle` again.
- A catalog can turn this off with `"idle_review": false`.

## 3. Plan shape: `bw shape`

- A new command, `bw shape`, reports the shape of the tasks not yet done: the longest chain of dependent tasks (its length and task IDs), the most tasks ready at the same time if every task took equal time, the tasks that wait on four or more others, and a suggested crew size (that most-ready-at-once number). `--json` returns the same.
- It warns when the longest chain is more than half the remaining tasks, or when one task waits on most of the others, and points to `bw guide planning` (section 7).
- `bw check` does not change: it stays quick and only validates the catalog.
- `bw tasks` and `bw summary` show how many workers are useful right now: ready tasks plus claimed tasks.

### `bw todo`: what's left

- `bw todo` lists every task not yet done, one line each, in the order work can happen (tasks before the tasks that need them, then by priority): state (claimed, ready or waiting), ID, title, holder, size, step progress and, for waiting tasks, what it waits on. A header line gives the counts and the plan `description`.
- `--json` returns the same. `--mine` limits it to tasks the caller holds or could take (roles and effort, sections 5 and the previous proposal).
- It differs from `bw queue` (only free ready tasks) and `bw tasks` (every task, done ones included, grouped by state). It reads local state after one fetch and is as quick as `bw queue`.

## 4. Plan description

- Optional catalog field `description`: one or two sentences, at most 200 characters, saying what this plan is for.
- The TUI and `bw web` show it under the project name; `bw summary` prints it first; `--json` outputs that describe the board include it.
- `bw guide planning` tells planners to write it and to update it when the catalog moves to a new plan.

## 5. Roles

- Task field `roles`: a list of role names, for example `["frontend"]`. A task without `roles` is open to every worker.
- A worker declares its roles with `--role frontend,backend` or `BW_ROLE`, like `--as`. A worker with no declared roles is a generalist and may take any task.
- `bw queue`, `bw crack` and `bw idle` show and assign a worker only tasks open to it: no `roles`, or at least one role in common. `bw claim <task>` ignores roles, since choosing a task explicitly is a human decision. `bw queue --all` and `bw tasks` still show everything, with roles.
- `bw idle` says `stop`, with a reason naming the roles needed, when the only remaining work is for other roles and none of it can unblock work for this worker's roles. It says `wait` when blocked work for its roles remains.
- Optional catalog field `roles` lists the allowed names; when present, `bw check` rejects unknown role names on tasks. Role names follow task ID rules.
- Reviews: an optional task field `review_roles` limits who reviews; when absent, any worker may review. `bw review take` without an ID honours it; taking by ID ignores it.
- This combines with `effort` from the previous proposal: a task is open to a worker only when both match.

## 6. Resource guards

Agents often start heavy local stacks for their tasks. Before a worker takes new work, bw checks that the machine has room.

- Catalog field `resources`, all optional, with these defaults:

  | Field | Default | Meaning |
  | --- | --- | --- |
  | `min_free_disk_gb` | 10 | Free space on the filesystem holding the repository |
  | `min_free_memory_gb` | 2 | Available memory (macOS: free + inactive + purgeable; Linux: `MemAvailable`) |
  | `critical_free_disk_gb` | 3 | Below this, workers stop (see below) |
  | `critical_free_memory_gb` | 0.5 | Below this, workers stop |
  | `max_claims` | none | Most tasks claimed at once across all workers |

- Each person can override them on their own machine with `BW_MIN_FREE_DISK_GB`, `BW_MIN_FREE_MEMORY_GB`, `BW_CRITICAL_FREE_DISK_GB`, `BW_CRITICAL_FREE_MEMORY_GB` and `BW_MAX_CLAIMS`, or turn the guard off with `BW_RESOURCE_GUARD=off`. The environment wins over the catalog, because the catalog is shared and machines differ.
- `bw crack` and `bw claim` refuse when a limit is not met, with exit code 5 and a message naming the limit, the current value and the variable that changes it. Work already claimed is never affected.
- `bw idle` returns `wait` with a `low_resources` reason instead of `claim` when a minimum is not met; `bw wait` returns once there is room again.
- At a critical level, `bw idle` and `bw wait` return `stop` with a `critical_resources` reason, even for a worker holding a claim. `bw help agents` says what that means: commit and push your work, tear down your local stacks, release your claim with a note saying where the work stands, clean up, and stop. `bw check` rejects critical values above the minimums.
- `bw doctor` prints the minimums and critical levels in force, where each came from, and the current values. `--json` returns the same.
- Reading free disk and memory must work on macOS and Linux without extra tools or elevated rights. On any other system the guard is off and `bw doctor` says so.

## 7. `bw guide`: how to plan and work well with bw

Help says what each command does. The guide says how to use bw well, drawing on experience. It is written for an agent that has never used bw.

- `bw guide` lists the topics. `bw guide planning`, `bw guide agents` and `bw guide operating` print each one; `--markdown` prints it for a repository's docs.
- `planning`, for an agent turning a plan into a catalog:
  - Keep chains short and the plan wide. Give the first wave several independent tasks. Check the shape with `bw shape` before handing the plan to workers.
  - Avoid one large task at the end that waits on everything, such as a final runbook, an event catalogue or an integration pass. Split it per area, or let each area's task do its own part, with a short final pass.
  - End each plan with a review-and-fix task by a different worker, at a higher `effort` where available. Tag the result once it is done.
  - Serialize tasks that edit the same files with `needs`, so two agents never fight over one file. Keep each task to one area.
  - Use `size`, `effort`, `roles` and `steps`: size for planning and stats, effort and roles for who takes it, steps so progress is visible.
  - Point each task's `ref` at a fixed brief with a done bar that can be checked. Agents record their gap decisions as addenda instead of editing briefs.
  - Size the crew to the plan: start as many workers as the most tasks ready at once, not more.
  - Set `resources` limits that fit the machines the plan will run on.
  - Write a short `description` for each plan.
  - Run `bw shape` before handing the plan to workers.
- `agents`, for a worker:
  - Claim before work; heartbeat while working; release or finish before stopping.
  - When blocked, run `bw wait`, never a polling loop; act on what it returns.
  - When `idle` says `review`, review.
  - When `idle` says `stop` for critical resources, push, tear down, release with a note and stop.
  - Clean up local stacks and scratch files as each task ends, not at the end of the session; heavy stacks left running are what fill disks.
  - Declare your `--effort` and `--role` when your operator gives them.
- `operating`, for the person running workers:
  - Start workers with one instruction that names the project's agent document and says whether to do one task or work continuously.
  - Watch the board with the TUI or `bw web`; answer coordinator questions there.
  - `bw todo` is the quick answer to what's left.
  - Upgrade every worker's `bw` before a catalog uses new fields; `bw version` shows each build.
  - Use `bw doctor` before starting a crew on a small machine.
- `bw help` points to `bw guide`, and `bw help agents --markdown` includes a one-line pointer to `bw guide agents`.

## 8. TUI and `bw web`

Show everything this proposal adds in both the TUI and the `bw web` dashboard, from the same data the `--json` commands return:

- the plan `description` under the project name;
- a "What's left" view matching `bw todo`;
- the plan shape from `bw shape`: longest chain, most tasks ready at once, suggested crew size, and the tasks many others wait on, highlighted;
- roles on each task, and each worker's declared roles and effort;
- each worker's current activity as the board shows it: working (task and step progress), reviewing (review held), or idle since its last write. `bw wait` writes nothing, so don't add writes just to show waiting;
- resource levels from `bw doctor`: free disk and memory against the minimum and critical levels, with a clear banner when either is low or critical;
- reviews that idle agents can pick up.

### Claimed is not the same as in progress

A claimed task today looks the same whether its holder just claimed it, is busy on it, or went quiet. Show the difference everywhere a task's state appears (TUI, `bw web`, `bw todo`, `bw tasks`, `bw show`, `--json`), derived from the board, with no new writes:

| Shown as | When |
| --- | --- |
| claimed | Claimed, and the holder has written nothing since (no heartbeat, step tick, note or review request) for up to 10 minutes |
| in progress | The holder has written since the claim and its last write is within the stale window. Show step progress (`2/5`) when there are steps |
| in review | A review is requested for the current head; show waiting for a reviewer, under review (with the reviewer), or changes requested |
| idle | Claimed with no write for more than 10 minutes but less than the stale window: probably blocked or thinking; show how long |
| stale | Past the stale window, as now |

- "in progress" and "in review" use the in-progress colour; "claimed" and "idle" use a muted one; "stale" keeps its warning colour.
- The task list line shows the activity word in place of the bare "CLAIMED" (for example `IN PROGRESS · codex-ce17 · 2/5`), and the detail pane adds the time since the holder's last write and what it was (heartbeat, step, note, review request).
- The 10-minute threshold is a catalog field `idle_minutes` (default 10).

Both stay read-only except where they already allow writes (answering questions).

## 9. Agents view

A per-agent view of the plan in the TUI and `bw web`, backed by a new command. The command is `bw workers`, not `bw agents`, because `bw help agents` is already the agents topic and bw calls everyone who claims work a worker; the views label the section "Agents".

### `bw workers`

- `bw workers` lists every worker that has claimed a task, ticked a step, held a review, or written a question or note in the current catalog's history. `bw workers <name>` shows one worker in detail. `--json` returns the same; both views use it.
- Data comes from the state ref's history, as `bw stats` does today. Reuse that code; don't add a second history reader.
- For each worker:
  - current activity, as the board shows it: working (task and step progress), reviewing (review held), or idle since its last write;
  - declared roles and effort as last recorded (see "Recording roles and effort"), when known;
  - first and last activity times;
  - tasks done, released and currently held, with counts;
  - reviews taken, with verdicts (approved, changes requested), and questions and notes written;
  - total time holding claims.
- For each task the worker held, in time order:
  - task ID and title, size, and outcome (done, released, still held);
  - claimed at, done or released at, and the time between;
  - time from requesting review to the verdict, when there was a review;
  - each step with the time it took (see "Step durations").

### Step durations

- A step's duration is the time from the previous tick on the same claim (or from the claim, for the first tick) to its own tick.
- Show a duration only when it is meaningful. Show "unknown" when steps were ticked out of catalog order, when a step was unticked and ticked again, when several steps were ticked within one minute (finished together), or when the task changed hands in between.
- Record each tick's time in the state ref if it isn't already, with the same clock and precision as claim times.
- `bw stats --task <id>` shows the same step durations.

### Recording roles and effort

- When a worker claims a task or takes a review, record the `--role` and `--effort` it declared (if any) with that claim or review in the state ref, so the agents view can show what each worker was running as. Nothing else uses these recorded values.

### In the TUI and `bw web`

- Add an "Agents" section to both. A list shows each worker with current activity, tasks done, time holding claims and last activity, sorted with active workers first.
- Selecting a worker shows its detail from `bw workers`: its tasks in time order, each expandable to its steps and their durations, with review times and verdicts.
- In `bw web`, every task, review, branch and commit links to GitHub as elsewhere.
- Both stay read-only.

## 10. README

Rewrite `README.md` as the front page for someone who has never heard of bw. Use Markdown headings, short paragraphs, tables and fenced code blocks.

1. Open with two or three sentences: what bw is (a small Git-native CLI that coordinates several coding agents, or people, working one plan in one repository) and why you'd want it (no server, claims and reviews live in Git, dependencies unlock work, agents don't collide).
2. A short "How it works" section: the task file, the state ref, claims, needs, the review/question/note board. A small diagram (a fenced text block is enough) of a task moving through ready, claimed, review and done.
3. Install, then a quick start that sets up a project, writes a three-task plan, and has two workers claim and finish tasks, with real commands and their output.
4. The task file format as a table of every field, including those from this and the previous proposal.
5. A command reference table grouped by purpose (planning, working, reviewing, watching, setup), one line each, pointing to `bw help <command>`.
6. Short sections for `bw web` and the TUI (including the Agents view and `bw workers`), shell completion, `bw guide`, roles and effort, and resource guards.
7. "Running agents with bw": the kickoff instruction, one task vs continuous work, waiting with `bw wait`.
8. Limits and non-goals: cooperative, not a lock or permission system; no server; text on the board is pushed to the remote, so no secrets.
9. Licence and compatibility status (pre-release, no compatibility promise yet).

Keep it accurate to the shipped commands: generate or check any command lists against `bw help` so they cannot drift.

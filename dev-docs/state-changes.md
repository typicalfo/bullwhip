# Bullwhip: task state views and idle check (plan)

Oct 23, 2026

## Why

This plan adds four commands to bw: `bw tasks` shows every task by state, `bw summary` gives a one-screen overview, `bw stats` reports on the current run, and `bw idle` tells an idle worker whether to wait or stop. Build it on top of the current unmerged branch.

- **Idle agents pile up.** Workers told to wait while locked work remains all wait, even when one remaining task can only use one of them. For example, four idle workers all wait on a single remaining task.
- **No clear view of state.** `bw queue --all` mixes ready, waiting and done tasks in one list and doesn't say what a waiting task is blocked on or who holds it. Humans and agents both need to see what's done, claimed, ready and waiting at a glance.

## Commands

All four read state only. Each has `--json` for agents and plain aligned text for people. Every task has exactly one display state: **done**, **claimed** (ready and held), **ready** (ready and free) or **waiting** (some need not done).

### `bw tasks [--state S]... [--json]`

- Lists every task grouped by state, in the order claimed, ready, waiting, done. Within a group, highest priority first, then ID.
- Claimed rows show the holder and how long since their last activity, with the stale flag when it applies.
- Waiting rows show what blocks them: each need that is not done, with its state and holder, for example `waiting on api (claimed by worker-1, 12m)`.
- Done rows show who finished it and when.
- `--state` (repeatable) limits output to those states, for example `bw tasks --state waiting --state claimed`.
- It replaces `bw queue --all`: remove `--all` from `queue`, so `queue` only answers "what can I claim". No alias.

```text
CLAIMED  api        worker-1  active 4m
READY    docs       p35
WAITING  frontend   on api (claimed by worker-1)
DONE     schema     worker-2  2h ago
```

### `bw summary [--json]`

One screen: counts per state; each active claim with holder, age and stale flag; open reviews (waiting, taken) and unanswered questions (and how many need a coordinator). It ends with one line saying whether every task is done, for example `13 of 14 done; 1 claimed; nothing ready`.

### `bw idle --as NAME [--json]`

Tells an idle worker what to do next. It decides from current state, in this order:

1. **work**: the caller holds a claim. Its output names the task. Exit 0.
2. **claim**: a free ready task exists. Exit 0; the worker runs `bw crack`.
3. **wait**: W > C, where W is the number of waiting tasks and C the number of tasks claimed by other workers. Exit 0. The extra unlocked tasks would need more workers than those already holding claims.
4. **stop**: otherwise, including when every task is done. Exit 3. The workers holding claims pick up what their work unlocks.

JSON carries `action`, `task` (for work), `waiting`, `claimed` and a one-line `reason`. Help text explains the rule and that it is a cooperative heuristic, not a lock: several idle workers that check at the same moment can all see `wait`.

### `bw stats [--worker NAME] [--task ID] [--json]`

Reports on the current run, computed from the event history on `refs/bullwhip/state`. The run is the task catalog on origin's default branch: it starts at the first claim of any of its tasks and ends at the last done mark, or runs to now while tasks remain.

- **Run:** start time, elapsed wall time, done count against total, and tasks done per hour. When the run is finished, it also shows the end time and total duration.
- **Per worker:** tasks completed, total time holding claims, current claim, releases (including forced ones with their reason), reviews given with their verdicts, reviews received, questions asked and notes written. Sorted by tasks completed.
- **Per task:** who claimed it and when, every holder in order (releases and handoffs), time from ready to claimed, from claimed to done, review rounds and whether the approval was self-approved.
- **Waiting time:** how long tasks sat ready with no claim, and how long they sat waiting on needs, so a slow dependency chain shows up.
- `--worker` and `--task` narrow the report to one worker or one task.
- Times come from event timestamps only; bw records no new data for this. Durations are wall time, not effort.

```text
Run     started 12:31  elapsed 5h12m  13/14 done  2.5/h
WORKER     DONE  HELD    REVIEWS   RELEASES  CURRENT
worker-1   5     3h40m   4 (4 ok)  0         frontend 4m
worker-2   4     3h05m   3 (2 ok)  1
TASK       HOLDERS    READY>CLAIM  CLAIM>DONE  ROUNDS
schema     worker-2   1m           48m         1
api        worker-1   3m           1h22m       2
```

## Rules and done bar

Stack this on the current unmerged branch, as one MR or a short stack.

- Follow bullwhip's `AGENTS.md`. The spec goes in `dev-docs/bullwhip.md`, help in `internal/bw/help.go`, and the README is regenerated with `scripts/docs.sh`. Keep production code within the stated size budget.
- No back-compat: `queue --all` is removed outright, not aliased or deprecated.
- Read-only commands make no writes to `refs/bullwhip/state` and need no `--as`, except `idle`, which needs the caller's name.
- This plan is fixed. Decisions it leaves open are recorded as an addendum in the MR description; scope questions go to the project owner.
- **Done bar:**
  - Tests on disposable Git remotes with real competing workers cover every state in `tasks` and `summary`, `--state` filtering, waiting-reason text, each `idle` outcome, and `stats` figures (run start and end, per-worker and per-task durations, handoffs, review rounds, filters) checked against a scripted event history. The `idle` tests include four idle workers with one claimed task and one waiting task (all get `stop`) and one claim that unlocks three tasks (idle workers get `wait`).
  - `go test ./...`, `go test -race ./...`, `go vet ./...` and `go tool govulncheck ./...` pass, and `scripts/acceptance.sh` passes.
  - Plain-text output is shown in the MR for a sample board.

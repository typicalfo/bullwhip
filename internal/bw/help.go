package bw

const common = `
Output:
  Human output fits the terminal width (100 columns for pipes and files).
  Tables wrap cells or use labeled records on narrow terminals; no fields
  are truncated. Use --json for lossless structured output in agents/scripts.

Common flags:
  --json       Emit one JSON envelope: {ok, exit_code, data} or {ok, exit_code, error}.
  --as <name>  Name the worker that owns the claim, e.g. --as worker-1.
               Overrides BW_AS for this command.
  --role a,b  Declare roles (BW_ROLE); omitted means generalist.
  --effort LEVEL  Declare low, medium or high (BW_EFFORT).
  --help, -h   Show this command's help without accessing a repository.

Worker name:
  Pass --as <name> each time, or export BW_AS=worker-1 in the worker's shell.
  Use the same name for claim/crack, done and release. Give each concurrent
  worker a different name; bw does not identify or name agents automatically.
  With --json, claim/crack, done, release and board writes require --as or BW_AS.
  Without --json, Git's user.name is the fallback when neither is supplied.
  Reading queue/tasks/summary/stats/status/help, checking tasks, init and reopen
  need no name. idle needs the caller's identity.

Exit codes:
  0 success; 1 usage/setup error; 2 conflict (claimed by someone else, not
  ready, not yours); 3 no free ready task or idle/wait stop; 4 remote failure or busy;
  5 insufficient resources (claim/crack).

Retries:
  Repeating claim, crack, done, release or reopen is safe. If it already
  took effect, it succeeds with unchanged. Keep using the same worker name.

Debugging:
  BW_DEBUG=1 prints Git commands and their errors, credentials redacted.
`

var commandOrder = []string{"wait", "todo", "shape", "doctor", "workers", "guide", "web", "dashboard", "completion", "step", "init", "check", "queue", "tasks", "summary", "tui", "idle", "stats", "show", "claim", "crack", "release", "done", "reopen", "heartbeat", "status", "holds", "review", "ask", "answer", "questions", "note", "notes", "version", "help-2"}

var helpText = map[string]string{
	"wait": `bw wait --as NAME [--timeout 9m] [--interval 30s] [--json]
Block until idle advises work, claim, review or stop; on timeout print wait.
Timeout must be positive and at most 60m; interval must be at least 10s.
Fetches without heartbeats or state writes. Ctrl-C/SIGTERM exit cleanly.
Example: bw wait --as worker-1 --timeout 9m --json
` + common,
	"todo": `bw todo [--mine] [--role ROLES] [--effort LEVEL] [--as NAME] [--json]
What's left, in dependency order then priority: claimed, ready and waiting
work, holders, size, step progress and blockers. --mine includes your claims
and unheld work matching your roles and effort; requires your identity.
Example: bw todo --mine --as worker-1 --role frontend --json
` + common,
	"shape": `bw shape [--json]
Analyze remaining tasks: longest chain (length and IDs), equal-duration wave
width, suggested crew, tasks with four or more prerequisites and bottlenecks.
Warnings point to bw guide planning. check stays a quick catalog validator.
Example: bw shape --json
` + common,
	"doctor": `bw doctor [--json]
Show resource minimums, critical levels, sources, overrides and current values.
Defaults (GB, decimal): minimum disk 10, memory 2; critical disk 3, memory 0.5.
max_claims has no default limit. Critical levels must not exceed minimums.
BW_MIN_FREE_DISK_GB, BW_MIN_FREE_MEMORY_GB, BW_CRITICAL_FREE_DISK_GB,
BW_CRITICAL_FREE_MEMORY_GB and BW_MAX_CLAIMS override the catalog. Set
BW_RESOURCE_GUARD=off to disable. Other operating systems disable the guard.
macOS uses native Mach memory statistics (cgo build); Linux uses MemAvailable.
Claim/crack refuse with exit 5 below a minimum or at max_claims; existing
claims are unaffected. idle/wait return wait for minimums and stop for critical
levels, including for holders. doctor never writes state.
Example: bw doctor --json
` + common,
	"workers": `bw workers [NAME] [--json]
List agents in the current catalog's Git history, active first, or show one.
Includes roles/effort last declared at claim/review take, first/last activity,
claims, releases, completions, verdicts, questions, notes and held time.
Task histories include review delay and step durations. Ambiguous timing is
unknown (out-of-order, undone/re-ticked, batch ticks within a minute or handoff).
Example: bw workers worker-1 --json
` + common,
	"guide": `bw guide [planning|agents|operating] [--markdown]
Practical guidance for planning, working and running a crew.
Example: bw guide planning --markdown
` + common,

	"step": `bw step <task> <n> [--undo] [--as NAME] [--json]
Tick a 1-based checklist step as its holder. Ticks count as activity.
Steps are progress, not a gate: done warns about unticked steps.
Example: bw step api 1 --as worker-1 --json
` + common,
	"web": `bw web [--port PORT] [--open] [--as NAME]
Run the local dashboard on 127.0.0.1; an omitted port is chosen and printed.
Answers use the resolved identity and normal coordinator rules. Ctrl-C stops it. --json returns one snapshot.
Example: bw web --as coordinator --open
` + common,
	"dashboard": "Alias for bw web. See bw help web.\nExample: bw dashboard --as coordinator\n" + common,
	"completion": `bw completion bash|zsh|fish
bw completion install|uninstall [--shell bash|zsh|fish]
Print, install or remove shell completion. Installation prints paths and undo
instructions. Dynamic suggestions read local files/refs only, within 200ms.
Example: bw completion install --shell zsh
` + common,
	"tui": `Usage: bw tui [--as NAME] [--json]

Example: bw tui
Offline demo from a source checkout: examples/tui.sh

Leave this monitor open while the plan runs. It fetches one coherent
snapshot from origin every five seconds after the previous refresh completes.
Tasks show ownership, dependencies and plan references. Attention shows open
questions, stale claims and unmerged reviews; Notes shows newest handoffs first.
Enter opens question answers or review findings/history. Press h in Attention
to include answered questions and merged reviews.
Counts and the last successful refresh remain visible. A failed refresh keeps
the last snapshot, labels it as stale, and retries automatically. Initial errors
also retry. Stale claims indicate inactivity, not proven stalls; they never expire.

Keys: 1 tasks, 2 attention, 3 notes; Left/Right cycle sections (wrapping);
/ filter; Up/Down or j/k select;
Enter/Tab focus details; arrows/PgUp/PgDn scroll; Esc back; r refresh;
? help; q or Ctrl+C quit. While filtering, letters belong to the filter.
Wide terminals show list and detail together; narrow terminals drill into detail.

Press a on a question to write an answer. The editor shows your identity:
--as NAME, BW_AS, or Git user.name. Ctrl+S submits to origin; Esc cancels.
Submission uses the same coordinator rules and retry safety as bw answer.
Errors preserve your draft. While submitting, wait for the result before exit.
Answers are shared and retained in Git history; do not include secrets.

Browsing does not write state or need an identity. Only an explicitly submitted
answer writes; claims and review actions still use the CLI in another terminal.
The display stays open when all tasks are done.
--json emits one snapshot and exits, for scripts and noninteractive terminals.
Unset BW_DEBUG for the interactive view. Git authentication is the normal CLI's.
` + common,

	"help": `Usage: bw <command> [arguments] [flags]

Commands:
  init              Create the task file and origin's state ref; safe to repeat
  check             Validate task IDs and dependencies
  queue, q [--all]  Free ready tasks, or all task states
  tasks [--state S] All tasks grouped by state, with blockers
  summary           Counts, active claims, open reviews and questions
  tui               Live project monitor with questions, reviews and answers
  web, dashboard    Local web dashboard; --port PORT, --open
  completion        Print, install or uninstall bash/zsh/fish completion
  step <task> <n>   Tick a checklist step; --undo unticks
  idle              Decide whether to work, claim, review, wait or stop
  wait              Block until idle has an action; --timeout 9m, --interval 30s
  todo              What is left, in dependency order; --mine
  shape             Longest chain, parallel width and suggested crew
  doctor            Resource thresholds, sources and current levels
  workers [NAME]    Agents and task/step histories
  guide [topic]     Practical planning, agents and operating guidance
  stats             Run, worker and task statistics from Git history
  show <task>       State, holder, needs and unlocks
  claim <task>      Claim ready work; --next picks the highest-priority free task
  crack             Alias for claim --next
  release <task>    Return unfinished work; --force --reason for an idle or coordinated claim
  done <task>       Mark your claimed task done, unlocking dependent tasks
  reopen <task>     Undo a done mark
  heartbeat         Record that you are still working on your claims
  status            Claims, holders, ages and stale warnings
  holds [<name>]    Claims and pending reviews held; --check exits 2 if any
  review            Request, take, release and record reviews (bw review --help)
  ask <task> TEXT    Ask a question with a stable number
  answer <n> TEXT    Answer a numbered question
  questions [--all] List unanswered questions, or all history
  note <task> TEXT   Record a handoff or FYI
  notes [--task T]  List notes, optionally for one task
  help [topic]      Command or topic help: setup, planning, format, agents
  version           Print the build commit
  help-2            Overview, worker identity, repository and exit details

Flags: --json, --as <name>, --help (-h), --effort low|medium|high, --role a,b.
BW_ROLE declares roles; no roles means generalist. Use bw guide for workflows.
Optional task fields: size, effort, review_effort, steps, roles, review_roles. BW_EFFORT sets
the worker reasoning limit; explicit claim/review IDs override the limit.
Use bw <command> --help for usage and flags.
`,
	"help-2": `Usage: bw help-2 [--json]

Bullwhip (bw) tracks tasks, claims and a review/question/note board through Git.
You or your planning agent supply the tasks; bw follows that task list.

Loop: queue -> claim <task> -> work -> done <task>
Stop unfinished: stop work, then release <task>.
Claims stay until released; idle ones can be force-released.

Options: --json for agent output; --as <name> for the worker name; --help.

Worker name: --as worker-1 names the worker that owns a claim. Instead of
passing it each time, set export BW_AS=worker-1 in that worker's shell.
Use a distinct name per worker and the same name through claim, done and
release. Agents using --json must supply --as or BW_AS for those commands
(including crack and all board writes). Interactive commands can fall back to git user.name.

Repository: run from any subdirectory; remote: origin. Tasks come from
.bullwhip/tasks.json on origin's default branch, or the local file until
it is merged there.

Exits: 0 success, 1 usage/setup, 2 conflict, 3 no free ready task,
4 remote failure or busy. Repeating a command that took effect is safe.

Help: bw help setup; bw help planning; bw help format; bw help agents.
Use bw <command> --help for flags, or bw help agents --markdown for AGENTS.md.

Board text and findings are pushed to origin and retained in Git history.
Do not record secrets, prompts, customer data, personal identifiers or command output.

Git writers can bypass this cooperative tool. Review and execution follow
your project's own process. No server, daemon or worker launcher.

Example: bw init; bw queue; bw claim --next --as sam
`,
	"version": `Usage: bw version [--json]

Print the full source commit embedded at build time, with (modified) if the
source checkout had uncommitted changes. Works without Git or a repository.
If build metadata is unavailable, print unknown; build from a Git checkout
with Go's VCS metadata enabled to compare worker builds.
Example: bw version
` + common,
	"init": `Usage: bw init [--json] [--as <name>]

Create .bullwhip/tasks.json with one example task if absent. Create
refs/bullwhip/state on origin if absent. Existing tasks and state are kept.
Requires a Git working tree and an origin with read/write access.
Replace the example with your agreed tasks; bw help planning walks through it.
Commit and merge tasks.json to share the catalog. No application branch is
pushed. Uses your normal Git credentials; no server setup is installed.
Example: bw init
` + common,
	"check": `Usage: bw check [--json] [--as <name>]

Validate the local task file: format, unique IDs, known needs and no cycles.
This checks your working copy, the change you are about to merge. No remote
access is needed. Exit 1 explains what needs fixing.
Example: bw check --json
` + common,
	"queue": `--effort low|medium|high (or BW_EFFORT) filters automatic work by reasoning level.
Usage: bw queue [--all] [--json]
       bw q [--all] [--json]

List only free ready tasks, highest priority first, then ID.
--all includes waiting, claimed and done tasks in the same priority/ID order.
q is an alias for queue and accepts the same flags.
Use bw tasks for grouped states, blockers and completion times.
The JSON envelope's catalog field says which task file was used.
An empty list succeeds; claim --next uses exit 3 when no work is free.
Example: bw queue --json
` + common,
	"tasks": `Usage: bw tasks [--state S]... [--json]

List all tasks grouped as claimed, ready, waiting, done. Each group sorts by
priority then ID. Repeat --state to include any selected states.
Show holders and inactivity, unfinished dependencies with their holders,
and completion times recovered from Git history. No identity is required.
Example: bw tasks --state waiting --state claimed --json
` + common,
	"summary": `Usage: bw summary [--json]

Show counts by state, active claims, pending reviews and unanswered questions,
including how many require a coordinator. End with overall task progress.
No identity is required. This reads state without writing.
Example: bw summary --json
` + common,
	"idle": `bw idle --as NAME [--role ROLES] [--effort LEVEL] [--json]
Choose work (own claim), claim (eligible ready task), review (eligible open
review by another author), wait (eligible work is blocked/held), or stop.
A generalist declares no roles; otherwise at least one task role must match.
Effort and roles both apply. Explicit claim/review IDs bypass these filters.
idle_review:false disables review advice. wait writes no heartbeat.
Minimum resources or max_claims make claim advice wait (low_resources).
Critical resources always stop (critical_resources), including claim holders:
commit and push work, tear down stacks, release with a handoff note, clean up,
and stop. A stop action exits 3; other actions exit 0.
Example: bw idle --as worker-1 --role backend --effort high --json
` + common,
	"stats": `Also reports held wall time grouped by size, review waiting, and time from
claim or previous surviving tick to each surviving step tick. Undo removes ticks.
Worker working/idle durations start at their first claim; working is the union
of held intervals. Stale is held time past the recorded activity threshold.
Usage: bw stats [--worker NAME] [--task ID] [--json]

Report the current catalog's run, workers and task histories. Filters narrow
the detail rows, retaining the run totals. No identity is required.
Run start is its first claim; finish is the last done mark when all tasks
are done. An unfinished run goes to now. Ready/waiting time starts at run start.
Claim times use existing timestamps; done/release times use Git commit times
(second precision). These are wall times, not effort. No new data is recorded.
Fetches complete state history; unavailable or invalid history is an error.
Example: bw stats --worker worker-1 --json
Example: bw stats --task api
` + common,

	"show": `Usage: bw show <task> [--json] [--as <name>]

Show title, ref, priority, state, holder, age, needs and direct unlocks.
For local ref fragments such as plan.md#slice-4, prints that section from
the working tree. Missing anchors warn; claims still succeed. No execution
or network lookup. Paths must remain inside the repository.
Example: bw show api --json
` + common,
	"claim": `--next matches --effort low|medium|high or BW_EFFORT; an explicit ID ignores it.
Usage: bw claim <task> [--json] [--as <name>]
       bw claim --next [--json] [--as <name>]

Claim only a free task whose needs are all done.
  --next   Take the highest-priority free ready task, breaking ties by ID.
           If you already hold a claimed task, return it instead.
Concurrent claims have exactly one winner. Claiming a task you already hold
succeeds with unchanged. Exit 2 means a conflict; exit 3 means --next found
nothing free. Work may begin only after success.
Alias: bw crack is shorthand for bw claim --next.
Example: bw claim --next --as sam --json
` + common,
	"crack": `Declare --effort low|medium|high or BW_EFFORT to limit automatic selection.
Usage: bw crack [--json] [--as <name>]

Crack the whip: alias for bw claim --next. Take the highest-priority free
ready task, breaking ties by ID. If you already hold a claimed task, return
it instead. Uses the same claims, identity rules, retries and exit codes.
Exit 3 means no task is free and ready. Work may begin only after success.
Example: bw crack --as sam --json
` + common,
	"release": `Usage: bw release <task> [--force --reason TEXT] [--json] [--as <name>]

Give your claimed task back unfinished. Stop working before releasing.
  --force  Release another holder's claim. Allowed when the holder has been
           inactive for stale_hours (default 4), or at any time for a worker
           listed in the task file's coordinators.
  --reason Required with --force; recorded as a note on the task.
A holder is active while they run bw writes: claim, heartbeat, notes,
questions or reviews. Time never releases a claim by itself.
Releasing a task nobody holds succeeds with unchanged. Exit 2 means it is
done, not yours, or another holder's claim is still active.
Example: bw release api --force --reason "agent stopped at 01:11" --as alice
` + common,
	"heartbeat": `Usage: bw heartbeat [--json] [--as <name>]

Record that you are still working on your open claims. Any bw write also
counts, but long stretches of coding without one need a heartbeat: run it at
least every hour, so your claims do not become releasable by others.
Prints your open claims. Exit 3 means you hold none: if you were working on
one, it was released by a coordinator or after inactivity. Stop, read
bw notes --task <id>, and do not mark that work done.
Example: bw heartbeat --as sam
` + common,
	"done": `Usage: bw done <task> [--json] [--as <name>]

Mark your claimed task done, unlocking tasks whose needs are now all done.
Follow your project's review/verification process before marking done.
Exit 2 means it is not claimed by you. Repeating done succeeds with unchanged. This is a cooperative done mark,
not independent verification or a check of the code in your repository.
Example: bw done api --as sam --json
` + common,
	"reopen": `Usage: bw reopen <task> [--json] [--as <name>]

Undo an accidental done mark. Any worker may reopen; coordinate first.
Waiting/ready states are recomputed. Existing descendant claims and done
marks remain; coordinate any affected work through your project process.
Exit 2 means the task is claimed. A task that is not done succeeds with
unchanged. It becomes free, not claimed by you.
Example: bw reopen api
` + common,
	"status": `Usage: bw status [--json] [--as <name>]

Show every claimed task, its holder, age and how long the holder has been
idle. Flag claims whose holder has been inactive for stale_hours (default 4).
Time never releases or transfers a claim.
Claims for removed tasks also appear and can still be released by ID.
Example: bw status --json
` + common,
	"holds": `Usage: bw holds [<name>] [--check] [--json] [--as <name>]

Read origin's state without writing: show this worker's claimed tasks and
pending reviews, including self-reviews. Reviews in taken or requested state
count only while held; verdicts, release and merge do not.
Authored but unheld reviews, questions and notes are excluded.
Claims stay until released; idle ones can be force-released.
Ages and stale flags are reported without releasing.
With no name, use --as, BW_AS, then Git user.name (interactive only).
With --json and no name, --as or BW_AS is required. Unknown names hold nothing.
  --check  Exit 2 if anything is held, otherwise 0. JSON retains data on exit 2
           and error explains what is held and how to release it.
Without --check, a successful query always exits 0. Exit 1 means usage/setup;
exit 4 means remote failure or busy. No write access is needed.
Example: bw holds --as worker-1          # what am I holding?
Example: bw holds worker-3 --check --json # can worker-3 be removed? exit 2 if not
` + common,
	"review": `Automatic take matches --effort low|medium|high (or BW_EFFORT) against
review_effort, falling back to task effort. An explicit review ID ignores it.
Review list includes the effective effort level.
Usage: bw review <command> [--json] [--as NAME]

request <task> [--branch B] [--head SHA] [--note TEXT]
  Request review of your claimed task. Branch defaults to the current branch;
  head defaults to HEAD and must resolve to a Git commit. IDs replace / with --.
  Re-request after changes or a new head. Existing reviewer ownership is kept.
  Example: bw review request api --note "Ready" --as author --json
list [--open] [--mine]
  List reviews by ID; --open excludes merged; --mine means authored or held by
  you. self=true labels self-reviews; state labels self verdicts.
  Example: bw review list --open --json
take [<id>] [--self]
  Without an ID, take the oldest unheld request by another author, or return
  your held pending review unchanged on retry. Exit 3 if none is waiting.
  An explicit ID held by someone else conflicts (exit 2), regardless of age.
  Authors must give an ID and --self, and wait self_review_after (default 30m).
  Example: bw review take feature--api --as reviewer --json
release <id> [--force]
  Holder releases unfinished review. Mid-review releases re-queue it; after
  changes, wait for the author's next request. Approved/merged is unchanged.
  --force lets a human release another holder after coordinated stop.
  Example: bw review release feature--api --as reviewer --json
approve <id> [--notes FILE] [--model NAME]
  Holder approves the latest requested head. A self verdict is self-approved.
  Example: bw review approve feature--api --model codex --as reviewer --json
changes <id> --notes FILE [--model NAME]
  Holder requests changes; findings required. A self verdict is self-changes.
  Findings are appended as Markdown; model is optional audit text.
  Example: bw review changes feature--api --notes findings.md --as reviewer --json
merged <id> [--sha SHA]
  Author records merge after approval (including self-approved). SHA defaults
  to HEAD and must be an ancestor of origin's default branch. Does not run done.
  Example: bw review merged feature--api --as author --json
show <id>
  Show task, branch, author, holder, latest state, event history and findings.
  Example: bw review show feature--api --json

Review logs and findings share the CAS-updated refs/bullwhip/state tree with
claims. Board data uses board_version=1 in state.json.
No automatic takeover. Repeating an applied transition is unchanged.
Your project's docs decide reviewers, checks and merge policy; bw records.
Text is pushed to origin and kept in history: no secrets, prompts, customer
data, personal identifiers or command output.
` + common,
	"ask": `Usage: bw ask <task> TEXT [--technical] [--json] [--as NAME]

Append a question with a stable increasing integer ID. The same actor/task/text
returns unchanged on retry. Task is a context label; it need not be claimed.
When the task file lists coordinators, only they may answer, unless the
question is asked with --technical (how to do the work, not what it is).
  --technical  Any worker may answer.
Text is published on origin and retained in history. Never include secrets,
prompts, customer data, personal identifiers or command output.
Example: bw ask api "Which format?" --as worker-1 --json
` + common,
	"answer": `Usage: bw answer <n> TEXT [--json] [--as NAME]

Append an answer to an existing positive question number. Multiple answers
are allowed. The same actor/question/text returns unchanged on retry.
If the task file lists coordinators, a question asked without --technical
accepts answers only from them (exit 2 otherwise).
Answers are published to origin and retained in history; use commit-safe text.
Example: bw answer 1 "Use JSON." --as worker-2 --json
` + common,
	"questions": `Usage: bw questions [--all] [--json]

List unanswered questions by stable ID. --all includes answered questions and
all their answers. Reading an empty list succeeds.
Example: bw questions --all --json
` + common,
	"note": `Usage: bw note <task> TEXT [--json] [--as NAME]

Append a handoff or FYI. The same actor/task/text returns unchanged on retry.
Task is a context label, independent of claims. Notes are pushed to origin
and retained in history: no secrets, prompts, customer data, personal
identifiers or command output.
Example: bw note api "Finish documentation next." --as worker-1 --json
` + common,
	"notes": `Usage: bw notes [--task T] [--json]

List notes in append order. --task T filters by task context label.
Example: bw notes --task api --json
` + common,

	"setup": `Install shell completion with bw completion install, optionally --shell
bash|zsh|fish. Remove it with bw completion uninstall using the same shell.

Getting started: install once, set up each repository once.

Install with Go and Git available:
  go install github.com/typicalfo/bullwhip/cmd/bw@latest
Or from a Bullwhip source checkout:
  go install ./cmd/bw
This installs bw into GOBIN, or the bin directory of GOPATH when GOBIN is
unset. Put that directory on PATH. Check with: bw help

In the Git repository you want to coordinate:
  git remote -v
  bw init
Use the repository's existing origin remote and normal Git credentials.
If origin is missing, add it with git remote add origin <repository-url>.
bw init writes an example .bullwhip/tasks.json and creates the shared state
ref on origin. It requires read/write access; it does not push your code.

Discuss your plan with a planning agent and ask it to run bw help planning.
The agent replaces the example with your agreed tasks, runs bw check, then
helps you commit and merge the task file using your normal Git workflow.
Each repository has its own task file and shared state; no project
registration, configuration file or project ID is needed.

Once the task file is merged, workers can run from any repository subdirectory:
  bw queue
  bw claim <task-id> --as sam
  # Do the work as directed by the user.
  bw done <task-id> --as sam
If stopping unfinished, stop work first, then bw release <task-id> --as sam.
Use bw status to see holders and old claims.
Claims stay until released; idle ones can be force-released.
For agents, choose a distinct worker name before claiming. Either pass
--as worker-1 on each command, or export BW_AS=worker-1 in that worker's
shell and use --json. Keep that name for done and release. See bw help agents.

Every worker uses .bullwhip/tasks.json from origin's default branch once it
exists there. Until then bw uses the local file and says so. bw check always
checks your local edits. Merge catalog changes before assigning shared work.
Example: bw init; bw check; bw queue
` + common,
	"planning": `Set up an agreed plan for Bullwhip.

A planning agent turns the user's task list into .bullwhip/tasks.json.
Use the names, dependencies and references agreed with the user. Setting up
bw requires no knowledge of the project's language, code layout or internals.
The user and planner decide the work and what done means. bw reads task
metadata; it does not interpret plan prose or impose a document template.

1. Run bw init in the user's Git repository (see bw help setup for access).
2. Replace the example in .bullwhip/tasks.json with the agreed tasks:

{
  "tasks": [
    {"id": "draft", "title": "Prepare the draft", "ref": "plan.md"},
    {"id": "review", "title": "Review the draft", "needs": ["draft"]},
    {"id": "notes", "title": "Collect notes"}
  ]
}

Only id and title are required. ref is optional text pointing to instructions
wherever the user keeps them. A local fragment (plan.md#slice-4) is resolved
from the working tree by show, claim and crack; missing anchors warn.
needs lists task IDs that must be marked done first. Independent tasks omit
needs. Work finished before tracking is added as an ordinary task, claimed
and marked done once. Only done marks for tasks in this catalog satisfy
needs; no external system satisfies a prerequisite. priority is optional (default 0, higher first); ties sort by ID.
Use bw help format for the full format and ID rules. Use stable IDs when
editing a plan; use a new ID for new work, since old IDs retain their state.

3. Run bw check. Fix any format, duplicate-ID, missing-dependency or cycle errors.
4. Commit .bullwhip/tasks.json and any user-chosen plan documents, then merge
   using the repository's normal process. bw does not commit or merge for you.
5. Run bw tasks. draft and notes start ready; review waits for draft.
   Give workers bw help agents, or its --markdown output for AGENTS.md.

The JSON file is the complete bw plan format. Descriptions may use any
format or location the user wants. bw check validates task metadata and
dependencies; task contents and completion decisions belong to the user.
Editing the catalog needs no task claim. Claim before starting the listed
work, and mark it done only when the user-defined work is complete.
Example: bw init; bw check; bw tasks
` + common,
	"format": `Optional task fields: size (xs/s/m/l/xl), effort and review_effort
(low/medium/high); review_effort defaults to effort. steps has 1–32 strings
(up to 120 characters) or {"id":"stable-id","text":"description"} entries.
Step keys must be unique. Stable IDs preserve ticks when text is reworded.
Catalog repo_url optionally overrides the origin-derived repository web URL. If set, forge must be github; GitHub is the default and the only supported link format.
Upgrade all workers before using new fields; decoding rejects unknown fields.

Task file: .bullwhip/tasks.json

The current Git working tree is the project. bw finds its root from any
subdirectory. Commit and merge edits normally: every worker uses the copy on
origin's default branch, whatever branch or worktree it is on, so all agree
on what is ready. Until the file is merged there, the local copy is used and
bw says so. bw check validates the local copy. No project ID/config exists.

{
  "stale_hours": 4,
  "coordinators": ["alice"],
  "self_review_after": "30m",
  "tasks": [
    {"id": "api", "title": "Build the API", "ref": "specs/api.md", "priority": 10},
    {"id": "ui", "title": "Build the UI", "needs": ["api"]},
    {"id": "docs", "title": "Write the guide"}
  ]
}

id and title are required. IDs: 1-128 lowercase letters, digits, dot,
underscore or hyphen, beginning with a letter or digit. ref is an optional
link or path to a full description. needs defaults to []; priority is an
integer, default 0 (higher goes first). stale_hours is a nonnegative integer:
the hours a holder may go without any bw write before others may release the
claim with --force; 0 or omission uses the default 4. coordinators optionally
lists worker names who may release any claim with --force and a reason.
Unknown fields, duplicate JSON keys and null task arrays fail.
Run bw check after editing. Unknown dependencies, duplicate IDs and cycles fail.
self_review_after is a nonnegative Go duration string (default "30m").
A ref fragment may name a Markdown heading slug or explicit HTML id/name
anchor. show, claim and crack print its local section; a missing section
warns without blocking a claim. Other references remain links.
Descriptions can use any format or location. bw does not check project code. See bw help planning to create the task list.

api and docs begin ready. ui waits until api is marked done. A claimed task
stays claimed until explicitly released or done. Removing/readding an ID
retains its state; use a new ID for new work. Changing needs doesn't cancel
existing claims or done marks. Reopening a parent changes readiness but
leaves descendants' existing claims/done marks alone: coordinate with them.

Run scripts/acceptance.sh for local validation.

Claims and done marks live in state.json on origin's refs/bullwhip/state,
an independent Git history updated with compare-and-swap pushes. Concurrent
updates reload and recheck before retrying; unrelated marks are preserved.
Never edit this ref by hand. Git write access can bypass bw's checks.
Example: bw check --json

Additional catalog fields: description (one line, at most 200 characters), roles
(allowed role names), idle_review (default true), idle_minutes (default 10),
resources (min_free_disk_gb, min_free_memory_gb, critical_free_disk_gb,
critical_free_memory_gb, max_claims). See bw help doctor for defaults/overrides.
Task roles and review_roles are lists of role names following task ID rules.
Without roles a task is open to all; without worker roles a worker is a generalist.
Review roles apply to automatic review take; explicit IDs override filters.
` + common,
	"agents": `Tick steps with bw step <task> <n> as you finish them. When your operator gives
you a reasoning level, declare --effort low|medium|high (or BW_EFFORT).

Planning agents: use bw help planning to write the agreed task catalog.
That setup needs no knowledge of the project internals or task-document format.

bw crack --json is shorthand for bw claim --next --json.

Working agents: choose your worker name before your first claim.
The name records who holds the task. It is a claim label, not Git login.
bw does not detect an agent's identity or generate a name automatically.
Choose a name different from every other concurrent worker, such as worker-1.

Supply your name in either of two ways:
  --as worker-1        Pass it on every claim/crack, done and release.
  export BW_AS=worker-1  Set the default in your worker's shell environment.
--as overrides BW_AS for that command. Keep the same name for the entire
work loop. If commands run in fresh shells, pass --as each time or set
BW_AS in each invocation; an export in a previous shell will not carry over.

Use --json for agent commands. claim/crack, done and release with --json
require --as or BW_AS; Git's user.name is not used for these agent commands.
Board writes also require --as or BW_AS with --json. Read-only commands
need no name, except idle, holds without a name and review list --mine use
your identity. For interactive use without --json,
Git's user.name is the fallback when neither --as nor BW_AS is set.

Always claim before starting: bw claim <task> --json, or bw crack --json.
Never work on a task you do not hold. bw show <task> --json displays its
holder and dependencies.
Example:
  export BW_AS=worker-1  # Choose a distinct identity for this worker.
  bw queue --json
  bw crack --json
  # Use the returned task id and ref to follow the user's instructions.
  bw done <task-id> --json

When idle, run bw idle --json and follow data.action: work resumes the named
claim; claim means run bw crack; review means take the named review; wait
means run bw wait --json; stop means
report and stop. The wait decision reserves no worker slots: several idle
workers can all see wait. Use bw tasks for the complete board and blockers.

The same claim without setting an environment variable:
  bw crack --as worker-1 --json
  # Work, then keep the same name:
  bw done <task-id> --as worker-1 --json

bw tracks ownership and dependencies. Follow the user's instructions for the
work and for deciding when to mark it done.
While working on a claim, run bw heartbeat --json at least every hour (any
bw write such as a note or review request also counts). A claim whose holder
has been silent for stale_hours can be released by others. If heartbeat
exits 3, or your task shows as ready or held by someone else, your claim was
released: stop, read bw notes --task <id>, and do not mark that work done.
If stopping unfinished, stop all task work, then bw release <task> --json.
Before you stop, run bw holds --check --json; finish or release anything it lists.
Never release another worker's claim unless your instructions say to; the
command is bw release <task> --force --reason "why", and it is recorded.

Read exit_code and ok from the JSON envelope:
0: success; begin work only after a successful claim.
1: usage/setup error; fix the input/configuration using the error message.
2: conflict; inspect bw show <task> or choose another ready task.
3: no free work, or idle says stop. After crack returns 3, use bw idle --json
   to decide. Wait only when its action is wait; stop when its action is stop.
4: remote failure or busy; do no new work. Retry the same command once the
   remote is reachable. If the earlier attempt took effect, the retry
   succeeds with "unchanged": true. claim --next returns a task you hold.

Never edit refs/bullwhip/state by hand. Change task descriptions/dependencies
only through the repository's normal reviewed .bullwhip/tasks.json changes.
A claim is cooperative coordination, not permission to merge or deploy.
The native board records your project's review process; it does not run
checks, merge branches, enforce model choices or mark tasks done on merge.
Use every board command with --json and a distinct --as or BW_AS identity.
Examples (author worker-1, reviewer worker-2):
  bw review request api --branch feature/api --note "Ready for review" --json
  bw review list --open --json
  bw review take feature--api --as worker-2 --json
  bw review show feature--api --json
  bw review changes feature--api --notes findings.md --as worker-2 --json
  # Author fixes changes and requests review again; reviewer then approves:
  bw review approve feature--api --notes findings.md --model codex --as worker-2 --json
  bw review release feature--api --as worker-2 --json
  # After your project's merge process:
  bw review merged feature--api --json
  bw ask api "Which format should this use?" --json
  bw answer 1 "Use JSON." --json
  bw questions --all --json
  bw note api "Next worker: finish the docs." --json
  bw notes --task api --json
review take without an ID takes the oldest waiting review by another author.
A held review never expires. Stop reviewing before release; a human may
coordinate review release <id> --force. Self-review needs an explicit ID,
--self, and self_review_after elapsed without a taker (default 30m).
Self verdicts are labeled self-approved or self-changes. They remain visible
in list/show; self-approved permits recording merged.
Board questions, notes and findings are published to origin and retained in
Git history. Do not store secrets, prompts, customer data, personal identifiers
or command output. Repeating identical text by the same actor is unchanged;
use different wording for a distinct repeated question or note.
Use bw help agents --markdown to print this block for AGENTS.md.

When idle returns wait, run bw wait --as NAME --json and act on the result;
never poll in a loop of your own. For review, run bw review take <review ID>,
follow the project's review process, then idle again. Declare --role (or BW_ROLE)
and --effort when supplied by your operator. A generalist declares no roles.
When critical_resources stops you: commit and push, tear down stacks, release
your claim, record a handoff note, clean up, and stop. Clean up per task.
See bw guide agents for practical worker guidance.
` + common,
}

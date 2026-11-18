# CLI qualification

Run `./scripts/acceptance.sh` from a source checkout. It runs:

- `go test -count=1 ./...`
- `go test -race -count=1 ./...`
- `go vet ./...`
- `go tool govulncheck ./...`
- `./examples/try-it.sh`

## Coverage

Tests use actual Git processes and disposable bare remotes. Concurrent claim
processes produce exactly one holder for a task, while unrelated updates
preserve every successful change. Concurrent initializers and question
writers preserve state and unique IDs. Review-take races have one winner.
Lost push acknowledgements are recognized without duplicating writes.

Task coverage includes catalog validation, dependency unlocking, priority
ordering, ownership, stale-release rules, removed catalog tasks, safe
retries, reopening and JSON identity precedence. Workers read the shared
catalog from origin's default branch. Commands work from subdirectories;
local validation and help do not need remote access. Queue queries cover
ready-only and --all views, the q alias, priority ordering, empty results,
text and JSON output, invalid combinations, and unchanged remote state. Task
state filters cover individual states, unions and repeated flags. Main help
and the extended `help-2` overview are checked outside a repository.

Review coverage includes request, exclusive take, release, changes,
re-request, approval and merge recording. Merge ancestry and local commit
resolution are checked. Self-review requires the configured wait and an
explicit selection; both self-verdicts are labeled. Ownership does not
expire. A merge does not mark a task done.

Questions, answers, notes and findings survive mixed concurrent task and
board writes. Unknown state-tree files are preserved without interpretation.
Board schema metadata and file-size limits are checked. Markdown ref
fragments support headings and explicit anchors; missing anchors warn,
and paths escaping the repository are rejected.

Holds coverage includes claims versus done/released tasks, removed tasks,
stale flags, ages, pending and completed review states, re-requests,
self-reviews, unknown workers, identity precedence, populated JSON and
release instructions on exit 2, nested directories and remote failure.
Queries work when a remote hook refuses every write, and leave the remote
state tip unchanged.

Help is exercised outside a repository. The README drift test compares the
checked-in file with the output of the help generator. The demo races two
workers in separate clones, shows holds before and after completion, and
checks that completion unlocks dependent tasks.

## Limits

Qualification uses macOS arm64 and disposable local remotes with synthetic
workers. It does not qualify other platforms or hosted GitHub state-ref
permissions. No application workers, queues, host configuration or deployment
are activated by these tests. Git credentials can bypass CLI ownership
checks; coordination is cooperative.

## Latest run — October 23, 2026 (readable CLI output and queue compatibility)

`scripts/acceptance.sh` passed on `codex/readable-cli-output`, on macOS arm64
with Go 1.27.3 and Git 2.39.5 (Apple Git-154):

- `go test -count=1 ./...` passed (`internal/bw`: 89.001s).
- `go test -race -count=1 ./...` passed (`internal/bw`: 110.643s).
- `go vet ./...` passed.
- `go tool govulncheck ./...` reported no vulnerabilities.
- `./examples/try-it.sh` passed: one racing holder and dependency unlocking.

New tests cover `queue --all` and `q` in text/JSON, all four task states,
priority ordering, empty queues, alias help and argument validation. Reads
succeed against a write-rejecting disposable remote and preserve its state
and local refs. Formatting tests cover 24, 40, 60, 80, 100 and 140 columns,
long blocker lists, titles, workers, Unicode, terminal controls, unbroken
text, partial writes and lossless JSON values. The first full run exposed
leading spaces before wrapped idle advice; the formatter now preserves the
first-line prefix, and the final standard/race runs above passed.

The disposable TUI demo was inspected through tmux at 100x30 and 60x24,
including manual refresh, section navigation and quit. Routine refresh no
longer adds the refreshing label; existing error and frame tests pass.
`scripts/docs.sh` regenerated README and `git diff --check` passed.
No installed binary, real project queue, workers, host configuration or
hosted permissions were changed. Other operating systems remain unqualified.

## Previous run — October 23, 2026 (build version and claim help)

`./scripts/acceptance.sh` passed in the private version/help clone on macOS
arm64 with Go 1.27.3 and Git 2.39.5 (Apple Git-154):

- `go test -count=1 ./...` passed (`internal/bw`: 76.026s).
- `go test -race -count=1 ./...` passed (`internal/bw`: 91.408s).
- `go vet ./...` passed.
- `go tool govulncheck ./...` reported no vulnerabilities.
- `./examples/try-it.sh` passed.

Version tests compare the compiled binary's revision to its source checkout,
run it outside a repository with Git absent from PATH, verify JSON/text agree,
and check an actual `-buildvcs=false` binary reports unavailable metadata.
Help tests include the new command and regenerated README. `git diff --check`
passed. Source builds without VCS metadata cannot identify their commit;
this is reported as unknown, never inferred from the runtime repository.

## Previous run — October 23, 2026 (state views)

`./scripts/acceptance.sh` passed on the final state-view branch in its private
clone, on macOS arm64 with Go 1.27.3 and Git 2.39.5 (Apple Git-154):

- `go test -count=1 ./...` passed (`internal/bw`: 78.218s).
- `go test -race -count=1 ./...` passed (`internal/bw`: 95.589s).
- `go vet ./...` passed.
- `go tool govulncheck ./...` reported no vulnerabilities.
- `./examples/try-it.sh` passed, showing tasks, summary, stats and idle along
  with the competing claims and dependency unlocking.

New tests cover grouped task states, priority/ID ordering, repeatable state
filters, blocker holders/inactivity/staleness, completion timestamps, summary
review and coordinator-question counts, reopened prerequisites and preserved
ownership. Real competing processes exercise claim ownership and four idle
workers against one versus three waiting tasks. Every new query succeeds
against a write-rejecting remote and leaves its state tip and local refs intact.

A fixed-timestamp Git history checks run boundaries and throughput, ready,
waiting and held durations, ordered handoffs, ordinary and forced releases,
forced reasons, review rounds and self-approval, worker/task filters, ongoing
and reopened runs. Corrupt historical state fails explicitly; a concurrent
update does not move the snapshot being analyzed. Unannotated release actors
remain unknown. Wall times are not effort; overlapping claims sum durations.

`scripts/docs.sh` regenerated README and `git diff --check` passed. A prior
acceptance run caught README drift while help was being regenerated; the
final run above used a fixed tree. Sample text output is included in the MR.
The owner waived the obsolete production line-count target. Platform and
hosted-permission limitations below still apply; no real queues were activated.

## Previous run — October 23, 2026 (help/queue integration)

`./scripts/acceptance.sh` passed in the private help/queue clone on macOS
arm64 with Go 1.27.3 and Git 2.39.5 (Apple Git-154):

- `go test -count=1 ./...` passed (`internal/bw`: 64.302s).
- `go test -race -count=1 ./...` passed (`internal/bw`: 71.966s).
- `go vet ./...` passed.
- `go tool govulncheck ./...` reported no vulnerabilities.
- `./examples/try-it.sh` passed: one racing holder and dependency unlocking.

`scripts/docs.sh` regenerated README; `git diff --check` passed. The new
queue filters and both help pages also passed focused tests. The follow-up
queue/status formatting change uses aligned task, status, worker, age, idle
and title columns; the disposable demo output was inspected for alignment.
These checks were rerun after merging the heartbeat and coordinator changes
from master, preserving inactivity reporting in the aligned output.
All remotes and
workers used for qualification were disposable; hosted permissions and other
platforms remain unqualified.

## Previous run — October 22, 2026

All commands above passed on macOS arm64 with Go 1.27.3 and Git 2.39.5
(Apple Git-154). Standard tests completed in 51.125s and race tests in
57.658s for `internal/bw`. Vet passed, govulncheck found no vulnerabilities,
and the demo confirmed one racing holder and dependency unlocking.
Acceptance ran with a filtered PATH containing Go and system tools, with
no additional language interpreter available. Runtime dependencies are Go's
standard library and Git; the pinned vulnerability checker is a development
tool dependency. Production Go totals 2,585 lines, excluding tests.

`scripts/docs.sh` regenerated README; `git diff --check` passed. A direct
JSON probe confirmed the removed command is treated as any unknown command:
exit 1 and a pointer to `bw help`.

## Human TUI — October 23, 2026

Implemented and qualified in a private clone on `codex/human-tui`. The original
checkout and hosted repository were not changed. Charm dependencies are pinned
at Bubble Tea v2.1.0, Bubbles v2.2.1 and Lip Gloss v2.0.6.

`scripts/acceptance.sh` passed: standard tests (83.216s), race tests (107.858s),
vet, govulncheck (no vulnerabilities), and the competing-worker demo. After
final UI refinements, focused TUI/monitor race tests passed (9.096s), and vet
and govulncheck passed again. `examples/tui.sh --json` passed using disposable
local repositories. README was regenerated with `scripts/docs.sh`; the help
and README drift test and `git diff --check` passed.

New tests cover snapshot selection across refreshes, filtered selection,
stale filter results, shortcut letters while typing, timer coalescing,
failed-read retention/retry, read cancellation, remote/local ref preservation,
dependency unlocking, stale claims and terminal escape stripping. Frame tests
cover 140x40, 100x30, 80x24, 60x24, 24x8, 12x4 and 1x1, expanded help and both
focus modes; empty/error/Unicode frames also exercise light and dark palettes.
An initial color-sensitive text assertion was corrected to strip ANSI styling
before checking the visible quit hint.

A tmux PTY smoke test used only a synthetic local plan. Captured rendered
frames were inspected at 120x32, 100x30, 80x24 and 60x24. Exercised task,
question/review and note views, Enter/Esc/Tab focus, filtering the letter q,
scroll keys, resize, refresh and quit. Temporarily making the disposable
remote unavailable showed the failure banner and retained the last snapshot;
restoring it recovered automatically. Normal quit restored the terminal and
returned to the shell. The offline demo remains available as `examples/tui.sh`.

No live project queue, real workers, hosted Git authentication, Windows/Linux
terminal runtime, interactive child process or mouse workflow was exercised.
Stale claims were tested with synthetic timestamps, not a multi-hour live run.

Final-source reruns of `go test ./...` and `go test -race ./...` both passed.
Standard tests completed in 92.748s; the full race result is recorded below:

```text
?   	github.com/typicalfo/bullwhip/cmd/bw	[no test files]
ok  	github.com/typicalfo/bullwhip/internal/bw	106.175s
```

## TUI visual polish — October 23, 2026

The monitor now uses an amber identity badge and selected tabs, a completion
bar with task counts, semantic task-state colors, highlighted selection,
quieter detail labels, and a lighter pane divider. Connection failures retain
an explicit warning color and text. Filtering has its own footer so q is not
advertised as quit while it is being typed. Short terminals show the compact
quit/resizing message instead of clipping controls beneath the extra chrome.

Existing frame, focus, filtering and refresh tests passed after styling changes;
the final focused race run passed in 9.368s. Rendered tmux frames were inspected
at 110x30, 100x30 and 60x24 with the offline demo, including Tasks, Attention,
question detail and filter input. Automated frames cover the existing size
matrix, light/dark palettes and Unicode/error content. No live project state
was changed by the visual qualification.

The full acceptance script passed for this pass: `go test -count=1 ./...`
(87.370s), `go test -race -count=1 ./...` (98.987s), `go vet ./...`,
`go tool govulncheck ./...` (no vulnerabilities) and the competing-worker demo.
`scripts/docs.sh` and `git diff --check` also passed.

## TUI navigation and answers — October 23, 2026

Left/Right now cycle the three sections with wrapping. Filter input retains
arrow-key cursor movement. Attention can show answered questions and merged
reviews with h; review details include findings and the event history.
The a action opens a question draft with the resolved identity, Ctrl+S submits
through the existing answer operation, and Esc cancels. Browsing stays free of
writes; a submission waits for a result, prevents duplicate in-flight submits,
and preserves the draft on errors.

Focused tests cover arrow navigation, filter cursor editing, answer-editor key
ownership, duplicate-submit prevention, draft retention, success/cancel states,
editor sizing, coordinator rejection, idempotent retries and board history.
A disposable-remote PTY test at 100x30 submitted a synthetic answer as lead,
then displayed the saved author/text and updated unanswered count. Review
history was inspected at 60x24. No real project or hosted state was modified.

`go test ./...` passed (95.127s), `go test -race ./...` passed (109.111s),
`go vet ./...` passed, and `go tool govulncheck ./...` found no vulnerabilities.
The competing-worker demo passed. A final focused race run passed (11.350s),
including an adjustment to keep background filter results flowing during answer
editing; its dedicated draft-preservation regression test also passed under the
race detector (1.469s). README regeneration and `git diff --check` passed.

## Task metadata and dashboard qualification — October 24, 2026

Implementation and tests used the private clone
`bullwhip-private-metadata-20261024` on `codex/task-metadata-dashboard`.
The original checkout and installed `bw` were not changed. Development
commands operated only on disposable repositories with local bare origins.
The game view remains deferred.

Schema/upgrade note for the merge request: optional task fields are `size`,
`effort`, `review_effort`, and `steps`; optional catalog fields are `repo_url`
and `forge`. Step commands add `step_ticks` and `step_catalog` to task marks
on `refs/bullwhip/state`. All workers must upgrade before catalogs use new
fields or workers write step progress. Links, progress, step changes,
`merged_sha`, and timing metrics are computed output, not additional state
fields. The owner installs after merge.

New coverage includes field/step validation, holder-only ticking and undo,
ID-preserving edits and additions, removal/reopening, done warnings in JSON,
effort-limited automatic claims and review takes, explicit-ID overrides,
idle stopping on higher-effort work, and two competing tick processes
preserving both updates. Statistics tests check keyed step intervals, size
aggregation and worker working/idle/stale intervals. Forge tests cover GitHub link building,
escaping, repository containment, and merged-commit links after
review list removes its event history.

Dashboard HTTP tests cover vendored assets, SSE cancellation, coordinator
answer authorization, and rejection of cross-origin/foreign-host requests.
Manual browser checks used a disposable local project: board, burn-down,
throughput, time and worker views rendered; submitting a synthetic answer
as the coordinator persisted it and displayed it after refresh. Ctrl-C
stopped the foreground process. No real project queue or worker was started.
Tabler 1.7.1 is pinned with package and licence hashes in
`internal/bw/webassets/VENDOR.txt`; its upstream npm package omits the root
licence, so the retained MIT licence comes from the recorded upstream commit.

Completion install/uninstall tests use temporary HOME/config directories,
including repeat installs. Offline completion succeeds with an unreachable
origin. Bash and zsh scripts pass their native syntax checks; fish script
creation/removal is tested, but fish is not installed on this host, so its
native parser and interactive behavior were not exercised. Dynamic review
and question suggestions depend on a state ref already available locally;
completion never fetches one.

Timing decisions: step time is from the claim or preceding surviving tick,
not measured effort. Undo removes its timestamp. Review waiting ends at a
verdict or the next request for that same review. Worker working/idle time
starts at the first claim, uses the union of claim intervals, and stale is
the working-time subset beyond the activity threshold. Sum-of-claim held
time remains separately available. Plain-string replacements at the same
position are labeled reopened; stable-ID rewording keeps its tick. File
links use the locally known default branch, falling back to HEAD. Web JSON
mode returns one snapshot, matching the TUI convention.

Sample plain-text board from the disposable dashboard project:

```text
STATE    TASK       WORKER    DETAILS                             TITLE
CLAIMED  api        worker-1  active 7m ago                       Build task metadata API [size m,
                                                                  effort high, steps 1/2]

READY    docs       -         p0                                  Document the workflow [size s,
                                                                  effort low]

WAITING  dashboard  -         on api (claimed by worker-1,        Ship the project dashboard [size
                              active 7m ago)                      l, effort medium]

```

Final acceptance results (macOS, Go toolchain from go.mod):

- `go test -count=1 ./...` — passed (internal/bw: 119.003s).
- `go test -race -count=1 ./...` — passed (internal/bw: 132.298s).
- `go vet ./...` — passed.
- `go tool govulncheck ./...` — passed; “No vulnerabilities found.”
- `./examples/try-it.sh` — passed; exactly one competing holder and correct unlocking.
- `./scripts/acceptance.sh` — passed, including all checks above.
- `node --check internal/bw/webassets/dashboard.js`, `bash -n` and `zsh -n`
  on generated completion scripts — passed.
- Manual local-ref completion returned the review ID and question number in
  24.7 ms. Browser qualification additionally found and fixed a pending-review
  rendering bug (verdict is absent until recorded); both the review and time
  views were rechecked successfully with that pending round.

The final browser-only fix handles an absent verdict and does not change Go
behavior. The embedded bundle was rebuilt and its JavaScript syntax and live
pending-review rendering checked after that fix. Fish and real remote hosting
integration are the platform/integration limits; no live project was used.

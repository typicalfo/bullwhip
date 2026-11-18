# Waiting, roles and planning delivery

Implemented from `proposal-waiting-roles-guide.md` in a private clone on
`codex/waiting-roles-guide`. The preceding metadata/dashboard proposal was
already implemented on the base branch; its deferred game view stays deferred.

## Behavior

- `wait` shares idle advice, blocks without writes, supports bounded timeouts
  and intervals, and cancels its Git subprocesses on interruption.
- Idle advice respects roles and effort, offers eligible reviews, waits for
  blocked eligible work, and reports resource minimums/critical levels.
- `shape`, `todo`, `doctor`, `workers` and `guide` expose the new planning and
  operational information. Tasks and summary include useful worker counts.
- Claim activity is derived as claimed, in progress, in review, idle or stale.
  Display activity is separate from the task's coordination state, so ownership
  and dependency checks retain their existing meanings.
- Workers and statistics share the existing first-parent Git history reader.
  Historical tick snapshots preserve predecessor times through catalog edits;
  unordered, undone/repeated, batched or cross-claim timings are unknown.
- TUI and web views share command data, including What's left, shape,
  resources and Agents. Agent tasks expand into step/review timing. Web
  expansion survives refreshes. Both dashboards retain their existing answer
  action; the new views are read-only.
- README is regenerated as a front page with a checked command reference.

## Fields requiring coordinated upgrades

New catalog fields in `.bullwhip/tasks.json`:

- `description`
- `roles`
- `idle_review`
- `idle_minutes`
- `resources.min_free_disk_gb`
- `resources.min_free_memory_gb`
- `resources.critical_free_disk_gb`
- `resources.critical_free_memory_gb`
- `resources.max_claims`

New per-task fields: `roles`, `review_roles`.

New fields reaching `refs/bullwhip/state`:

- `state.json` task marks: `roles`, `effort`, when declared at claim.
- Review `taken` events in `reviews/<id>/events.jsonl`: `roles`, `effort`,
  when declared at review take.

Step tick timestamps and step-catalog snapshots already existed in state; this
change reads their history. Activity, shape, resource readings and worker
statistics are derived and add no state writes. There is no new state version,
heartbeat while waiting, lock or lease. All workers must upgrade before using
new catalog/state fields because strict decoders reject unknown fields.

## Validation and limits

Validation uses only disposable repositories with local bare origins and
synthetic workers. Development binaries were built in the private clone or test
temporary directories. No development command was pointed at a live project's
state ref. The installed `bw`, shell completion and host configuration were not
changed.

Validation completed successfully on 2026-10-24 (America/New_York):

- `go test ./...` and `go test -count=1 ./...`: passed, including real
  competing processes, role/review selection, capacity contention, signal
  cancellation, read-only wait behavior and worker history.
- `go test -race ./...` and the acceptance race suite: passed.
- `go vet ./...`: passed.
- `go tool govulncheck ./...`: no vulnerabilities found.
- `scripts/docs.sh` and README drift checks: passed.
- `scripts/acceptance.sh`: passed standard/race tests, vet, vulnerability
  checks and the disposable competing-worker demonstration.
- Linux and macOS-without-cgo cross-builds: passed compilation.
- Focused TUI description-refresh, Agents drill-down and wait-identity
  regressions: passed after final UI/identity adjustments.
- `git diff --check`: passed.

The default claim-capacity guard is disabled for ordinary test fixtures so
results do not depend on the developer machine's free memory/disk. Dedicated
resource tests explicitly enable it, with deterministic thresholds and real
competing claim processes against a disposable remote.

macOS readings use native Mach APIs through cgo; Linux reads `MemAvailable`.
Neither invokes extra runtime tools or needs elevated privileges. macOS builds
with `CGO_ENABLED=0` compile but report unavailable memory readings; use a cgo
build or explicitly disable the guard. Linux was cross-built on macOS; its
runtime readings were not exercised on a Linux host. Other systems disable the
guard and explain that in doctor.

Browser checks used a disposable synthetic plan: board activity/roles/steps,
What's left dependency order, shape, forced critical-resource banner and
threshold provenance, expandable Agents task/step/review history, retained
expansion after live refresh, and no browser console warnings/errors. A PTY
check exercised Agents/task drill-down, back and quit, including terminal
restoration. Automated frame checks cover wide and narrow terminals.

The repository has no project licence file to cite; README states that fact
and identifies the separately vendored Tabler licence without inventing a
project licence.

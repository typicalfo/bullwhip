# Working on Bullwhip

Canonical project: [github.com/typicalfo/bullwhip](https://github.com/typicalfo/bullwhip).

## Scope

- The implementation specification is `dev-docs/bullwhip.md`. Command help and the generated README describe the public CLI.
- Build a small deterministic `bw` CLI: repository-local tasks, Git claims/done marks, dependency unlocking.
- Only the foreground, loopback-only `bw web` dashboard server is allowed; no daemon, model launcher, project registry, leases, start tokens, verification ledger or shared-resource locks. Review, verification and deployment remain each project's own process.
- Other projects keep their plans and task coordination artifacts in their own repositories. Do not import them here.
- Finishing the CLI does not authorize real project activation, worker execution, host configuration, deployment or changing branch protections. Tests use disposable local repositories and synthetic workers.

## Engineering

- Work on an isolated branch. Do not overwrite a human branch or force-push. Delivery follows the owner's instructions and live protections.
- State lives on origin's `refs/bullwhip/state`. Use normal compare-and-swap Git pushes with fresh reads and re-evaluate after contention. Never merge or rebase stale state candidates.
- A claim never expires. Staleness only flags it; another worker's stale claim requires an explicit coordinated `release --force`.
- Git credentials can bypass the CLI. Describe cooperative coordination accurately; do not claim hostile-client enforcement.
- Never store secrets, prompts, customer data or command output in state. Preserve normal SSH/TLS verification and credential handling.
- Exercise real competing processes and disposable Git remotes in tests. Run `go test ./...`, `go test -race ./...`, `go vet ./...`, and `go tool govulncheck ./...`; record commands, results and limitations. `scripts/acceptance.sh` runs all checks and the demo.
- Keep help alongside commands in `internal/bw/help.go`. Run `scripts/docs.sh` to regenerate README; a test checks for drift.

## Calling bw as an agent

Read `bw help agents` (or `bw help agents --markdown`). The loop is queue, claim, work, done; stop work before releasing unfinished tasks. Use a distinct identity and `--json`. Follow the project's own review process before done. Never edit the state ref by hand or work on a task held by someone else.

Do not activate project queues or run workers without an explicit assignment.

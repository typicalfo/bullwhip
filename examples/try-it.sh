#!/usr/bin/env bash
# Only disposable local repositories; no model, real worker or network access.
set -euo pipefail
project_root="$(cd "$(dirname "$0")/.." && pwd)"
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
cd "$project_root"
go build -o "$sandbox/bw" ./cmd/bw
git init --bare --quiet "$sandbox/origin.git"
git init --quiet "$sandbox/work"
cd "$sandbox/work"
git remote add origin "$sandbox/origin.git"
"$sandbox/bw" init
cat > .bullwhip/tasks.json <<'TASKS'
{"tasks":[
 {"id":"design","title":"Design the change","priority":10},
 {"id":"docs","title":"Write docs"},
 {"id":"build","title":"Build it","needs":["design"]},
 {"id":"review","title":"Review it","needs":["build"]}
]}
TASKS
git -c user.name=example -c user.email=example@localhost add .bullwhip/tasks.json
git -c user.name=example -c user.email=example@localhost -c core.hooksPath=/dev/null -c commit.gpgSign=false commit --quiet -m 'Example task graph'
# Merge the catalog to origin's default branch; every worker reads it there.
git -c core.hooksPath=/dev/null push --quiet origin HEAD
git clone --quiet "$sandbox/origin.git" "$sandbox/other"
# Both processes wait on pipe barriers in separate clones.
mkfifo "$sandbox/alice.start" "$sandbox/bob.start"
for worker in alice bob; do
  (
    IFS= read -r _ < "$sandbox/$worker.start"
    if [ "$worker" = bob ]; then cd "$sandbox/other"; fi
    set +e
    "$sandbox/bw" claim design --as "$worker" --json > "$sandbox/$worker.json"
    echo "$?" > "$sandbox/$worker.exit"
  ) &
done
# Release each reader through its own barrier.
printf 'go\n' > "$sandbox/alice.start"
printf 'go\n' > "$sandbox/bob.start"
wait
alice="$(cat "$sandbox/alice.exit")"
bob="$(cat "$sandbox/bob.exit")"
cat "$sandbox/alice.json" "$sandbox/bob.json"
if [ "$alice/$bob" = 0/2 ]; then winner=alice
elif [ "$alice/$bob" = 2/0 ]; then winner=bob
else echo "Expected one winner and one conflict, got $alice/$bob" >&2; exit 1
fi
"$sandbox/bw" holds --as "$winner"
"$sandbox/bw" tasks
"$sandbox/bw" summary
"$sandbox/bw" stats
"$sandbox/bw" idle --as "$winner" --json
"$sandbox/bw" done design --as "$winner"
"$sandbox/bw" holds --check --json --as "$winner"
"$sandbox/bw" claim --next --as "$winner"
"$sandbox/bw" done build --as "$winner"
"$sandbox/bw" tasks
printf 'PASS: one racing holder; completing design unlocked build, then review.\n'

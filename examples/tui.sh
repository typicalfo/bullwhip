#!/usr/bin/env bash
# Offline human-monitor demo. All state lives in disposable local repositories.
set -euo pipefail
project_root="$(cd "$(dirname "$0")/.." && pwd)"
sandbox="$(mktemp -d)"
trap 'rm -rf "$sandbox"' EXIT
cd "$project_root"
go build -o "$sandbox/bw" ./cmd/bw
git init --bare --quiet --template= "$sandbox/origin.git"
git init --quiet --template= "$sandbox/demo"
cd "$sandbox/demo"
git remote add origin "$sandbox/origin.git"
"$sandbox/bw" init > /dev/null
cat > .bullwhip/tasks.json <<'TASKS'
{"coordinators":["lead"],"tasks":[
 {"id":"design","title":"Agree on the design","priority":10},
 {"id":"api","title":"Implement the service","needs":["design"]},
 {"id":"docs","title":"Write operator documentation"},
 {"id":"release","title":"Prepare the release","needs":["api","docs"]}
]}
TASKS
git add .bullwhip/tasks.json
git -c user.name=example -c user.email=example@localhost -c core.hooksPath=/dev/null -c commit.gpgSign=false commit --quiet -m 'Synthetic TUI catalog'
git -c core.hooksPath=/dev/null push --quiet origin HEAD
"$sandbox/bw" claim design --as demo-designer > /dev/null
"$sandbox/bw" done design --as demo-designer > /dev/null
"$sandbox/bw" claim api --as demo-builder > /dev/null
"$sandbox/bw" ask api 'Which retry behavior should users see?' --as demo-builder > /dev/null
"$sandbox/bw" note api 'Implementation is ready; waiting for a product decision.' --as demo-builder > /dev/null
"$sandbox/bw" review request api --branch feature/api --as demo-builder > /dev/null
"$sandbox/bw" tui --as lead "$@"

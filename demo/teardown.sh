#!/bin/sh
# Remove everything demo/seed.sh created: the kovan-demo-* tmux sessions and
# the demo root. Touches nothing else.
set -eu

ROOT="${KOVAN_DEMO_ROOT:-/tmp/kovan-demo}"

tmux ls -F '#S' 2>/dev/null | grep '^kovan-demo-' | while read -r s; do
	tmux kill-session -t "$s"
done
rm -rf "$ROOT"

echo "demo removed ($ROOT, kovan-demo-* tmux sessions)"

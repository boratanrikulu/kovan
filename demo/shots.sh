#!/bin/sh
# Regenerate the demo screenshots (docs/img/{board,method,needs-you,monitor}.png)
# from the seeded demo, in iTerm2, with no manual clicking. macOS + iTerm2 only.
#
# It seeds the demo, opens a fixed-size iTerm2 window running the cockpit,
# drives the TUI to each state by injecting keystrokes, and captures the window
# itself (screencapture -l) — so the shots keep the rounded, transparent
# corners and drop shadow of a hand-taken window screenshot. The mouse cursor
# is never captured.
#
# One-time: grant Screen Recording to whatever runs this (iTerm2/Terminal) in
# System Settings > Privacy & Security > Screen Recording, or the grabs come
# out empty.
#
# Tunables (env):
#   KOVAN_SHOT_COLS / KOVAN_SHOT_ROWS  window size; set once to match framing
#   KOVAN_SHOT_PROFILE                 iTerm2 profile (default: Default)
#   KOVAN_SHOT_TEARDOWN=1              remove the demo when done
set -eu

case "$(uname)" in
Darwin) ;;
*)
	echo "shots.sh needs macOS + iTerm2." >&2
	exit 1
	;;
esac
[ -d /Applications/iTerm.app ] || {
	echo "iTerm2 not found at /Applications/iTerm.app." >&2
	exit 1
}
command -v swift >/dev/null 2>&1 || {
	echo "swift not found (install the Command Line Tools: xcode-select --install)." >&2
	exit 1
}

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
IMG_DIR="$REPO_DIR/docs/img"
DEMO_ROOT="${KOVAN_DEMO_ROOT:-/tmp/kovan-demo}"
BIN="$REPO_DIR/bin/kovan"

COLS="${KOVAN_SHOT_COLS:-127}"
ROWS="${KOVAN_SHOT_ROWS:-36}"
PROFILE="${KOVAN_SHOT_PROFILE:-Default}"

# --- fresh binary + demo -----------------------------------------------------
make -C "$REPO_DIR" build >/dev/null
sh "$SCRIPT_DIR/seed.sh" >/dev/null
echo "seeded; shooting within the 2-minute summary-freshness window"

# --- open the cockpit window (returns the iTerm2 window id) -------------------
WINID="$(osascript <<OSA
tell application "iTerm2"
	set w to (create window with profile "$PROFILE")
	tell current session of w
		set columns to $COLS
		set rows to $ROWS
		write text "cd $DEMO_ROOT/repos/gecit && KOVAN_HOME=$DEMO_ROOT/home '$BIN'"
	end tell
	activate
	return id of w
end tell
OSA
)"

cleanup() {
	osascript -e "tell application \"iTerm2\" to close (windows whose id is $WINID)" 2>/dev/null || true
	if [ "${KOVAN_SHOT_TEARDOWN:-0}" = "1" ]; then sh "$SCRIPT_DIR/teardown.sh" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT INT TERM

# key <string>: inject raw bytes into the running TUI (not a shell line).
sess="tell application \"iTerm2\" to tell current session of (first window whose id is $WINID)"
key() { osascript -e "$sess to write text \"$1\" newline no"; }
esc() { osascript -e "$sess to write text (ASCII character 27) newline no"; }

# cgwindowid <x> <w>: the CGWindowID of the frontmost iTerm2 window at that
# left edge and width — screencapture -l needs the CG id, not iTerm2's own id.
cgwindowid() {
	swift - "$1" "$2" <<'SWIFT'
import CoreGraphics
import Foundation
let a = CommandLine.arguments
let x = Double(a[1])!, w = Double(a[2])!
let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
for win in list {
	guard (win[kCGWindowOwnerName as String] as? String) == "iTerm2" else { continue }
	guard let b = win[kCGWindowBounds as String] as? [String: Double] else { continue }
	if abs((b["X"] ?? -1) - x) < 6 && abs((b["Width"] ?? -1) - w) < 6 {
		print(win[kCGWindowNumber as String] as! Int)
		exit(0)
	}
}
exit(1)
SWIFT
}

# shot <name>: window-capture the cockpit into docs/img/<name>.png.
shot() {
	b="$(osascript -e "tell application \"iTerm2\" to get bounds of (first window whose id is $WINID)")"
	x="$(echo "$b" | cut -d, -f1 | tr -d ' ')"
	w=$(($(echo "$b" | cut -d, -f3 | tr -d ' ') - x))
	cg="$(cgwindowid "$x" "$w")" || {
		echo "could not resolve the window; is Screen Recording granted?" >&2
		exit 1
	}
	screencapture -x -l"$cg" -t png "$IMG_DIR/$1.png"
	echo "  $1.png"
}

sleep 3 # let the cockpit load and capture the selected agent's peek

# the board as it opens: ipv6 selected, its summary and peek below (board.png
# is not in the README today, but the demo keeps it current for reuse)
shot board

# method view; move the file cursor 8 entries down (3 global files + 2 global
# skills + account voice + 2 account skills) onto the project's context.md, so
# the contents pane previews the repo's own notes, not the first file.
key m
sleep 1
i=0; while [ "$i" -lt 8 ]; do key j; sleep 0.2; i=$((i + 1)); done
sleep 2
shot method
esc
sleep 1

# down to mapiter (the needs-you row); wait for its peek to populate
key j
sleep 2
shot needs-you

# the S monitor page (every running agent's summary)
key S
sleep 2
shot monitor
key S

echo "done -> $IMG_DIR/{board,method,needs-you,monitor}.png"

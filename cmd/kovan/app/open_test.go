package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/session"
)

// TestWakeLaunchExpiredTranscript pins the recovery path: the agent tool prunes
// transcripts on its own retention window, so an agent older than it must still
// open — fresh, rebuilding from the notes — instead of failing on a --resume
// with nothing to resume.
func TestWakeLaunchExpiredTranscript(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sid := "3c0e895f-0717-422e-b768-0c0bfdf7b143"
	m := &session.Manifest{ID: "SC-1", Repo: "r", Worktree: home, SessionID: sid}
	transcriptPaths.Delete(sid)

	mode, prompt := wakeLaunch(m)
	if mode != launchFresh {
		t.Error("expired transcript should start fresh, got resume")
	}
	if !strings.Contains(prompt, "previous conversation is gone") {
		t.Errorf("prompt should say the history is gone, got %q", prompt)
	}
	if !strings.Contains(prompt, "wait for my go") {
		t.Errorf("prompt should stop before changing anything, got %q", prompt)
	}

	// The conversation is still on disk: resume it, unchanged from before.
	dir := filepath.Join(home, ".claude", "projects", "proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriptPaths.Delete(sid)
	if mode, _ := wakeLaunch(m); mode != launchResume {
		t.Error("a live transcript should resume, got fresh")
	}

	// An agent with no session id at all keeps the old --continue behaviour.
	if mode, _ := wakeLaunch(&session.Manifest{ID: "SC-2", Worktree: home}); mode != launchResume {
		t.Error("no session id should resume (--continue), got fresh")
	}
}

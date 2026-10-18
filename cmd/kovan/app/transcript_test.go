package app

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTranscript plants a transcript for sessionID under a temp HOME, so
// the claude driver's ~/.claude/projects glob finds it hermetically.
func writeTranscript(t *testing.T, sessionID, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", "-repo-x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTranscriptDigestBySessionID(t *testing.T) {
	writeTranscript(t, "sess-digest-1",
		`{"type":"user","message":{"content":"hello agent"}}`+"\n")
	if got := convoOf("", "", "sess-digest-1").digest(); got != "you: hello agent" {
		t.Errorf("digest = %q", got)
	}
	if got := convoOf("", "", "").digest(); got != "" {
		t.Errorf("empty session id: digest = %q, want empty", got)
	}
	if got := convoOf("", "", "sess-missing").digest(); got != "" {
		t.Errorf("missing transcript: digest = %q, want empty", got)
	}
}

// TestTranscriptModeAfterRefactor guards the perm-column path that now shares
// the transcript tail with the digest.
func TestTranscriptModeAfterRefactor(t *testing.T) {
	writeTranscript(t, "sess-perm-1",
		`{"type":"user","permissionMode":"bypassPermissions","message":{"content":"x"}}`+"\n")
	mode, ok := convoOf("", "", "sess-perm-1").perm()
	if !ok || mode != "bypassPermissions" {
		t.Errorf("transcriptMode = %q, %v", mode, ok)
	}
}

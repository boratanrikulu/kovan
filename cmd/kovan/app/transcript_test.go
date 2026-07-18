package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDigest(t *testing.T) {
	lines := []string{
		`ge","content":[{"type":"text","text":"partial first line from a mid-record tail"}]}}`,
		`{"type":"user","message":{"role":"user","content":"Read your brief and execute it"}}`,
		`{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"private"},{"type":"text","text":"I'll start by reading the brief."}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"go test ./...","description":"Run the suite"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok  \tpkg 0.2s"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","input":{"file_path":"/repo/main.go"}}]}}`,
		`{"type":"system","subtype":"turn_duration","level":null}`,
		`{"type":"last-prompt","lastPrompt":"an unsent suggestion would not even be here","sessionId":"x"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Done. Tests pass."}]}}`,
	}
	got := digest([]byte(strings.Join(lines, "\n")))
	want := []string{
		"you: Read your brief and execute it",
		"agent: I'll start by reading the brief.",
		"agent ran Bash: Run the suite",
		"agent ran Read: /repo/main.go",
		"agent: Done. Tests pass.",
	}
	if got != strings.Join(want, "\n") {
		t.Errorf("digest =\n%s\nwant\n%s", got, strings.Join(want, "\n"))
	}
	if strings.Contains(got, "private") || strings.Contains(got, "tool_result") || strings.Contains(got, "unsent") {
		t.Errorf("digest leaked a skipped record:\n%s", got)
	}
}

func TestDigestCapsEntries(t *testing.T) {
	var lines []string
	for i := 0; i < digestMaxEntries+10; i++ {
		lines = append(lines, fmt.Sprintf(`{"type":"user","message":{"content":"turn %d"}}`, i))
	}
	got := strings.Split(digest([]byte(strings.Join(lines, "\n"))), "\n")
	if len(got) != digestMaxEntries {
		t.Fatalf("entries = %d, want %d", len(got), digestMaxEntries)
	}
	if got[len(got)-1] != fmt.Sprintf("you: turn %d", digestMaxEntries+9) {
		t.Errorf("cap must keep the newest entries, last = %q", got[len(got)-1])
	}
}

// writeTranscript plants a transcript for sessionID under a temp HOME, so
// transcriptTail's ~/.claude/projects glob finds it hermetically.
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
	if got := transcriptDigest("sess-digest-1"); got != "you: hello agent" {
		t.Errorf("digest = %q", got)
	}
	if got := transcriptDigest(""); got != "" {
		t.Errorf("empty session id: digest = %q, want empty", got)
	}
	if got := transcriptDigest("sess-missing"); got != "" {
		t.Errorf("missing transcript: digest = %q, want empty", got)
	}
}

// TestTranscriptModeAfterRefactor guards the perm-column path that now shares
// transcriptTail with the digest.
func TestTranscriptModeAfterRefactor(t *testing.T) {
	writeTranscript(t, "sess-perm-1",
		`{"type":"user","permissionMode":"bypassPermissions","message":{"content":"x"}}`+"\n")
	mode, ok := transcriptMode("sess-perm-1")
	if !ok || mode != "bypassPermissions" {
		t.Errorf("transcriptMode = %q, %v", mode, ok)
	}
}

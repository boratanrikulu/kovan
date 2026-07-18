package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/boratanrikulu/kovan/internal/session"
)

func TestMapMode(t *testing.T) {
	cases := map[string]string{
		"default":           "default",
		"acceptEdits":       "auto",
		"plan":              "plan",
		"bypassPermissions": "bypass",
		"weird":             "weird", // unknown passes through
	}
	for in, want := range cases {
		if got := mapMode(in); got != want {
			t.Errorf("mapMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLastPermissionMode(t *testing.T) {
	data := []byte(`{"type":"user","permissionMode":"default"}
{"type":"assistant","permissionMode":"acceptEdits"}
{"type":"user","msg":"no mode here"}
`)
	if got, ok := lastPermissionMode(data); !ok || got != "acceptEdits" {
		t.Errorf("lastPermissionMode = (%q,%v), want acceptEdits", got, ok)
	}
	// A null value is not a match; fall back to the prior string value.
	withNull := append(data, []byte(`{"permissionMode":null}`+"\n")...)
	if got, ok := lastPermissionMode(withNull); !ok || got != "acceptEdits" {
		t.Errorf("with trailing null = (%q,%v), want acceptEdits", got, ok)
	}
	if _, ok := lastPermissionMode([]byte(`{"type":"user"}`)); ok {
		t.Error("no permissionMode should not match")
	}
}

func TestPermMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sid := "11111111-2222-4333-8444-555555555555"
	proj := filepath.Join(home, ".claude", "projects", "-Users-bora-x")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := `{"permissionMode":"default"}` + "\n" + `{"permissionMode":"acceptEdits"}` + "\n"
	if err := os.WriteFile(filepath.Join(proj, sid+".jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}

	// The transcript's latest mode wins over a stale hook Mode in the manifest.
	m := &session.Manifest{SessionID: sid, Mode: "auto"}
	if got := permMode(m); got != "auto" { // acceptEdits → auto, matches here by mapping
		t.Errorf("resolveMode = %q, want auto (from transcript acceptEdits)", got)
	}

	// Make the manifest disagree so the test proves the source is the transcript.
	m.Mode = "plan"
	if got := permMode(m); got != "auto" {
		t.Errorf("resolveMode = %q, want auto from transcript, not the manifest's plan", got)
	}

	// No transcript → fall back to the manifest's hook Mode.
	missing := &session.Manifest{SessionID: "no-such-id", Mode: "plan"}
	if got := permMode(missing); got != "plan" {
		t.Errorf("resolveMode (no transcript) = %q, want the manifest fallback plan", got)
	}
}

func TestTranscriptModeCachedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sid := "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	proj := filepath.Join(home, ".claude", "projects", "-Users-bora-a")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(proj, sid+".jsonl")
	if err := os.WriteFile(path, []byte(`{"permissionMode":"acceptEdits"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := transcriptMode(sid); !ok || got != "acceptEdits" {
		t.Fatalf("first resolve = (%q,%v), want acceptEdits", got, ok)
	}

	// The path is now cached. Fresh content at the same path is picked up.
	if err := os.WriteFile(path, []byte(`{"permissionMode":"plan"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := transcriptMode(sid); !ok || got != "plan" {
		t.Errorf("cached-path read = (%q,%v), want plan", got, ok)
	}

	// A moved transcript invalidates the cached path and re-resolves.
	proj2 := filepath.Join(home, ".claude", "projects", "-Users-bora-b")
	if err := os.MkdirAll(proj2, 0o755); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(proj2, sid+".jsonl")
	if err := os.WriteFile(moved, []byte(`{"permissionMode":"default"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, ok := transcriptMode(sid); !ok || got != "default" {
		t.Errorf("after move = (%q,%v), want default via re-resolve", got, ok)
	}

	// Gone entirely → no match, and the stale entry doesn't wedge the miss.
	if err := os.Remove(moved); err != nil {
		t.Fatal(err)
	}
	if _, ok := transcriptMode(sid); ok {
		t.Error("removed transcript should not resolve")
	}
}

func TestNewSessionID(t *testing.T) {
	id, err := session.NewID()
	if err != nil {
		t.Fatal(err)
	}
	// UUIDv4 shape: 8-4-4-4-12 hex, version 4.
	if len(id) != 36 || id[14] != '4' {
		t.Errorf("NewID = %q, want a v4 uuid", id)
	}
	if other, _ := session.NewID(); other == id {
		t.Error("NewID should be random, got a repeat")
	}
}

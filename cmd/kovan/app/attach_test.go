package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBrief(t *testing.T) {
	taskAbs := t.TempDir()
	brief := filepath.Join(taskAbs, "context.md")

	src := t.TempDir()
	var images []string
	for _, n := range []string{"a", "b"} {
		p := filepath.Join(src, n+".png")
		if err := os.WriteFile(p, []byte("PNG-"+n), 0o644); err != nil {
			t.Fatal(err)
		}
		images = append(images, p)
	}

	body := "Look at [[image #1]] and [[image #2]] here.\n\nmore prose"
	if err := writeBrief(taskAbs, brief, "TASK-1", "fix vfs", "", briefInput{text: body, images: images}); err != nil {
		t.Fatal(err)
	}

	// Images moved into images/paste-N.png, bytes intact, sources gone.
	for i, want := range []string{"PNG-a", "PNG-b"} {
		dst := filepath.Join(taskAbs, "images", fmt.Sprintf("paste-%d.png", i+1))
		if got, err := os.ReadFile(dst); err != nil || string(got) != want {
			t.Errorf("image %d = %q (err %v), want %q", i+1, got, err, want)
		}
	}
	if _, err := os.Stat(images[0]); !os.IsNotExist(err) {
		t.Errorf("source image should be moved, stat = %v", err)
	}

	got, _ := os.ReadFile(brief)
	for _, want := range []string{
		"# TASK-1 — fix vfs",
		"![image #1](./images/paste-1.png)",
		"![image #2](./images/paste-2.png)",
		"more prose",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("brief missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(string(got), "[[image #1]]") {
		t.Errorf("tokens should be resolved, not left literal:\n%s", got)
	}
}

func TestWriteBriefEmpty(t *testing.T) {
	taskAbs := t.TempDir()
	brief := filepath.Join(taskAbs, "context.md")
	template := "# TASK-1 — fix vfs\n\n## Summary\n"
	if err := os.WriteFile(brief, []byte(template), 0o644); err != nil {
		t.Fatal(err)
	}
	// An empty brief is a no-op: the scaffolded template stands (CLI $EDITOR path).
	if err := writeBrief(taskAbs, brief, "TASK-1", "fix vfs", "", briefInput{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(brief); string(got) != template {
		t.Errorf("empty brief should leave the template unchanged, got:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(taskAbs, "images")); !os.IsNotExist(err) {
		t.Error("empty brief should not create an images dir")
	}
}

package config

import (
	"strings"
	"testing"
)

// The templates and the structs must describe the same schema: every key the
// template documents must exist in the code, and every leaf the code reads
// must be documented in the template. Adding a field without documenting it
// (or documenting a key that was removed) fails here.
func TestTemplatesMatchSchema(t *testing.T) {
	cases := []struct {
		name     string
		schema   map[string]nodeKind
		template string
	}{
		{"global", schemaOf(Global{}), globalTemplate},
		{"repo", schemaOf(Repo{}), repoTemplate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			documented := pathSet(parseKeys(c.template, c.schema))
			for path := range documented {
				if _, ok := c.schema[path]; !ok {
					t.Errorf("template documents %q; the schema does not know it", path)
				}
			}
			for path, kind := range c.schema {
				if kind == kindLeaf && !documented[path] {
					t.Errorf("schema reads %q; the template does not document it", path)
				}
			}
		})
	}
}

func TestSchemaOfGlobal(t *testing.T) {
	s := schemaOf(Global{})
	for path, kind := range map[string]nodeKind{
		"runner":                 kindLeaf,
		"gates":                  kindStruct,
		"gates.push":             kindLeaf,
		"gates.default_branch":   kindStruct,
		"gates.patterns":         kindList,
		"gates.patterns[].match": kindLeaf,
		"accounts":               kindMap,
		"accounts.*.token_file":  kindLeaf,
		"projects.*.color":       kindLeaf,
		"tmux.options":           kindLeaf,
	} {
		if got, ok := s[path]; !ok || got != kind {
			t.Errorf("schema[%q] = %v, %v; want %v, true", path, got, ok, kind)
		}
	}
}

func TestCheckGlobalDeadKeys(t *testing.T) {
	rep := CheckGlobal([]byte("gates:\n  work_hours: \"09:00-18:00\"\n  push: ask\n"))
	if len(rep.Dead) != 1 || rep.Dead[0].Path != "gates.work_hours" {
		t.Fatalf("Dead = %+v; want exactly gates.work_hours", rep.Dead)
	}
}

func TestCheckGlobalDeadCollapsesToHighestUnknown(t *testing.T) {
	rep := CheckGlobal([]byte("montior:\n  model: opus\n"))
	if len(rep.Dead) != 1 || rep.Dead[0].Path != "montior" {
		t.Fatalf("Dead = %+v; want exactly montior", rep.Dead)
	}
}

func TestCheckGlobalStaleComment(t *testing.T) {
	in := "# gates:\n#   work_hours: \"09:00-18:00\"\ngates:\n  push: ask\n"
	rep := CheckGlobal([]byte(in))
	if len(rep.Dead) != 0 {
		t.Fatalf("Dead = %+v; want none", rep.Dead)
	}
	if len(rep.Stale) != 1 || rep.Stale[0].Path != "gates.work_hours" {
		t.Fatalf("Stale = %+v; want exactly gates.work_hours", rep.Stale)
	}
}

func TestCheckGlobalNewKeys(t *testing.T) {
	rep := CheckGlobal([]byte("runner: tmux\ngates:\n  push: ask\n"))
	paths := map[string]string{}
	for _, f := range rep.New {
		paths[f.Path] = f.Note
	}
	// gates is mentioned, so its unmentioned children are listed one by one.
	for _, want := range []string{"gates.read_only", "gates.default_branch", "gates.patterns"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("New misses %q; got %v", want, paths)
		}
	}
	// unmentioned sections collapse to their top-level key
	for _, want := range []string{"apps", "monitor", "default_mode"} {
		if _, ok := paths[want]; !ok {
			t.Errorf("New misses %q; got %v", want, paths)
		}
	}
	if _, ok := paths["apps.editor"]; ok {
		t.Errorf("New lists apps.editor although apps already collapses it")
	}
	if note := paths["gates.read_only"]; !strings.Contains(note, "read-only modes") {
		t.Errorf("gates.read_only note = %q; want the template's comment text", note)
	}
	// default_branch has no trailing comment of its own; it borrows a child's
	if note := paths["gates.default_branch"]; !strings.Contains(note, "protected branch") {
		t.Errorf("gates.default_branch note = %q; want a borrowed child doc", note)
	}
}

func TestCheckGlobalMentionInCommentSuppressesNew(t *testing.T) {
	rep := CheckGlobal([]byte("# monitor:\n#   model: opus\n"))
	for _, f := range rep.New {
		if f.Path == "monitor" || f.Path == "monitor.model" {
			t.Errorf("New lists %q although the file documents it", f.Path)
		}
	}
}

func TestCheckGlobalWildcardKeysAreKnown(t *testing.T) {
	in := "accounts:\n  foo:\n    token_file: /tmp/x\nprojects:\n  bar:\n    color: cyan\n"
	rep := CheckGlobal([]byte(in))
	if len(rep.Dead) != 0 {
		t.Fatalf("Dead = %+v; want none", rep.Dead)
	}
}

func TestCheckGlobalTypeError(t *testing.T) {
	rep := CheckGlobal([]byte("tmux:\n  options: notalist\n"))
	if len(rep.Values) != 1 || !strings.Contains(rep.Values[0].Path, "cannot unmarshal") {
		t.Fatalf("Values = %+v; want one cannot-unmarshal finding", rep.Values)
	}
}

func TestCheckGlobalUnknownFieldNotDoubleReported(t *testing.T) {
	rep := CheckGlobal([]byte("gates:\n  work_hours: x\n"))
	if len(rep.Values) != 0 {
		t.Fatalf("Values = %+v; unknown fields belong in Dead only", rep.Values)
	}
}

func TestCheckGlobalParseError(t *testing.T) {
	rep := CheckGlobal([]byte("runner: [unclosed\n"))
	if rep.ParseErr == "" {
		t.Fatal("ParseErr empty; want a parse failure")
	}
}

func TestCheckGlobalCleanScaffold(t *testing.T) {
	rep := CheckGlobal([]byte(globalTemplate))
	if len(rep.Dead) != 0 || len(rep.Stale) != 0 || len(rep.New) != 0 || len(rep.Values) != 0 {
		t.Fatalf("fresh scaffold not clean: %+v", rep)
	}
}

func TestCheckRepoCleanScaffold(t *testing.T) {
	rep := CheckRepo([]byte(repoTemplate))
	if len(rep.Dead) != 0 || len(rep.Stale) != 0 || len(rep.New) != 0 || len(rep.Values) != 0 {
		t.Fatalf("fresh scaffold not clean: %+v", rep)
	}
}

func TestCheckRepoDeadAndNew(t *testing.T) {
	rep := CheckRepo([]byte("worktree:\n  prefix: agent\n  branch_prefix: feat\n"))
	if len(rep.Dead) != 1 || rep.Dead[0].Path != "worktree.branch_prefix" {
		t.Fatalf("Dead = %+v; want exactly worktree.branch_prefix", rep.Dead)
	}
	found := false
	for _, f := range rep.New {
		if f.Path == "worktree.base" {
			found = true
		}
	}
	if !found {
		t.Fatalf("New = %+v; want worktree.base listed", rep.New)
	}
}

func TestCheckPatternListKeys(t *testing.T) {
	in := "gates:\n  patterns:\n    - match: 'x'\n      action: ask\n      badkey: y\n"
	rep := CheckGlobal([]byte(in))
	if len(rep.Dead) != 1 || rep.Dead[0].Path != "gates.patterns[].badkey" {
		t.Fatalf("Dead = %+v; want exactly gates.patterns[].badkey", rep.Dead)
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBoardLayoutRoundTrip(t *testing.T) {
	t.Setenv("KOVAN_HOME", t.TempDir())

	// A missing file is the default layout.
	l, err := LoadBoardLayout()
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Columns) != 0 || len(l.HiddenColumns) != 0 {
		t.Fatalf("missing file loaded as %+v, want the zero layout", l)
	}

	want := &BoardLayout{Columns: []string{"ID", "STATE", "TITLE"}, HiddenColumns: []string{"PERM", "ACCOUNT"}}
	if err := SaveBoardLayout(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadBoardLayout()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Columns) != 3 || got.Columns[0] != "ID" || len(got.HiddenColumns) != 2 || got.HiddenColumns[0] != "PERM" {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestBoardLayoutUnparsable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "board.yaml"), []byte("{[ not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBoardLayout(); err == nil {
		t.Fatal("garbage board.yaml should surface a parse error")
	}
}

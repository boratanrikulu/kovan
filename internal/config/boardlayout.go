package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// BoardLayout is the cockpit's per-user board preferences. Unlike config.yaml
// (human-authored, scaffold-only), this file is machine-written: the board's
// columns overlay saves it on every change.
type BoardLayout struct {
	Columns       []string `yaml:"columns,omitempty"` // full column order by name
	HiddenColumns []string `yaml:"hidden_columns,omitempty"`
}

func boardLayoutFile() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "board.yaml"), nil
}

// LoadBoardLayout reads ~/.kovan/board.yaml. A missing file is the default
// layout, not an error.
func LoadBoardLayout() (*BoardLayout, error) {
	l := &BoardLayout{}
	path, err := boardLayoutFile()
	if err != nil {
		return nil, err
	}
	if err := readYAML(path, l); err != nil {
		return nil, err
	}
	return l, nil
}

// SaveBoardLayout writes ~/.kovan/board.yaml atomically, so a concurrent
// cockpit never reads a half-written file.
func SaveBoardLayout(l *BoardLayout) error {
	path, err := boardLayoutFile()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(l)
	if err != nil {
		return fmt.Errorf("marshal board layout: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "board-*.tmp")
	if err != nil {
		return fmt.Errorf("write board layout: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write board layout: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write board layout: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("write board layout: %w", err)
	}
	return nil
}

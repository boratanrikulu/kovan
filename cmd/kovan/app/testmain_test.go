package app

import (
	"os"
	"testing"
)

// TestMain forces KOVAN_HOME to a throwaway dir when the caller didn't set
// one, so no test can ever read or prune the real ~/.kovan session index.
func TestMain(m *testing.M) {
	if os.Getenv("KOVAN_HOME") == "" {
		dir, err := os.MkdirTemp("", "kovan-test-home-")
		if err != nil {
			panic(err)
		}
		os.Setenv("KOVAN_HOME", dir)
		code := m.Run()
		os.RemoveAll(dir)
		os.Exit(code)
	}
	os.Exit(m.Run())
}

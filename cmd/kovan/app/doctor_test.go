package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/boratanrikulu/kovan/internal/config"
)

// staleConfig is an aged scaffold: a key the schema no longer has, both active
// and documented in a comment, plus a gate value the code ignores.
const staleConfig = `# kovan's own settings.

# gates:
#   work_hours: "09:00-18:00"   # only gate during these hours
gates:
  work_hours: "09:00-18:00"
  push: aks
`

// doctorHome isolates a test: a throwaway kovan home, and a working directory
// outside any git repo so the surrounding checkout's .kovan.yaml stays out of
// the report.
func doctorHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("KOVAN_HOME", home)
	t.Chdir(t.TempDir())
	return home
}

func TestDoctorReportsDriftAndExitsDirty(t *testing.T) {
	home := doctorHome(t)
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(staleConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	clean, err := runDoctor(&out)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Fatal("clean = true; want dirty")
	}
	got := out.String()
	for _, want := range []string{
		"no longer read",
		"gates.work_hours",
		"new since your config was written",
		"gates.read_only",
		"check values",
		`gates.push: "aks"`,
		"silently disables",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
}

func TestDoctorMissingFileIsClean(t *testing.T) {
	doctorHome(t)
	var out strings.Builder
	clean, err := runDoctor(&out)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatalf("clean = false; want clean:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "not present, defaults apply") {
		t.Errorf("output misses the missing-file line:\n%s", out.String())
	}
}

func TestDoctorValueFindings(t *testing.T) {
	home := doctorHome(t)
	cfg := `default_account: ghost
default_mode: nosuchmode
projects:
  kovan:
    color: pink
gates:
  patterns:
    - match: "([unclosed"
      action: block
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	clean, err := runDoctor(&out)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Fatal("clean = true; want dirty")
	}
	got := out.String()
	for _, want := range []string{
		`default_account: "ghost"`,
		`default_mode: "nosuchmode"`,
		`projects.kovan.color: "pink"`,
		"gates.patterns[0].match",
		"gates.patterns[0].action",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output misses %q:\n%s", want, got)
		}
	}
}

func TestDoctorFreshScaffoldIsClean(t *testing.T) {
	home := doctorHome(t)
	if err := config.ScaffoldGlobal(home); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	clean, err := runDoctor(&out)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatalf("fresh scaffold reported dirty:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "ok — matches the current schema") {
		t.Errorf("output misses the ok line:\n%s", out.String())
	}
}

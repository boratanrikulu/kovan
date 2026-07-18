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
	clean, err := runDoctor(&out, nil)
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
	clean, err := runDoctor(&out, nil)
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
	clean, err := runDoctor(&out, nil)
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
	clean, err := runDoctor(&out, nil)
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

func TestDoctorSyncRewritesWithBackup(t *testing.T) {
	home := doctorHome(t)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(staleConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	var asked []string
	decide := func(f config.Finding) bool {
		asked = append(asked, f.Path)
		return true
	}
	var out strings.Builder
	if _, err := runDoctor(&out, decide); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "gates.work_hours" {
		t.Fatalf("asked = %v; want one grouped ask for gates.work_hours", asked)
	}
	synced, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(synced)
	if strings.Contains(got, "work_hours") {
		t.Errorf("removed key still in file:\n%s", got)
	}
	for _, want := range []string{"  push: aks\n", "#   read_only: ask"} {
		if !strings.Contains(got, want) {
			t.Errorf("synced file misses %q:\n%s", want, got)
		}
	}
	bak, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(bak) != staleConfig {
		t.Errorf("backup does not hold the original")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("file mode changed to %v; want 0600 preserved", info.Mode().Perm())
	}
	if !strings.Contains(out.String(), "synced (backup at") {
		t.Errorf("output misses the synced line:\n%s", out.String())
	}
}

func TestDoctorSyncPristineFileNoQuestions(t *testing.T) {
	home := doctorHome(t)
	path := filepath.Join(home, "config.yaml")
	stale := "# old header\n\n# gates:\n#   work_hours: \"x\"\n"
	if err := os.WriteFile(path, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	decide := func(f config.Finding) bool {
		t.Fatalf("asked about %s; a pristine file must not prompt", f.Path)
		return false
	}
	var out strings.Builder
	clean, err := runDoctor(&out, decide)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatalf("clean = false after pristine sync:\n%s", out.String())
	}
	synced, _ := os.ReadFile(path)
	if strings.Contains(string(synced), "work_hours") {
		t.Errorf("pristine file not replaced by fresh template:\n%s", synced)
	}
}

func TestDoctorSyncDeclinedKeepsKeyAndStaysDirty(t *testing.T) {
	home := doctorHome(t)
	path := filepath.Join(home, "config.yaml")
	if err := os.WriteFile(path, []byte(staleConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	decide := func(config.Finding) bool { return false }
	var out strings.Builder
	clean, err := runDoctor(&out, decide)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Fatal("clean = true; a kept dead key must stay dirty")
	}
	synced, _ := os.ReadFile(path)
	if !strings.Contains(string(synced), "  work_hours:") {
		t.Errorf("declined key removed anyway:\n%s", synced)
	}
}

func TestDoctorSyncInSyncFileUntouched(t *testing.T) {
	home := doctorHome(t)
	path := filepath.Join(home, "config.yaml")
	if err := config.ScaffoldGlobal(home); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if _, err := runDoctor(&out, func(config.Finding) bool { return true }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "already in sync") {
		t.Errorf("output misses the in-sync line:\n%s", out.String())
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("backup written although nothing changed")
	}
}

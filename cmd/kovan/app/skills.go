package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/boratanrikulu/kovan/internal/config"
	"github.com/boratanrikulu/kovan/internal/git"
	"github.com/boratanrikulu/kovan/internal/method"
)

// linkSkill symlinks the skill dir src to dst, idempotent and no-clobber. It
// returns true only when it creates a new link. An existing kovan symlink to src
// is a silent no-op; any other existing entry (a real dir, or a foreign symlink)
// is left untouched with a warning — kovan never clobbers a skill it didn't make.
func linkSkill(src, dst string) bool {
	if info, err := os.Lstat(dst); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if target, _ := os.Readlink(dst); target == src {
				return false // already ours
			}
		}
		fmt.Fprintf(os.Stderr, "kovan: skill %q already exists at %s; leaving it\n", filepath.Base(dst), dst)
		return false
	}
	if err := os.Symlink(src, dst); err != nil {
		fmt.Fprintf(os.Stderr, "kovan: link skill %s: %v\n", filepath.Base(dst), err)
		return false
	}
	return true
}

// linkGlobalSkills symlinks every global-layer skill into claudeSkillsDir
// (~/.claude/skills), so they apply to every Claude session — kovan or not, as
// intended. Best-effort and no-clobber; an absent global/skills dir is a no-op.
// It returns the names it newly linked.
func linkGlobalSkills(home, claudeSkillsDir string) ([]string, error) {
	srcs := method.GlobalSkills(home)
	if len(srcs) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(claudeSkillsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", claudeSkillsDir, err)
	}
	var linked []string
	for _, src := range srcs {
		name := filepath.Base(src)
		if linkSkill(src, filepath.Join(claudeSkillsDir, name)) {
			linked = append(linked, name)
		}
	}
	return linked, nil
}

// linkWorktreeSkills symlinks an agent's scoped skills (account + domain +
// project) into its worktree's .claude/skills, keeping each link untracked via
// the worktree's git exclude rather than the committed .gitignore. No-clobber: a
// name already present (e.g. a committed repo skill) is left alone. It returns
// the names it newly linked.
func linkWorktreeSkills(repo *git.Repo, worktree, account, domain, repoName string) ([]string, error) {
	home, err := config.Dir()
	if err != nil {
		return nil, err
	}
	srcs := method.WorktreeSkills(home, account, domain, repoName)
	if len(srcs) == 0 {
		return nil, nil
	}
	skillsDir := filepath.Join(worktree, ".claude", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", skillsDir, err)
	}
	var linked []string
	for _, src := range srcs {
		name := filepath.Base(src)
		if !linkSkill(src, filepath.Join(skillsDir, name)) {
			continue
		}
		linked = append(linked, name)
		// Keep our symlink out of git without touching the committed .gitignore.
		if err := repo.AddExclude(filepath.Join(".claude", "skills", name)); err != nil {
			fmt.Fprintf(os.Stderr, "kovan: exclude skill %s: %v\n", name, err)
		}
	}
	return linked, nil
}

// unlinkWorktreeSkills removes the scoped-skill symlinks linkWorktreeSkills made
// in a worktree's .claude/skills, recomputed from the same layers. It removes
// only a symlink that still points at the kovan skill source — never a real
// directory or a foreign link — so a name the user took over is left intact. It
// returns the names it removed.
func unlinkWorktreeSkills(worktree, account, domain, repoName string) ([]string, error) {
	home, err := config.Dir()
	if err != nil {
		return nil, err
	}
	skillsDir := filepath.Join(worktree, ".claude", "skills")
	var removed []string
	for _, src := range method.WorktreeSkills(home, account, domain, repoName) {
		dst := filepath.Join(skillsDir, filepath.Base(src))
		info, err := os.Lstat(dst)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue // gone, or a real entry we never made
		}
		if target, _ := os.Readlink(dst); target != src {
			continue // a foreign link; not ours to remove
		}
		if err := os.Remove(dst); err != nil {
			fmt.Fprintf(os.Stderr, "kovan: unlink skill %s: %v\n", filepath.Base(dst), err)
			continue
		}
		removed = append(removed, filepath.Base(dst))
	}
	return removed, nil
}

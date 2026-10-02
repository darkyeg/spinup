// Package project inspects a repo and works out what is missing for agents: remote, AGENTS.md, stack skills.
package project

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/darkyeg/spinup/internal/source"
)

// MaxAgentsLines is the AGENTS.md length above which it costs too much to load every session.
const MaxAgentsLines = 200

type Report struct {
	Root     string
	Stacks   []string
	Remote   Remote
	AgentsMD AgentsMD
	Notes    []Note
	Skills   []SkillGap
	Actions  []Action
}

type AgentsMD struct {
	Present bool
	Lines   int
}

func (a AgentsMD) TooLong() bool { return a.Lines > MaxAgentsLines }

type Note struct{ Stack, Text string }

// SkillGap is a stack's skills from one source that the repo does not have yet.
type SkillGap struct {
	Stack   string
	Source  string
	Missing []string
}

// Action changes the repo; Apply returns a line describing the outcome.
type Action struct {
	Description string
	Apply       func(ctx context.Context) (string, error)
}

// Inspect reports on the git repo at root without changing it.
func Inspect(ctx context.Context, src source.Source, root string, run Runner) (Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return Report{}, fmt.Errorf("%s is not a git repo root", root)
	}
	rules, err := loadRules(src.Data())
	if err != nil {
		return Report{}, err
	}
	repo := os.DirFS(root)
	files := listFiles(repo)
	deps := packageDeps(repo, files)

	report := Report{Root: root, Remote: inspectRemote(ctx, run, root), AgentsMD: inspectAgentsMD(repo)}
	if report.Remote.Rewrite != "" {
		report.Actions = append(report.Actions, Action{"fix the remote", rewriteRemote(run, root, report.Remote)})
	}
	for _, r := range rules {
		if !r.matches(repo, files, deps) {
			continue
		}
		report.Stacks = append(report.Stacks, r.Stack)
		if r.Note != "" {
			report.Notes = append(report.Notes, Note{r.Stack, r.Note})
		}
		for _, gap := range missingSkills(repo, r) {
			report.Skills = append(report.Skills, gap)
			report.Actions = append(report.Actions, addSkills(run, root, gap))
		}
	}
	return report, nil
}

func inspectAgentsMD(repo fs.FS) AgentsMD {
	text, err := fs.ReadFile(repo, "AGENTS.md")
	if err != nil {
		return AgentsMD{}
	}
	return AgentsMD{Present: true, Lines: countLines(string(text))}
}

func missingSkills(repo fs.FS, r rule) []SkillGap {
	var gaps []SkillGap
	origins := make([]string, 0, len(r.Skills))
	for origin := range r.Skills {
		origins = append(origins, origin)
	}
	slices.Sort(origins)
	for _, origin := range origins {
		var missing []string
		for _, name := range r.Skills[origin] {
			if _, err := fs.Stat(repo, path.Join(".agents/skills", name)); err != nil {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			gaps = append(gaps, SkillGap{r.Stack, origin, missing})
		}
	}
	return gaps
}

func addSkills(run Runner, root string, gap SkillGap) Action {
	args := []string{"--yes", "skills", "add", gap.Source, "-a", "claude-code", "-a", "codex"}
	for _, name := range gap.Missing {
		args = append(args, "-s", name)
	}
	args = append(args, "-y")
	return Action{
		Description: "add " + strings.Join(gap.Missing, ", "),
		Apply: func(ctx context.Context) (string, error) {
			err := run.RunIn(ctx, root, "npx", args...)
			if errors.Is(err, exec.ErrNotFound) {
				return "", errors.New("Node.js is needed for skills (npx not found). Install Node.js, then re-run")
			}
			if err != nil {
				return "", fmt.Errorf("skills add %s failed: %w", gap.Source, err)
			}
			return "Added " + strings.Join(gap.Missing, ", "), nil
		},
	}
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(text, "\n"), "\n") + 1
}

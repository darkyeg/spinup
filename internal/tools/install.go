package tools

import (
	"context"
	"runtime"

	"github.com/darkyeg/spinup/internal/shell"
)

// Outcome says how installing one tool went.
type Outcome int

const (
	Installed Outcome = iota
	NoInstaller
	Failed
)

// Result is the outcome of installing one tool.
type Result struct {
	Tool    Tool
	Outcome Outcome
	Err     error
}

func hostPlatform() platform {
	return platform{os: runtime.GOOS, which: func(program string) bool {
		_, ok := shell.Find(program)
		return ok
	}}
}

// Missing returns the tools none of whose check commands are on PATH.
func Missing(all []Tool) []Tool {
	shell.RefreshPath()
	p := hostPlatform()
	var missing []Tool
	for _, t := range all {
		if !installed(t, p) {
			missing = append(missing, t)
		}
	}
	return missing
}

func installed(t Tool, p platform) bool {
	for _, command := range t.Check {
		if p.which(command) {
			return true
		}
	}
	return false
}

const aptGet = "apt-get"

type run func(ctx context.Context, asRoot bool, program string, args ...string) error

// session installs tools one after another: each choice is made after the previous install has put
// its tools on PATH, and apt's package index is refreshed once, before the first apt install.
type session struct {
	platform     platform
	run          run
	refreshPath  func()
	indexUpdated bool
}

func hostSession() *session {
	return &session{platform: hostPlatform(), run: runProgram, refreshPath: shell.RefreshPath}
}

func runProgram(ctx context.Context, asRoot bool, program string, args ...string) error {
	if asRoot {
		return shell.AsRoot(ctx, program, args...)
	}
	return shell.Run(ctx, program, args...)
}

// Install runs each tool's installer in this terminal and carries on after a failure.
func Install(ctx context.Context, missing []Tool) []Result {
	s := hostSession()
	results := make([]Result, 0, len(missing))
	for _, t := range missing {
		results = append(results, s.install(ctx, t))
	}
	return results
}

func (s *session) install(ctx context.Context, t Tool) Result {
	in, ok := choose(t, s.platform)
	if !ok {
		return Result{Tool: t, Outcome: NoInstaller}
	}
	if in.program == aptGet && !s.indexUpdated {
		if err := s.run(ctx, true, aptGet, "update"); err != nil {
			return Result{Tool: t, Outcome: Failed, Err: err}
		}
		s.indexUpdated = true
	}
	if err := s.run(ctx, in.asRoot, in.program, in.args...); err != nil {
		return Result{Tool: t, Outcome: Failed, Err: err}
	}
	s.refreshPath()
	return Result{Tool: t, Outcome: Installed}
}

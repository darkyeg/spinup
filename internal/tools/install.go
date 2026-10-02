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

// Install runs each tool's installer in this terminal and carries on after a failure.
func Install(ctx context.Context, missing []Tool) []Result {
	p := hostPlatform()
	results := make([]Result, 0, len(missing))
	for _, t := range missing {
		results = append(results, installOne(ctx, t, p))
	}
	return results
}

func installOne(ctx context.Context, t Tool, p platform) Result {
	in, ok := choose(t, p)
	if !ok {
		return Result{Tool: t, Outcome: NoInstaller}
	}
	run := shell.Run
	if in.asRoot {
		run = shell.AsRoot
	}
	if err := run(ctx, in.program, in.args...); err != nil {
		return Result{Tool: t, Outcome: Failed, Err: err}
	}
	return Result{Tool: t, Outcome: Installed}
}

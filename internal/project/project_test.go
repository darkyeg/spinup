package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/darkyeg/spinup/internal/source"
)

func TestListFilesSkipsBuildDotAndDeepDirs(t *testing.T) {
	repo := fstest.MapFS{
		"go.mod":                   {},
		"a/b/c/ok.txt":             {},
		"a/b/c/d/deep.txt":         {},
		"node_modules/x/y.txt":     {},
		"web/dist/out.js":          {},
		".github/workflows/ci.yml": {},
		"svc/vendor/v.go":          {},
		".env":                     {},
		"pkg/target/t.rs":          {},
		"pkg/src/main.rs":          {},
		"x/bin/a":                  {},
		"x/obj/a":                  {},
		"x/out/a":                  {},
		"x/build/a":                {},
	}
	got := listFiles(repo)
	want := []string{".env", "a/b/c/ok.txt", "go.mod", "pkg/src/main.rs"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPackageDepsReadsAllDependencyGroups(t *testing.T) {
	repo := fstest.MapFS{
		"package.json":        {Data: []byte(`{"dependencies": {"next": "1"}, "devDependencies": {"effect": "1"}}`)},
		"apps/w/package.json": {Data: []byte(`{"peerDependencies": {"react": "1"}}`)},
		"bad/package.json":    {Data: []byte(`not json`)},
	}
	deps := packageDeps(repo, listFiles(repo))
	for _, name := range []string{"next", "effect", "react"} {
		if !deps[name] {
			t.Errorf("missing %s in %v", name, deps)
		}
	}
}

func TestRuleMatches(t *testing.T) {
	repo := fstest.MapFS{
		"go.mod":         {Data: []byte("require google.golang.org/grpc v1")},
		"svc/App.csproj": {},
	}
	files := listFiles(repo)
	cases := []struct {
		name string
		rule rule
		deps map[string]bool
		want bool
	}{
		{"file glob", rule{Files: []string{"go.mod"}}, nil, true},
		{"glob on a nested file name", rule{Files: []string{"*.csproj"}}, nil, true},
		{"no match", rule{Files: []string{"Cargo.toml"}}, nil, false},
		{"package", rule{Packages: []string{"next"}}, map[string]bool{"next": true}, true},
		{"contains hit", rule{Files: []string{"go.mod"}, Contains: []string{"connectrpc.com/connect", "google.golang.org/grpc"}}, nil, true},
		{"contains miss", rule{Files: []string{"go.mod"}, Contains: []string{"connectrpc.com/connect"}}, nil, false},
		{"empty contains never matches", rule{Files: []string{"go.mod"}, Contains: []string{}}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.rule.matches(repo, files, c.deps); got != c.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func TestScpTarget(t *testing.T) {
	cases := []struct {
		url, host, path string
		ok              bool
	}{
		{"gh:owner/repo", "gh", "owner/repo", true},
		{"git@work:owner/repo.git", "work", "owner/repo", true},
		{"host:owner/repo/", "host", "owner/repo", true},
		{"https://example.com/owner/repo", "", "", false},
		{"/local/path", "", "", false},
	}
	for _, c := range cases {
		host, path, ok := scpTarget(c.url)
		if host != c.host || path != c.path || ok != c.ok {
			t.Errorf("scpTarget(%q) = %q %q %v", c.url, host, path, ok)
		}
	}
}

func TestResolvesToGitHub(t *testing.T) {
	if !resolvesToGitHub("user git\r\nHostName github.com\r\nport 22") {
		t.Error("github.com should resolve")
	}
	if resolvesToGitHub("hostname gitlab.com\nproxycommand github.com") {
		t.Error("other hosts should not resolve")
	}
}

type fakeRunner struct {
	outputs map[string]error
	stdout  map[string]string
	calls   []string
	runErr  error
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	for prefix, err := range f.outputs {
		if strings.Contains(call, prefix) {
			return f.stdout[prefix], err
		}
	}
	return "", nil
}

func (f *fakeRunner) RunIn(_ context.Context, dir, name string, args ...string) error {
	f.calls = append(f.calls, "in "+dir+": "+name+" "+strings.Join(args, " "))
	return f.runErr
}

func TestInspectRemote(t *testing.T) {
	cases := []struct {
		name string
		run  *fakeRunner
		want Remote
	}{
		{"github url is fine", &fakeRunner{stdout: map[string]string{"get-url": "https://github.com/o/r\n"}, outputs: map[string]error{"get-url": nil}},
			Remote{URL: "https://github.com/o/r"}},
		{"alias for github is rewritten", &fakeRunner{
			stdout:  map[string]string{"get-url": "gh:o/r\n", "ssh -G": "hostname github.com\n"},
			outputs: map[string]error{"get-url": nil, "ssh -G": nil}},
			Remote{URL: "gh:o/r", Rewrite: "git@github.com:o/r.git"}},
		{"alias for another host is left alone", &fakeRunner{
			stdout:  map[string]string{"get-url": "gl:o/r\n", "ssh -G": "hostname gitlab.com\n"},
			outputs: map[string]error{"get-url": nil, "ssh -G": nil}},
			Remote{URL: "gl:o/r"}},
		{"no origin", &fakeRunner{outputs: map[string]error{"get-url": errors.New("no origin")}}, Remote{Problem: "no origin"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inspectRemote(context.Background(), c.run, "/r"); got != c.want {
				t.Fatalf("got %+v want %+v", got, c.want)
			}
		})
	}
}

func TestRewriteRemoteRevertsWhenGitHubRefuses(t *testing.T) {
	remote := Remote{URL: "gh:o/r", Rewrite: "git@github.com:o/r.git"}
	run := &fakeRunner{outputs: map[string]error{"ls-remote": errors.New("denied")}}
	message, err := rewriteRemote(run, "/r", remote)(context.Background())
	if err != nil || !strings.HasPrefix(message, "Kept gh:o/r") {
		t.Fatalf("%q, %v", message, err)
	}
	last := run.calls[len(run.calls)-1]
	if !strings.HasSuffix(last, "set-url origin gh:o/r") {
		t.Fatalf("not reverted: %v", run.calls)
	}
}

func TestRewriteRemoteKeepsWorkingRewrite(t *testing.T) {
	remote := Remote{URL: "gh:o/r", Rewrite: "git@github.com:o/r.git"}
	run := &fakeRunner{}
	message, err := rewriteRemote(run, "/r", remote)(context.Background())
	if err != nil || !strings.Contains(message, remote.Rewrite) || len(run.calls) != 2 {
		t.Fatalf("%q, %v, %v", message, err, run.calls)
	}
}

func TestInspectFindsStackAndSkillActions(t *testing.T) {
	repo, checkout := t.TempDir(), t.TempDir()
	put(t, filepath.Join(checkout, "skills/per-repo.json"), `{"rules": [
		{"stack": "Go", "files": ["go.mod"], "skills": {"o/s": ["a", "b"]}, "note": "hi"},
		{"stack": "Rust", "files": ["Cargo.toml"]}]}`)
	put(t, filepath.Join(repo, ".git/HEAD"), "")
	put(t, filepath.Join(repo, "go.mod"), "module x")
	put(t, filepath.Join(repo, ".agents/skills/a/SKILL.md"), "")
	put(t, filepath.Join(repo, "AGENTS.md"), "one\ntwo\n")
	run := &fakeRunner{stdout: map[string]string{"get-url": "https://github.com/o/r\n"}, outputs: map[string]error{"get-url": nil}}

	report, err := Inspect(context.Background(), source.At(checkout), repo, run)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(report.Stacks, []string{"Go"}) || report.AgentsMD != (AgentsMD{Present: true, Lines: 2}) {
		t.Fatalf("%+v", report)
	}
	if len(report.Skills) != 1 || !slices.Equal(report.Skills[0].Missing, []string{"b"}) || len(report.Actions) != 1 {
		t.Fatalf("%+v", report)
	}
	if _, err := report.Actions[0].Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "in " + report.Root + ": npx --yes skills add o/s -a claude-code -a codex -s b -y"
	if run.calls[len(run.calls)-1] != want {
		t.Fatalf("got %v", run.calls)
	}
}

func TestInspectRejectsNonRepo(t *testing.T) {
	if _, err := Inspect(context.Background(), source.At(t.TempDir()), t.TempDir(), &fakeRunner{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestAgentsMDTooLong(t *testing.T) {
	if (AgentsMD{Lines: MaxAgentsLines}).TooLong() || !(AgentsMD{Lines: MaxAgentsLines + 1}).TooLong() {
		t.Fatal("limit is exclusive")
	}
}

func put(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

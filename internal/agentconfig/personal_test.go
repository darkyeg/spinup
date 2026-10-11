package agentconfig

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkyeg/spinup/internal/library"
	"github.com/darkyeg/spinup/internal/source"
)

func TestBuiltinInstallPreservesPersonalChoices(t *testing.T) {
	home := t.TempDir()
	homes := Homes{Claude: filepath.Join(home, "claude"), Codex: filepath.Join(home, "codex")}
	yours := library.At(filepath.Join(home, "library"))
	personal := "Use my preferred provider for independent reviews.\n"
	put(t, yours.Path(library.Instructions), personal)
	existing := map[string]string{
		filepath.Join(homes.Claude, "settings.json"):        `{"model":"chosen","outputStyle":"chosen","promptSuggestionEnabled":true,"env":{"CLAUDE_CODE_SUBAGENT_MODEL":"chosen","CLAUDE_CODE_AUTO_COMPACT_WINDOW":"250000"}}`,
		filepath.Join(homes.Codex, "config.toml"):           "# personal\nmodel = \"chosen\"\nmodel_reasoning_effort = \"high\"\n[agents]\ndefault_subagent_model = \"chosen-child\"\nmax_concurrent_threads_per_session = 1\n",
		filepath.Join(homes.Claude, "agents", "Explore.md"): "My own search agent\n",
	}
	for path, text := range existing {
		put(t, path, text)
	}
	src := Sources{Spinup: source.Builtin(), Yours: yours}
	written, err := Install(src, homes)
	if err != nil || len(written) != 2 {
		t.Fatalf("install wrote %v: %v", written, err)
	}
	for path, text := range existing {
		if got := get(t, path); got != text {
			t.Errorf("personal configuration changed: %s", path)
		}
	}
	claude := get(t, filepath.Join(homes.Claude, "CLAUDE.md"))
	codex := get(t, filepath.Join(homes.Codex, "AGENTS.md"))
	if claude != codex || !strings.HasSuffix(claude, personal) {
		t.Fatal("both tools must receive the same personal instructions")
	}
	if written, err := Install(src, homes); err != nil || len(written) != 0 {
		t.Fatalf("repeat install wrote %v: %v", written, err)
	}
}

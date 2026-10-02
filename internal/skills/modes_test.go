package skills

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverrideSettings(t *testing.T) {
	tests := []struct {
		name        string
		settings    string
		manual      []string
		want        string
		wantChanged bool
	}{
		{
			name:        "adds override after the user's keys",
			settings:    `{"z": 1, "a": {"k": true}}`,
			manual:      []string{"x"},
			want:        "{\n  \"z\": 1,\n  \"a\": {\n    \"k\": true\n  },\n  \"skillOverrides\": {\n    \"x\": \"user-invocable-only\"\n  }\n}\n",
			wantChanged: true,
		},
		{
			name:        "creates settings from nothing",
			settings:    "",
			manual:      []string{"x"},
			want:        "{\n  \"skillOverrides\": {\n    \"x\": \"user-invocable-only\"\n  }\n}\n",
			wantChanged: true,
		},
		{
			name:        "drops the key when the last override goes",
			settings:    `{"a": 1, "skillOverrides": {"x": "user-invocable-only"}, "b": 2}`,
			manual:      nil,
			want:        "{\n  \"a\": 1,\n  \"b\": 2\n}\n",
			wantChanged: true,
		},
		{
			name:        "keeps overrides it does not own",
			settings:    `{"skillOverrides": {"x": "off", "y": "user-invocable-only"}}`,
			manual:      nil,
			want:        "{\n  \"skillOverrides\": {\n    \"x\": \"off\"\n  }\n}\n",
			wantChanged: true,
		},
		{
			name:     "unchanged when already right",
			settings: `{"skillOverrides": {"x": "user-invocable-only"}}`,
			manual:   []string{"x"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := overrideSettings([]byte(tt.settings), []string{"x", "y"}, tt.manual)
			if err != nil {
				t.Fatal(err)
			}
			if changed != tt.wantChanged {
				t.Fatalf("changed = %v", changed)
			}
			if changed && string(got) != tt.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestApplyModesWritesAndRemovesCodexPolicy(t *testing.T) {
	root := t.TempDir()
	p := paths{store: filepath.Join(root, "store"), claude: filepath.Join(root, "claude")}
	skillDir := filepath.Join(p.store, "x")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(skillDir, "agents", "openai.yaml")

	if err := p.applyModes([]string{"x"}, []string{"x"}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(policy); err != nil || string(data) != codexManualPolicy {
		t.Fatalf("policy = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(p.claude, "settings.json")); err != nil {
		t.Fatal("settings.json not written")
	}

	if err := p.applyModes([]string{"x"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "agents")); !os.IsNotExist(err) {
		t.Fatalf("agents dir should be gone, err = %v", err)
	}
}

func TestApplyModesKeepsEditedCodexPolicy(t *testing.T) {
	p := paths{store: t.TempDir(), claude: t.TempDir()}
	policy := filepath.Join(p.store, "x", "agents", "openai.yaml")
	if err := os.MkdirAll(filepath.Dir(policy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(policy, []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := p.applyModes([]string{"x"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(policy); err != nil {
		t.Fatal("edited policy was removed")
	}
}

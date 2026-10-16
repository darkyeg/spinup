package packages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkyeg/spinup/internal/host"
	"github.com/darkyeg/spinup/internal/shell"
	"github.com/darkyeg/spinup/internal/sysproc"
)

type Target struct {
	Name string
	Home string
}

func HostTargets() []Target {
	return []Target{{"claude-code", host.ClaudeHome()}, {"codex", host.CodexHome()}}
}

type installed struct {
	ID, Name, Version, Scope string
	Enabled                  bool
}
type nativeState struct {
	plugins []installed
	markets map[string]string
}
type adapter interface {
	Inspect(context.Context, Target) (nativeState, error)
	Register(context.Context, Target, string) error
	Install(context.Context, Target, string) error
	Remove(context.Context, Target, string) error
	Forget(context.Context, Target, string) error
}

type native struct{}

func (native) run(ctx context.Context, t Target, args ...string) ([]byte, error) {
	bin, env := "codex", "CODEX_HOME"
	if t.Name == "claude-code" {
		bin, env = "claude", "CLAUDE_CONFIG_DIR"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := shell.CommandContext(ctx, bin, args...)
	sysproc.Hide(cmd)
	cmd.Dir = filepath.Dir(t.Home)
	cmd.Env = append(os.Environ(), env+"="+t.Home)
	var stdout, stderr boundedBuffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s plugin command: %w: %s (update the tool if plugin commands are unavailable)", t.Name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len() < 4<<20 {
		_, _ = b.Buffer.Write(p[:min(n, (4<<20)-b.Len())])
	}
	return n, nil
}

func (n native) Inspect(ctx context.Context, t Target) (nativeState, error) {
	state := nativeState{markets: map[string]string{}}
	data, err := n.run(ctx, t, "plugin", "list", "--json")
	if err != nil {
		return state, err
	}
	if t.Name == "claude-code" {
		if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '[' {
			return state, fmt.Errorf("unsupported Claude plugin list format")
		}
		var rows []struct {
			ID      string `json:"id"`
			Version string `json:"version"`
			Scope   string `json:"scope"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.Unmarshal(data, &rows); err != nil {
			return state, fmt.Errorf("unsupported Claude plugin list: %w", err)
		}
		for _, r := range rows {
			name, _, _ := strings.Cut(r.ID, "@")
			state.plugins = append(state.plugins, installed{r.ID, name, r.Version, r.Scope, r.Enabled})
		}
	} else {
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(data, &shape); err != nil || shape["installed"] == nil {
			return state, fmt.Errorf("unsupported Codex plugin list format")
		}
		var result struct {
			Installed []struct {
				ID      string `json:"pluginId"`
				Name    string `json:"name"`
				Version string `json:"version"`
				Enabled bool   `json:"enabled"`
			} `json:"installed"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return state, fmt.Errorf("unsupported Codex plugin list: %w", err)
		}
		for _, r := range result.Installed {
			state.plugins = append(state.plugins, installed{r.ID, r.Name, r.Version, "user", r.Enabled})
		}
	}
	data, err = n.run(ctx, t, "plugin", "marketplace", "list", "--json")
	if err != nil {
		return state, err
	}
	if t.Name == "claude-code" {
		if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '[' {
			return state, fmt.Errorf("unsupported Claude marketplace list format")
		}
		var rows []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}
		if err := json.Unmarshal(data, &rows); err != nil {
			return state, err
		}
		for _, r := range rows {
			state.markets[r.Name] = r.Path
		}
	} else {
		var shape map[string]json.RawMessage
		if err := json.Unmarshal(data, &shape); err != nil || shape["marketplaces"] == nil {
			return state, fmt.Errorf("unsupported Codex marketplace list format")
		}
		var result struct {
			Marketplaces []struct {
				Name string `json:"name"`
				Root string `json:"root"`
			} `json:"marketplaces"`
		}
		if err := json.Unmarshal(data, &result); err != nil {
			return state, err
		}
		for _, r := range result.Marketplaces {
			state.markets[r.Name] = r.Root
		}
	}
	return state, nil
}

func (n native) Register(ctx context.Context, t Target, root string) error {
	args := []string{"plugin", "marketplace", "add", root}
	if t.Name == "claude-code" {
		args = append(args, "--scope", "user")
	}
	_, err := n.run(ctx, t, append(args, "--json")...)
	return err
}
func (n native) Install(ctx context.Context, t Target, id string) error {
	verb := "add"
	if t.Name == "claude-code" {
		verb = "install"
	}
	args := []string{"plugin", verb, id}
	if t.Name == "claude-code" {
		args = append(args, "--scope", "user")
	}
	_, err := n.run(ctx, t, append(args, "--json")...)
	return err
}
func (n native) Remove(ctx context.Context, t Target, id string) error {
	verb := "remove"
	if t.Name == "claude-code" {
		verb = "uninstall"
	}
	args := []string{"plugin", verb, id}
	if t.Name == "claude-code" {
		args = append(args, "--scope", "user", "--keep-data")
	}
	_, err := n.run(ctx, t, append(args, "--json")...)
	return err
}
func (n native) Forget(ctx context.Context, t Target, market string) error {
	args := []string{"plugin", "marketplace", "remove", market}
	if t.Name == "claude-code" {
		args = append(args, "--scope", "user")
	}
	_, err := n.run(ctx, t, append(args, "--json")...)
	return err
}

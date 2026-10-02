// Package proxy runs CLIProxyAPI: renders its config, starts and stops it, installs it, and reads
// its management API.
package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkyeg/spinup"
	"github.com/darkyeg/spinup/internal/config"
	"github.com/darkyeg/spinup/internal/sysproc"
)

// Render fills the config template.
func Render(template, host string, port int, authDir string, s config.Secrets) string {
	return strings.NewReplacer(
		"{{HOST}}", host,
		"{{AUTH_DIR}}", filepath.ToSlash(authDir), // forward slashes: no YAML escapes on Windows
		"{{PORT}}", strconv.Itoa(port),
		"{{API_KEY}}", s.APIKey,
		"{{SECRET_KEY}}", s.ManagementPassword, // the proxy hashes it on start
	).Replace(template)
}

// WriteConfig renders the config for the service: CLIProxyAPI listens on 127.0.0.1 only; the
// spinup front and peer port are the way in.
func WriteConfig(c config.Config, s config.Secrets) error {
	text := Render(spinup.ProxyConfigTemplate, "127.0.0.1", c.ProxyPort, c.AuthDir, s)
	return config.WriteFileAtomic(c.ProxyConfig(), []byte(text), 0o600)
}

// Runner keeps CLIProxyAPI running while it is wanted, and stopped otherwise.
type Runner struct {
	Exe, Dir, Config string
	Port             int
	Log              *log.Logger

	mu     sync.Mutex
	want   bool
	cmd    *exec.Cmd
	exited chan struct{}
}

// Start makes sure the proxy runs (restarting it if it crashes) and waits until it answers.
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	r.want = true
	running := r.cmd != nil
	r.mu.Unlock()
	if !running {
		if err := r.spawn(); err != nil {
			return err
		}
	}
	return r.waitHealthy(ctx)
}

func (r *Runner) spawn() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.want || r.cmd != nil {
		return nil
	}
	cmd := exec.Command(r.Exe, "-config", r.Config)
	cmd.Dir = r.Dir
	sysproc.Hide(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start CLIProxyAPI: %w", err)
	}
	r.cmd, r.exited = cmd, make(chan struct{})
	r.Log.Printf("proxy: started CLIProxyAPI (pid %d)", cmd.Process.Pid)
	go r.watch(cmd, r.exited)
	return nil
}

func (r *Runner) watch(cmd *exec.Cmd, exited chan struct{}) {
	err := cmd.Wait()
	close(exited)
	r.mu.Lock()
	r.cmd = nil
	again := r.want
	r.mu.Unlock()
	if again {
		r.Log.Printf("proxy: CLIProxyAPI exited unexpectedly (%v); restarting in 3s", err)
		time.Sleep(3 * time.Second)
		if err := r.spawn(); err != nil {
			r.Log.Printf("proxy: %v", err)
		}
	}
}

// Stop stops the proxy and waits for it to exit. Before killing it, it waits until no login file
// has changed for a moment, so a refresh in progress gets written out first.
func (r *Runner) Stop(quiet func() bool) {
	r.mu.Lock()
	r.want = false
	cmd, exited := r.cmd, r.exited
	r.mu.Unlock()
	if cmd == nil {
		return
	}
	for i := 0; i < 10 && quiet != nil && !quiet(); i++ {
		time.Sleep(500 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		r.Log.Printf("proxy: CLIProxyAPI did not exit after kill")
	}
	r.Log.Printf("proxy: stopped CLIProxyAPI")
}

// Running reports whether the proxy process is up.
func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cmd != nil
}

func (r *Runner) waitHealthy(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if Healthy(ctx, fmt.Sprintf("http://127.0.0.1:%d", r.Port)) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("CLIProxyAPI didn't answer on port %d within 30s (see logs in %s)", r.Port, r.Dir)
}

// Healthy reports whether a CLIProxyAPI answers at base.
func Healthy(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return strings.Contains(string(body), "CLI Proxy API")
}

// AuthState is one login as the proxy sees it.
type AuthState struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Unavailable   bool   `json:"unavailable"`
	Disabled      bool   `json:"disabled"`
}

// Broken reports whether the proxy failed to use this login because its token was refused.
func (a AuthState) Broken() bool {
	msg := strings.ToLower(a.StatusMessage)
	refused := strings.Contains(msg, "invalid_grant") || strings.Contains(msg, "refresh") ||
		strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized")
	return !a.Disabled && refused && (a.Unavailable || strings.EqualFold(a.Status, "error"))
}

// AuthStates lists the logins through the management API.
func AuthStates(ctx context.Context, port int, managementPassword string) ([]AuthState, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("http://127.0.0.1:%d/v0/management/auth-files", port), nil)
	req.Header.Set("Authorization", "Bearer "+managementPassword)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("management API: %s", resp.Status)
	}
	var out struct {
		Files []AuthState `json:"files"`
	}
	return out.Files, json.NewDecoder(resp.Body).Decode(&out)
}

// Installed reports whether the binary exists.
func Installed(c config.Config) bool {
	_, err := os.Stat(c.ProxyExe())
	return err == nil
}

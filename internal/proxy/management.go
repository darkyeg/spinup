package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// healthy reports whether a CLIProxyAPI answers at base.
func healthy(ctx context.Context, base string) bool {
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

// LoginState is one login as the running proxy sees it.
type LoginState struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Unavailable   bool   `json:"unavailable"`
	Disabled      bool   `json:"disabled"`
}

// Refused reports whether the provider refused the login's token, as opposed to a rate limit.
func (s LoginState) Refused() bool {
	msg := strings.ToLower(s.StatusMessage)
	refused := strings.Contains(msg, "invalid_grant") || strings.Contains(msg, "refresh") ||
		strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized")
	return !s.Disabled && refused && (s.Unavailable || strings.EqualFold(s.Status, "error"))
}

func LoginStates(ctx context.Context, port int, managementPassword string) ([]LoginState, error) {
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
		Files []LoginState `json:"files"`
	}
	return out.Files, json.NewDecoder(resp.Body).Decode(&out)
}

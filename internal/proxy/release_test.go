package proxy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkyeg/spinup"
	"github.com/darkyeg/spinup/internal/config"
)

func TestRenderFillsEveryPlaceholder(t *testing.T) {
	out := render(spinup.ProxyConfigTemplate, "127.0.0.1", 8327, filepath.FromSlash("/home/me/.cli-proxy-api"),
		config.Secrets{APIKey: "k", ManagementPassword: "m"})
	if strings.Contains(out, "{{") {
		t.Fatalf("unfilled placeholder in:\n%s", out)
	}
	for _, want := range []string{`host: "127.0.0.1"`, "port: 8327", `- "k"`, `secret-key: "m"`, `auth-dir: "/home/me/.cli-proxy-api"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestChecksumFor(t *testing.T) {
	sums := "abc  CLIProxyAPI_1_linux_amd64.tar.gz\ndef *CLIProxyAPI_1_windows_amd64.zip\n"
	if checksumFor(sums, "CLIProxyAPI_1_windows_amd64.zip") != "def" {
		t.Error("binary-mode line not parsed")
	}
	if checksumFor(sums, "missing") != "" {
		t.Error("missing asset must have no checksum")
	}
}

func TestRefused(t *testing.T) {
	if !(LoginState{Unavailable: true, StatusMessage: "refresh failed: invalid_grant"}).Refused() {
		t.Error("invalid_grant is a refused token")
	}
	if (LoginState{Unavailable: true, StatusMessage: "rate limited"}).Refused() {
		t.Error("a rate limit is not a refused token")
	}
}

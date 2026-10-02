package proxy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkyeg/spinup"
	"github.com/darkyeg/spinup/internal/config"
)

func TestRenderFillsEveryPlaceholder(t *testing.T) {
	out := Render(spinup.ProxyConfigTemplate, "127.0.0.1", 8327, filepath.FromSlash("/home/me/.cli-proxy-api"),
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

func TestBroken(t *testing.T) {
	if !(AuthState{Unavailable: true, StatusMessage: "refresh failed: invalid_grant"}).Broken() {
		t.Error("invalid_grant must count as broken")
	}
	if (AuthState{Unavailable: true, StatusMessage: "rate limited"}).Broken() {
		t.Error("a rate limit is not a broken token")
	}
}
